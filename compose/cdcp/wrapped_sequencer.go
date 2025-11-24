package cdcp

import (
	"errors"
	"fmt"
	"sync"

	"github.com/compose-network/specs/compose"
	"github.com/compose-network/specs/compose/scp"
	"github.com/rs/zerolog"
)

var (
	ErrNoTransactions       = errors.New("no transactions to execute")
	ErrNotInSimulatingState = errors.New("sequencer not in simulating state")
	ErrDuplicateWSDecided   = errors.New("duplicate WS decided")
)

// WrappedSequencerInstance is an interface that represents the wrapped-sequencer logic for a CDCP instance.
// Wrapped sequencer (WS) isn't a real sequencer, but rather a representative from an external rollup (ER).
type WrappedSequencerInstance interface {
	DecisionState() compose.DecisionState
	Run() error
	ProcessMailboxMessage(msg scp.MailboxMessage) error
	ProcessNativeDecidedMessage(decided bool) error
	Timeout()
}

// WSState tracks the state machine for a WS in a CDCP session.
type WSState int

const (
	WSStateSimulating WSState = iota
	WSStateWaitingNativeDecided
	WSStateWaitingERResponse
	WSStateDone
)

// SafeExecuteArguments represents the arguments for the `safe_execute` solidity function.
type SafeExecuteArguments struct {
	// mailbox.putInbox transactions
	PutInboxMessages []scp.MailboxMessage
	// mailbox.putOutbox transactions
	PutOutboxMessages []scp.MailboxMessage
	Transactions      [][]byte
}

// WSSimulationRequest represents the inputs for a mailbox-aware simulation
// for a safe_execute function.
type WSSimulationRequest struct {
	SafeExecuteArguments
	Snapshot compose.StateRoot
}

// WSSimulationResponse represents the response of a simulation by WSExecutionEngine
// The simulation returns:
// - ReadMiss (!= nil) if it tried to read, but failed due to missing message.
// - WriteMiss (!= nil) if it tried to write a message, but failed because it hasn't been pre-populated yet.
// - WrittenMessages: a list of successfully written mailbox messages.
// - Err: != nil if a different VM error occurred.
// Success case: if err is nil and there's no ReadMiss or WriteMiss.
type WSSimulationResponse struct {
	ReadMiss        *scp.MailboxMessageHeader
	WriteMiss       *scp.MailboxMessage
	WrittenMessages []scp.MailboxMessage
	Err             error
}

// WSExecutionEngine represents the execution engine for the WS, such as the EVM.
type WSExecutionEngine interface {
	ChainID() compose.ChainID
	// Simulate runs the VM, simulating a safe_execute transaction with a mailbox-aware tracer.
	Simulate(request WSSimulationRequest) WSSimulationResponse
}

// ERClient represents the connection to the ER client
// with an API to submit a transaction.
// It returns error == nil if it is successful and will be included.
type ERClient interface {
	SubmitTransaction(seArgs SafeExecuteArguments) error
}

type WSNetwork interface {
	SendMailboxMessage(recipient compose.ChainID, msg scp.MailboxMessage)
	SendWSDecidedMessage(decided bool)
}

type wsInstance struct {
	mu sync.Mutex

	// Dependencies
	execution WSExecutionEngine
	network   WSNetwork
	erClient  ERClient

	// Protocol state
	state         WSState
	decisionState compose.DecisionState
	nativeDecided *bool

	// List of transactions to be executed by this chain (from the request)
	txs [][]byte
	// Read requests made by the transactions (returned by simulations). Removed on fulfillment.
	expectedReadRequests []scp.MailboxMessageHeader
	// Incoming mailbox messages that can be used to satisfy expected reads.
	pendingMessages []scp.MailboxMessage
	// Consumed pendingMessages that should populate the Mailbox contract
	// before the next transaction simulation.
	putInboxMessages []scp.MailboxMessage
	// Identifies the VM state for the simulation
	vmSnapshot compose.StateRoot

	// Write messages to be prepopulated
	writePrePopulationMessages []scp.MailboxMessage
	// Cache of successfully written messages
	writtenMessagesCache []scp.MailboxMessage

	logger zerolog.Logger
}

func NewWrappedSequencerInstance(
	instance compose.Instance,
	execution WSExecutionEngine,
	network WSNetwork,
	erClient ERClient,
	vmSnapshot compose.StateRoot,
	logger zerolog.Logger,
) (WrappedSequencerInstance, error) {
	// Build runner
	r := &wsInstance{
		mu:                         sync.Mutex{},
		execution:                  execution,
		network:                    network,
		erClient:                   erClient,
		state:                      WSStateSimulating, // First state
		decisionState:              compose.DecisionStatePending,
		nativeDecided:              nil,
		txs:                        make([][]byte, 0),
		putInboxMessages:           make([]scp.MailboxMessage, 0),
		expectedReadRequests:       make([]scp.MailboxMessageHeader, 0),
		pendingMessages:            make([]scp.MailboxMessage, 0),
		vmSnapshot:                 vmSnapshot,
		writePrePopulationMessages: make([]scp.MailboxMessage, 0),
		writtenMessagesCache:       make([]scp.MailboxMessage, 0),
		logger:                     logger,
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

func (ws *wsInstance) DecisionState() compose.DecisionState {
	ws.mu.Lock()
	defer ws.mu.Unlock()
	return ws.decisionState
}

// Run executes calls to the mailbox-aware simulation.
// If simulation succeeds, it sends Vote(true) to the SP and set state to waiting for decided.
// If simulation fails due to read miss, it adds the expected read message and looks for new reads to insert.
// If simulation fails for other reasons, it sends Vote(false) and terminates.
func (ws *wsInstance) Run() error {
	ws.mu.Lock()
	if ws.state != WSStateSimulating {
		ws.mu.Unlock()
		return ErrNotInSimulatingState
	}

	// Run simulation
	response := ws.execution.Simulate(WSSimulationRequest{
		SafeExecuteArguments: SafeExecuteArguments{
			PutInboxMessages:  append([]scp.MailboxMessage(nil), ws.putInboxMessages...),
			PutOutboxMessages: append([]scp.MailboxMessage(nil), ws.writePrePopulationMessages...),
			Transactions:      compose.CloneByteSlices(ws.txs),
		},
		Snapshot: ws.vmSnapshot,
	})
	if response.Err != nil {

		ws.logger.Info().Msg("Simulation failed, rejecting instance. Error: " + response.Err.Error())

		ws.network.SendWSDecidedMessage(false)
		ws.state = WSStateDone
		ws.decisionState = compose.DecisionStateRejected
		ws.mu.Unlock()

		return fmt.Errorf("simulating sequencer failed: %w", response.Err)
	}

	// Send write messages
	ws.sendWriteMessages(response.WrittenMessages)

	// Consume mailbox messages.
	if response.ReadMiss != nil {
		ws.logger.Info().
			Uint64("source_chain_id", uint64(response.ReadMiss.SourceChainID)).
			Str("label", response.ReadMiss.Label).
			Msg("Simulation hit read miss, requesting mailbox message.")
		ws.expectedReadRequests = append(ws.expectedReadRequests, *response.ReadMiss)
		ws.mu.Unlock()
		return ws.consumeReceivedMailboxMessagesAndSimulate()
	}

	// If there was a write miss, add it to prepopulation and run again.
	if response.WriteMiss != nil {
		ws.writePrePopulationMessages = append(ws.writePrePopulationMessages, *response.WriteMiss)
		ws.logger.Info().
			Uint64("destination_chain_id", uint64(response.WriteMiss.DestChainID)).
			Str("label", response.WriteMiss.Label).
			Msg("Simulation hit write miss, adding message to prepopulation list.")
		ws.mu.Unlock()
		return ws.Run()
	}

	// Set state to waiting for native decided, and attempt calling ER (if already received native decided)
	ws.logger.Info().Msg("Simulation succeeded, waiting for native decided.")
	ws.state = WSStateWaitingNativeDecided
	ws.mu.Unlock()
	ws.attemptERCall()
	return nil
}

func (ws *wsInstance) sendWriteMessages(messages []scp.MailboxMessage) {
	for _, msg := range messages {
		// Check if belongs to cache
		alreadySent := false
		for _, cachedMsg := range ws.writtenMessagesCache {
			if cachedMsg.Equal(msg) {
				alreadySent = true
				break
			}
		}
		if alreadySent {
			continue
		}

		// Send and add to cache if new message
		ws.network.SendMailboxMessage(msg.MailboxMessageHeader.DestChainID, msg)
		ws.writtenMessagesCache = append(ws.writtenMessagesCache, msg)
	}
}

// attemptERCall checks if the state is WSStateWaitingNativeDecided and if a native decided was already received.
// If so and:
//   - native decided == true, it calls the ER and sends its WSDecided result.
//   - native decided == false, terminates
func (ws *wsInstance) attemptERCall() {
	ws.mu.Lock()

	// If not waiting for native decided, return
	if ws.state != WSStateWaitingNativeDecided {
		ws.mu.Unlock()
		return
	}

	// If it has not received native decided, return
	if ws.nativeDecided == nil {
		ws.mu.Unlock()
		return
	}

	// If native decided as false, terminate
	if !*ws.nativeDecided {
		ws.decisionState = compose.DecisionStateRejected
		ws.state = WSStateDone
		ws.logger.Info().Msg("Native decided as false, rejecting instance")
		ws.mu.Unlock()
		return
	}

	// If native decided as true, calls ER.
	// Call it without lock, to free-up state.
	ws.state = WSStateWaitingERResponse
	ws.mu.Unlock()
	err := ws.erClient.SubmitTransaction(SafeExecuteArguments{
		PutInboxMessages:  append([]scp.MailboxMessage(nil), ws.putInboxMessages...),
		PutOutboxMessages: append([]scp.MailboxMessage(nil), ws.writePrePopulationMessages...),
		Transactions:      ws.txs,
	})
	ws.mu.Lock()

	// If it fails, send WSDecided as false and terminates
	if err != nil {
		ws.logger.Info().Err(err).Msg("ER call failed. Sending WSDecided as false.")
		ws.network.SendWSDecidedMessage(false)
		ws.state = WSStateDone
		ws.decisionState = compose.DecisionStateRejected
		ws.mu.Unlock()
		return
	}

	// Else, sends successful decision, and terminates
	ws.logger.Info().Err(err).Msg("ER call succeeded. Sending WSDecided as true.")
	ws.network.SendWSDecidedMessage(true)
	ws.state = WSStateDone
	ws.decisionState = compose.DecisionStateAccepted
	ws.mu.Unlock()
}

// consumeReceivedMailboxMessagesAndSimulate checks if any expected read mailbox messages have been received
// If so, remove from the lists, and call run to simulate
func (ws *wsInstance) consumeReceivedMailboxMessagesAndSimulate() error {
	ws.mu.Lock()
	includedAny := false

	// Loop through expected messages
	for idx := 0; idx < len(ws.expectedReadRequests); {
		expectedMsg := ws.expectedReadRequests[idx]
		matched := false
		// Look if it exists in received messages
		for receivedMsgIdx, receivedMsg := range ws.pendingMessages {
			if receivedMsg.MailboxMessageHeader.Equal(expectedMsg) {
				// If found, add to mailboxOps
				ws.putInboxMessages = append(ws.putInboxMessages, receivedMsg)
				// Remove from lists
				ws.expectedReadRequests = append(
					ws.expectedReadRequests[:idx],
					ws.expectedReadRequests[idx+1:]...)
				ws.pendingMessages = append(
					ws.pendingMessages[:receivedMsgIdx],
					ws.pendingMessages[receivedMsgIdx+1:]...)
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

	ws.mu.Unlock()
	if includedAny {
		ws.logger.Info().Msg("Consuming mailbox messages and re-simulating.")
		return ws.Run()
	}
	return nil
}

// ProcessMailboxMessage processes an incoming mailbox message
func (ws *wsInstance) ProcessMailboxMessage(msg scp.MailboxMessage) error {
	ws.mu.Lock()
	if ws.state != WSStateSimulating {
		ws.logger.Info().
			Uint64("source_chain_id", uint64(msg.MailboxMessageHeader.SourceChainID)).
			Str("label", msg.MailboxMessageHeader.Label).
			Msg("Ignoring mailbox message because not in simulating state")

		ws.mu.Unlock()
		return nil
	}

	ws.logger.Info().
		Uint64("source_chain_id", uint64(msg.MailboxMessageHeader.SourceChainID)).
		Str("label", msg.MailboxMessageHeader.Label).
		Msg("Adding mailbox message to pending list")

	ws.pendingMessages = append(ws.pendingMessages, msg)
	ws.mu.Unlock()
	return ws.consumeReceivedMailboxMessagesAndSimulate()
}

// ProcessNativeDecidedMessage receives a native decided message from the SP
func (ws *wsInstance) ProcessNativeDecidedMessage(decided bool) error {
	ws.mu.Lock()

	if ws.state == WSStateDone {
		ws.logger.Info().
			Bool("received_native_decided", decided).
			Str("stored_decision", ws.decisionState.String()).
			Msg("Ignoring native decided message because already done")

		ws.mu.Unlock()
		return nil
	}

	if ws.nativeDecided != nil {
		ws.logger.Info().
			Bool("received_native_decided", decided).
			Bool("stored_native_decision", *ws.nativeDecided).
			Msg("Ignoring native decided because already received")
		ws.mu.Unlock()
		return ErrDuplicateWSDecided
	}

	ws.logger.Info().
		Bool("received_native_decided", decided).
		Msg("Processing native decided message from SP")

	ws.nativeDecided = &decided
	ws.mu.Unlock()
	ws.attemptERCall()
	return nil
}

// Timeout is invoked when the timer fires.
// If not already in waiting for ER response or done state, terminates as rejected and sends WSDecided(false) to SP.
func (ws *wsInstance) Timeout() {
	ws.mu.Lock()
	defer ws.mu.Unlock()

	if ws.state == WSStateWaitingERResponse || ws.state == WSStateDone {
		ws.logger.Info().
			Msg("Ignoring timeout because already waiting for ER or done")
		return
	}

	ws.logger.Info().
		Msg("Timeout occurred, rejecting instance")

	ws.state = WSStateDone
	ws.decisionState = compose.DecisionStateRejected
	ws.network.SendWSDecidedMessage(false)
}
