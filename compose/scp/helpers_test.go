package scp

import (
	"github.com/compose-network/specs/compose"
)

// fakePublisherNetwork records calls for assertions.
type fakePublisherNetwork struct {
	startCalled   int
	startInstance compose.Instance
	startXT       compose.XTRequest

	decidedCalled int
	decisions     []struct {
		ID    compose.InstanceID
		Value bool
	}
}

func (f *fakePublisherNetwork) SendStartInstance(instance compose.Instance) {
	f.startCalled++
	f.startInstance = instance
	f.startXT = cloneXTRequest(instance.XTRequest)
}

func (f *fakePublisherNetwork) SendDecided(id compose.InstanceID, decided bool) {
	f.decidedCalled++
	f.decisions = append(f.decisions, struct {
		ID    compose.InstanceID
		Value bool
	}{ID: id, Value: decided})
}

// simulateResp encodes a single response step for the fake engine.
type simulateResp struct {
	read  *MailboxMessageHeader
	write []MailboxMessage
	err   error
}

// fakeExecutionEngine implements ExecutionEngine with scripted responses.
type fakeExecutionEngine struct {
	id      compose.ChainID
	steps   []simulateResp
	calls   int
	lastReq SimulationRequest
}

func (e *fakeExecutionEngine) ChainID() compose.ChainID { return e.id }

func (e *fakeExecutionEngine) Simulate(req SimulationRequest) (*MailboxMessageHeader, []MailboxMessage, error) {
	e.lastReq = SimulationRequest{
		PutInboxMessages: append([]MailboxMessage(nil), req.PutInboxMessages...),
		Transactions:     cloneByteSlices(req.Transactions),
		Snapshot:         req.Snapshot,
	}
	if e.calls < len(e.steps) {
		s := e.steps[e.calls]
		e.calls++
		writeCopy := append([]MailboxMessage(nil), s.write...)
		return s.read, writeCopy, s.err
	}
	// Default: success, no read
	e.calls++
	return nil, nil, nil
}

// fakeSequencerNetwork collects votes and mailbox messages.
type fakeSequencerNetwork struct {
	mailboxSent []struct {
		to  compose.ChainID
		msg MailboxMessage
	}
	votes []bool
}

func (n *fakeSequencerNetwork) SendMailboxMessage(recipient compose.ChainID, msg MailboxMessage) {
	n.mailboxSent = append(n.mailboxSent, struct {
		to  compose.ChainID
		msg MailboxMessage
	}{recipient, msg})
}

func (n *fakeSequencerNetwork) SendVote(v bool) {
	n.votes = append(n.votes, v)
}

func cloneXTRequest(req compose.XTRequest) compose.XTRequest {
	out := compose.XTRequest{
		Transactions: make([]compose.TransactionRequest, len(req.Transactions)),
	}
	for i, tr := range req.Transactions {
		out.Transactions[i] = compose.TransactionRequest{
			ChainID:      tr.ChainID,
			Transactions: cloneByteSlices(tr.Transactions),
		}
	}
	return out
}
