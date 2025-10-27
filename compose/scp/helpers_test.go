package scp

import (
	"compose"
)

// fakeTx is a minimal Transaction implementation for tests.
type fakeTx struct {
	chain compose.ChainID
	name  string
}

func (t fakeTx) ChainID() compose.ChainID { return t.chain }
func (t fakeTx) Bytes() []byte            { return []byte(t.name) }

// fakePublisherNetwork records calls for assertions.
type fakePublisherNetwork struct {
	startCalled   int
	startInstance compose.Instance
	startXT       []compose.Transaction

	decidedCalled int
	decidedValues []bool
}

func (f *fakePublisherNetwork) SendStartInstance(
	instance compose.Instance,
	xTRequest []compose.Transaction,
) {
	f.startCalled++
	f.startInstance = instance
	f.startXT = append([]compose.Transaction(nil), xTRequest...)
}

func (f *fakePublisherNetwork) SendDecided(decided bool) {
	f.decidedCalled++
	f.decidedValues = append(f.decidedValues, decided)
}

// simulateResp encodes a single response step for the fake engine.
type simulateResp struct {
	read *MailboxMessageHeader
	err  error
}

// fakeExecutionEngine implements ExecutionEngine with scripted responses.
type fakeExecutionEngine struct {
	id      compose.ChainID
	steps   []simulateResp
	calls   int
	lastReq SimulationRequest
}

func (e *fakeExecutionEngine) ChainID() compose.ChainID { return e.id }

func (e *fakeExecutionEngine) Simulate(req SimulationRequest) (*MailboxMessageHeader, error) {
	e.lastReq = req
	if e.calls < len(e.steps) {
		s := e.steps[e.calls]
		e.calls++
		return s.read, s.err
	}
	// Default: success, no read
	e.calls++
	return nil, nil
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
