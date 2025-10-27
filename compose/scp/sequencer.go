package scp

import (
	"compose"
	"errors"
	"sync"
)

var (
	ErrNoTransactions = errors.New("no transactions to execute")
)

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
	Transactions     []compose.Transaction
	Snapshot         compose.StateRoot
}

// ExecutionEngine represents the execution engine, such as the EVM.
type ExecutionEngine interface {
	ChainID() compose.ChainID
	Simulate(request SimulationRequest) (readRequest *MailboxMessageHeader, err error)
}

type SequencerNetwork interface {
	SendMailboxMessage(recipient compose.ChainID, msg MailboxMessage)
	SendVote(vote bool)
}

type SequencerInstance struct {
	mu sync.Mutex

	// Dependencies
	execution ExecutionEngine
	network   SequencerNetwork

	// Protocol state
	state         SequencerState
	decisionState compose.DecisionState

	// List of transactions to be executed by this chain.
	txs []compose.Transaction
	// Read requests made by the transactions. Removed on fulfillment.
	expectedReadRequests []MailboxMessageHeader
	// Incoming mailbox messages that can be used to satisfy expected reads.
	pendingMessages []MailboxMessage
	// Consumed pendingMessages that should populate the Mailbox contract
	// before the next transaction simulation.
	putInboxMessages []MailboxMessage
	// Identifies the VM state for the simulation
	vmSnapshot compose.StateRoot
}

func NewSequencerInstance(
	instance compose.Instance,
	execution ExecutionEngine,
	network SequencerNetwork,
	vmSnapshot compose.StateRoot,
) (*SequencerInstance, error) {
	// Build runner
	r := &SequencerInstance{
		mu:                   sync.Mutex{},
		execution:            execution,
		network:              network,
		state:                SeqStateSimulating, // First state
		decisionState:        compose.DecisionStatePending,
		txs:                  make([]compose.Transaction, 0),
		putInboxMessages:     make([]MailboxMessage, 0),
		expectedReadRequests: make([]MailboxMessageHeader, 0),
		pendingMessages:      make([]MailboxMessage, 0),
		vmSnapshot:           vmSnapshot,
	}

	// Filter transactions to this chain
	r.txs = make([]compose.Transaction, 0)
	for _, tx := range instance.XTRequest {
		if tx.ChainID() == r.execution.ChainID() {
			r.txs = append(r.txs, tx)
		}
	}
	if len(r.txs) == 0 {
		return nil, ErrNoTransactions
	}

	return r, nil
}

// Run executes calls the mailbox-aware simulation.
// If simulation succeeds, it sends Vote(true) to the SP and set state to waiting for decided.
// If simulation fails due to read miss, it adds the expected read message and looks for new reads to insert.
// If simulation fails for other reasons, it sends Vote(false) and terminates.
func (r *SequencerInstance) Run() error {
	r.mu.Lock()
	if r.state != SeqStateSimulating {
		r.mu.Unlock()
		return errors.New("sequencer is not in simulating state")
	}

	// Run simulation
	readRequest, err := r.execution.Simulate(SimulationRequest{
		PutInboxMessages: append([]MailboxMessage(nil), r.putInboxMessages...),
		Transactions:     append([]compose.Transaction(nil), r.txs...),
		Snapshot:         r.vmSnapshot,
	})
	if err != nil {
		r.network.SendVote(false)
		r.state = SeqStateDone
		r.decisionState = compose.DecisionStateRejected
		r.mu.Unlock()
		return errors.New("unknown simulation failure")
	}

	// Consume mailbox messages.
	if readRequest != nil {
		r.expectedReadRequests = append(r.expectedReadRequests, *readRequest)
		r.mu.Unlock()
		return r.consumeReceivedMailboxMessagesAndSimulate()
	}

	// Vote true.
	r.network.SendVote(true)
	r.state = SeqStateWaitingDecided
	r.mu.Unlock()
	return nil
}

// consumeReceivedMailboxMessagesAndSimulate checks if any expected read mailbox messages have been received
// If so, remove from the lists, and call runSimulation
func (r *SequencerInstance) consumeReceivedMailboxMessagesAndSimulate() error {
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
		return r.Run()
	}
	return nil
}

// ProcessMailboxMessage processes an incoming mailbox message
func (r *SequencerInstance) ProcessMailboxMessage(msg MailboxMessage) error {
	r.mu.Lock()
	if r.state != SeqStateSimulating {
		// TODO log that message is ignored
		r.mu.Unlock()
		return nil
	}

	r.pendingMessages = append(r.pendingMessages, msg)
	r.mu.Unlock()
	return r.consumeReceivedMailboxMessagesAndSimulate()
}

// ProcessDecidedMessage receives a decided message from the SP
func (r *SequencerInstance) ProcessDecidedMessage(decided bool) error {
	r.mu.Lock()

	if r.state == SeqStateDone {
		// TODO log that message is ignored as it has already decided
		r.mu.Unlock()
		return nil
	}

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
// If not already in waiting for decided or done state, terminates as rejected and sends Vote(false) to SP.
func (r *SequencerInstance) Timeout() {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.state == SeqStateWaitingDecided || r.state == SeqStateDone {
		return
	}

	r.state = SeqStateDone
	r.decisionState = compose.DecisionStateRejected
	r.network.SendVote(false)
}
