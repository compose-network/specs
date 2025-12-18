package scp

import (
	"errors"
	"fmt"
	"sync"

	"github.com/rs/zerolog"

	"github.com/compose-network/specs/compose"
)

var (
	ErrNoTransactions       = errors.New("no transactions to execute")
	ErrNotInSimulatingState = errors.New("sequencer not in simulating state")
)

// SequencerInstance is an interface that represents the sequencer-side logic for an SCP instance.
type SequencerInstance interface {
	DecisionState() compose.DecisionState
	Run() error
	ProcessMailboxMessage(msg MailboxMessage) error
	ProcessDecidedMessage(decided bool) error
	Timeout()
}

// SequencerState tracks the state machine for a sequencer in an SCP session.
type SequencerState int

const (
	SeqStateSimulating SequencerState = iota
	SeqStateWaitingDecided
	SeqStateDone
)

// SimulationRequest represents the inputs for the mailbox-aware simulator.
type SimulationRequest struct {
	// mailbox.putInbox transactions
	PutInboxMessages []MailboxMessage
	Transactions     [][]byte
	Snapshot         compose.StateRoot
}

// ExecutionEngine represents the execution engine, such as the EVM.
type ExecutionEngine interface {
	ChainID() compose.ChainID
	// Simulate runs the VM with a tracer for the simulation request.
	// The simulation returns a list of written mailbox messages (writeMessages).
	// If there's a read miss, readRequest is populated with the expected header.
	// If there's a simulation error (not read miss), err is populated.
	// Else, err is nil and readRequest is nil (successful transaction execution).
	Simulate(request SimulationRequest) (readRequest *MailboxMessageHeader, writeMessages []MailboxMessage, err error)
}

type SequencerNetwork interface {
	SendMailboxMessage(recipient compose.ChainID, msg MailboxMessage)
	SendVote(vote bool)
}

type sequencerInstance struct {
	mu sync.Mutex

	// Dependencies
	execution ExecutionEngine
	network   SequencerNetwork

	// Protocol state
	state         SequencerState
	decisionState compose.DecisionState

	// List of transactions to be executed by this chain (from the request)
	txs [][]byte
	// Read requests made by the transactions (returned by simulations). Removed on fulfillment.
	expectedReadRequests []MailboxMessageHeader
	// Incoming mailbox messages that can be used to satisfy expected reads.
	pendingMessages []MailboxMessage
	// Consumed pendingMessages that should populate the Mailbox contract
	// before the next transaction simulation.
	putInboxMessages []MailboxMessage
	// Identifies the VM state for the simulation
	vmSnapshot compose.StateRoot

	writtenMessagesCache []MailboxMessage

	logger zerolog.Logger
}

func NewSequencerInstance(
	instance compose.Instance,
	execution ExecutionEngine,
	network SequencerNetwork,
	vmSnapshot compose.StateRoot,
	logger zerolog.Logger,
) (SequencerInstance, error) {
	// Build runner
	r := &sequencerInstance{
		mu:                   sync.Mutex{},
		execution:            execution,
		network:              network,
		state:                SeqStateSimulating, // First state
		decisionState:        compose.DecisionStatePending,
		txs:                  make([][]byte, 0),
		putInboxMessages:     make([]MailboxMessage, 0),
		expectedReadRequests: make([]MailboxMessageHeader, 0),
		pendingMessages:      make([]MailboxMessage, 0),
		vmSnapshot:           vmSnapshot,
		writtenMessagesCache: make([]MailboxMessage, 0),
		logger:               logger,
	}

	// Filter transactions to this chain
	for _, req := range instance.XTRequest.Transactions {
		if req.ChainID != r.execution.ChainID() {
			continue
		}
		for _, payload := range req.Transactions {
			r.txs = append(r.txs, append([]byte(nil), payload...))
		}
	}
	if len(r.txs) == 0 {
		return nil, ErrNoTransactions
	}

	return r, nil
}

func (r *sequencerInstance) DecisionState() compose.DecisionState {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.decisionState
}

// Run executes calls to the mailbox-aware simulation.
// If simulation succeeds, it sends Vote(true) to the SP and set state to waiting for decided.
// If simulation fails due to read miss, it adds the expected read message and looks for new reads to insert.
// If simulation fails for other reasons, it sends Vote(false) and terminates.
func (r *sequencerInstance) Run() error {
	r.mu.Lock()
	if r.state != SeqStateSimulating {
		r.mu.Unlock()
		return ErrNotInSimulatingState
	}

	// Run simulation
	readRequest, writeMessages, err := r.execution.Simulate(SimulationRequest{
		PutInboxMessages: append([]MailboxMessage(nil), r.putInboxMessages...),
		Transactions:     compose.CloneByteSlices(r.txs),
		Snapshot:         r.vmSnapshot,
	})
	if err != nil {
		r.logger.Info().Msg("Simulation failed, rejecting instance. Error: " + err.Error())

		r.network.SendVote(false)
		r.state = SeqStateDone
		r.decisionState = compose.DecisionStateRejected
		r.mu.Unlock()

		return fmt.Errorf("simulating sequencer failed: %w", err)
	}

	// Send write messages
	r.sendWriteMessages(writeMessages)

	// Consume mailbox messages.
	if readRequest != nil {
		r.logger.Info().
			Uint64("source_chain_id", uint64(readRequest.SourceChainID)).
			Str("label", readRequest.Label).
			Msg("Simulation hit read miss, requesting mailbox message.")
		r.expectedReadRequests = append(r.expectedReadRequests, *readRequest)
		r.mu.Unlock()
		return r.consumeReceivedMailboxMessagesAndSimulate()
	}

	// Vote true.
	r.logger.Info().Msg("Simulation succeeded, voting true.")
	r.network.SendVote(true)
	r.state = SeqStateWaitingDecided
	r.mu.Unlock()
	return nil
}

func (r *sequencerInstance) sendWriteMessages(messages []MailboxMessage) {
	for _, msg := range messages {
		// Check if belongs to cache
		alreadySent := false
		for _, cachedMsg := range r.writtenMessagesCache {
			if cachedMsg.Equal(msg) {
				alreadySent = true
				break
			}
		}
		if alreadySent {
			continue
		}

		// Send if new message
		r.network.SendMailboxMessage(msg.MailboxMessageHeader.DestChainID, msg)
		r.writtenMessagesCache = append(r.writtenMessagesCache, msg)
	}
}

// consumeReceivedMailboxMessagesAndSimulate checks if any expected read mailbox messages have been received
// If so, remove from the lists, and call run to simulate.
func (r *sequencerInstance) consumeReceivedMailboxMessagesAndSimulate() error {
	r.mu.Lock()
	includedAny := false

	// Loop through expected messages
	for idx := 0; idx < len(r.expectedReadRequests); {
		expectedMsg := r.expectedReadRequests[idx]
		matched := false
		// Look if it exists in received messages
		for receivedMsgIdx, receivedMsg := range r.pendingMessages {
			if receivedMsg.MailboxMessageHeader.Equal(expectedMsg) {
				// If found, add to mailboxOps
				r.putInboxMessages = append(r.putInboxMessages, receivedMsg)
				// Remove from lists
				r.expectedReadRequests = append(
					r.expectedReadRequests[:idx],
					r.expectedReadRequests[idx+1:]...)
				r.pendingMessages = append(
					r.pendingMessages[:receivedMsgIdx],
					r.pendingMessages[receivedMsgIdx+1:]...)
				// Set flags
				includedAny = true
				matched = true
				break
			}
		}
		// If matched, do not increment idx since we removed current message from the list
		if !matched {
			idx++
		}
	}

	r.mu.Unlock()
	if includedAny {
		r.logger.Info().Msg("Consuming mailbox messages and re-simulating.")
		return r.Run()
	}
	return nil
}

// ProcessMailboxMessage processes an incoming mailbox message.
func (r *sequencerInstance) ProcessMailboxMessage(msg MailboxMessage) error {
	r.mu.Lock()
	if r.state != SeqStateSimulating {
		r.logger.Info().
			Uint64("source_chain_id", uint64(msg.MailboxMessageHeader.SourceChainID)).
			Str("label", msg.MailboxMessageHeader.Label).
			Msg("Ignoring mailbox message because not in simulating state")

		r.mu.Unlock()
		return nil
	}

	r.logger.Info().
		Uint64("source_chain_id", uint64(msg.MailboxMessageHeader.SourceChainID)).
		Str("label", msg.MailboxMessageHeader.Label).
		Msg("Adding mailbox message to pending list")

	r.pendingMessages = append(r.pendingMessages, msg)
	r.mu.Unlock()
	return r.consumeReceivedMailboxMessagesAndSimulate()
}

// ProcessDecidedMessage receives a decided message from the SP.
func (r *sequencerInstance) ProcessDecidedMessage(decided bool) error {
	r.mu.Lock()

	if r.state == SeqStateDone {
		r.logger.Info().
			Bool("received_decided", decided).
			Str("stored_decision", r.decisionState.String()).
			Msg("Ignoring decided message because already done")

		r.mu.Unlock()
		return nil
	}

	r.logger.Info().
		Bool("received_decided", decided).
		Msg("Processing decided message from SP")

	r.state = SeqStateDone
	if decided {
		r.decisionState = compose.DecisionStateAccepted
	} else {
		r.decisionState = compose.DecisionStateRejected
	}
	r.mu.Unlock()
	return nil
}

// Timeout is invoked when the timer fires.
// If not already in waiting for a decided or done state, terminates as rejected and sends Vote(false) to SP.
func (r *sequencerInstance) Timeout() {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.state == SeqStateWaitingDecided || r.state == SeqStateDone {
		r.logger.Info().
			Msg("Ignoring timeout because already waiting for decided or done")
		return
	}

	if len(r.expectedReadRequests) > 0 {
		for _, req := range r.expectedReadRequests {
			r.logger.Warn().
				Str("op", "read").
				Uint64("src_chain", uint64(req.SourceChainID)).
				Uint64("dest_chain", uint64(req.DestChainID)).
				Str("sender", req.Sender.String()).
				Str("receiver", req.Receiver.String()).
				Uint64("session_id", uint64(req.SessionID)).
				Str("label", req.Label).
				Msg("Unfulfilled mailbox request")
		}
	}

	r.logger.Info().
		Int("unfulfilled_reads", len(r.expectedReadRequests)).
		Msg("Timeout occurred, rejecting instance")

	r.state = SeqStateDone
	r.decisionState = compose.DecisionStateRejected
	r.network.SendVote(false)
}
