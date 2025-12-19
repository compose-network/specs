package scp

import (
	"testing"

	"github.com/compose-network/specs/compose"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func makeMsg(
	src compose.ChainID,
	label string,
	data []byte,
) MailboxMessage {
	return MailboxMessage{
		MailboxMessageHeader: MailboxMessageHeader{
			SourceChainID: src,
			DestChainID:   compose.ChainID(1),
			Sender:        compose.EthAddress{1},
			Receiver:      compose.EthAddress{2},
			SessionID:     compose.SessionID(1),
			Label:         label,
		},
		Data: append([]byte(nil), data...),
	}
}

func requireSequencerImpl(t *testing.T, seq SequencerInstance) *sequencerInstance {
	t.Helper()
	impl, ok := seq.(*sequencerInstance)
	require.True(t, ok, "expected *sequencerInstance, got %T", seq)
	return impl
}

func TestSequencer_VoteTrueOnImmediateSuccess(t *testing.T) {
	eng := &fakeExecutionEngine{id: 1, steps: []simulateResp{ /* default success */ }}
	net := &fakeSequencerNetwork{}
	inst := compose.Instance{
		XTRequest: compose.XTRequest{
			Transactions: []compose.TransactionRequest{
				{ChainID: 1, Transactions: [][]byte{[]byte("a")}},
				{ChainID: 2, Transactions: [][]byte{[]byte("b")}},
			},
		},
	}

	seq, err := NewSequencerInstance(inst, eng, net, compose.StateRoot{9}, testLogger())
	require.NoError(t, err)
	require.NoError(t, seq.Run())
	impl := requireSequencerImpl(t, seq)

	if assert.Len(t, net.votes, 1) {
		assert.True(t, net.votes[0])
	}

	// After success it should still be pending until a decided message arrives.
	assert.Equal(t, compose.DecisionStatePending, seq.DecisionState())
	assert.Equal(t, SeqStateWaitingDecided, impl.state)

	// Decided true moves to done accepted
	require.NoError(t, seq.ProcessDecidedMessage(true))
	assert.Equal(t, compose.DecisionStateAccepted, seq.DecisionState())
	assert.Equal(t, SeqStateDone, impl.state)

	// Subsequent decided is ignored
	require.NoError(t, seq.ProcessDecidedMessage(false))
	assert.Equal(t, compose.DecisionStateAccepted, seq.DecisionState())
}

func TestSequencer_ReadThenMailboxThenSuccess(t *testing.T) {
	// First call returns a read request, second call success.
	need := makeMsg(compose.ChainID(2), "X", []byte("d1"))
	eng := &fakeExecutionEngine{
		id:    1,
		steps: []simulateResp{{read: &need.MailboxMessageHeader}, {read: nil}},
	}
	net := &fakeSequencerNetwork{}
	inst := compose.Instance{
		XTRequest: compose.XTRequest{
			Transactions: []compose.TransactionRequest{
				{ChainID: 1, Transactions: [][]byte{[]byte("a")}},
			},
		},
	}

	seq, err := NewSequencerInstance(inst, eng, net, compose.StateRoot{}, testLogger())
	require.NoError(t, err)
	require.NoError(t, seq.Run())

	assert.Empty(t, net.votes)

	require.NoError(t, seq.ProcessMailboxMessage(need))

	if assert.Len(t, net.votes, 1) {
		assert.True(t, net.votes[0])
	}
}

func TestSequencer_MultipleReadsOutOfOrder(t *testing.T) {
	a := makeMsg(compose.ChainID(2), "A", []byte("a"))
	b := makeMsg(compose.ChainID(3), "B", []byte("b"))
	// Simulation: need A, then need B, then success.
	eng := &fakeExecutionEngine{
		id: 1,
		steps: []simulateResp{
			{read: &a.MailboxMessageHeader},
			{read: &b.MailboxMessageHeader},
			{read: nil},
		},
	}
	net := &fakeSequencerNetwork{}
	inst := compose.Instance{
		XTRequest: compose.XTRequest{
			Transactions: []compose.TransactionRequest{
				{ChainID: 1, Transactions: [][]byte{[]byte("x")}},
			},
		},
	}

	seq, err := NewSequencerInstance(inst, eng, net, compose.StateRoot{}, testLogger())
	require.NoError(t, err)
	require.NoError(t, seq.Run())

	// Deliver B first (out of order) — should not trigger until A arrives.
	require.NoError(t, seq.ProcessMailboxMessage(b))
	assert.Empty(t, net.votes)

	// Now deliver A; engine will ask for B, which is already buffered; it should resimulate and vote true
	require.NoError(t, seq.ProcessMailboxMessage(a))

	if assert.Len(t, net.votes, 1) {
		assert.True(t, net.votes[0])
	}
}

func TestSequencer_TimeoutBeforeVoteSendsFalse(t *testing.T) {
	// Engine keeps asking for one read, so we remain simulating
	need := makeMsg(compose.ChainID(2), "NEED", nil)
	eng := &fakeExecutionEngine{id: 1, steps: []simulateResp{{read: &need.MailboxMessageHeader}}}
	net := &fakeSequencerNetwork{}
	inst := compose.Instance{
		XTRequest: compose.XTRequest{
			Transactions: []compose.TransactionRequest{
				{ChainID: 1, Transactions: [][]byte{[]byte("x")}},
			},
		},
	}

	seq, err := NewSequencerInstance(inst, eng, net, compose.StateRoot{}, testLogger())
	require.NoError(t, err)
	require.NoError(t, seq.Run())
	assert.Equal(t, compose.DecisionStatePending, seq.DecisionState())
	impl := requireSequencerImpl(t, seq)
	assert.Equal(t, SeqStateSimulating, impl.state)

	seq.Timeout()
	if assert.Len(t, net.votes, 1) {
		assert.False(t, net.votes[0])
	}
	assert.Equal(t, compose.DecisionStateRejected, seq.DecisionState())
	assert.Equal(t, SeqStateDone, impl.state)

	// Now mailbox is ignored
	require.NoError(t, seq.ProcessMailboxMessage(need))
}

func TestSequencer_TimeoutWithMultipleUnfulfilledReads(t *testing.T) {
	const session compose.SessionID = 100

	msgA := makeMsg(compose.ChainID(2), "labelA", nil)
	msgA.SessionID = session
	msgB := makeMsg(compose.ChainID(3), "labelB", nil)
	msgB.SessionID = session

	// Simulation: need A, then need B (both will remain unfulfilled)
	eng := &fakeExecutionEngine{
		id: 1,
		steps: []simulateResp{
			{read: &msgA.MailboxMessageHeader},
			{read: &msgB.MailboxMessageHeader},
		},
	}
	net := &fakeSequencerNetwork{}
	inst := compose.Instance{
		XTRequest: compose.XTRequest{
			Transactions: []compose.TransactionRequest{
				{ChainID: 1, Transactions: [][]byte{[]byte("tx")}},
			},
		},
	}

	seq, err := NewSequencerInstance(inst, eng, net, compose.StateRoot{}, testLogger())
	require.NoError(t, err)
	require.NoError(t, seq.Run())

	impl := requireSequencerImpl(t, seq)

	// After the first simulation, we should have one expected read request (A)
	assert.Equal(t, SeqStateSimulating, impl.state)
	assert.Len(t, impl.expectedReadRequests, 1)
	assert.Equal(t, msgA.MailboxMessageHeader, impl.expectedReadRequests[0])

	// No votes yet
	assert.Empty(t, net.votes)

	// Timeout should reject and vote false
	seq.Timeout()

	// Verify state transitions
	assert.Equal(t, SeqStateDone, impl.state)
	assert.Equal(t, compose.DecisionStateRejected, seq.DecisionState())

	// Verify vote(false) was sent
	if assert.Len(t, net.votes, 1) {
		assert.False(t, net.votes[0])
	}

	// The expectedReadRequests should still contain the unfulfilled read (for logging)
	assert.Len(t, impl.expectedReadRequests, 1)
	req := impl.expectedReadRequests[0]
	assert.Equal(t, compose.ChainID(2), req.SourceChainID)
	assert.Equal(t, compose.ChainID(1), req.DestChainID)
	assert.Equal(t, compose.SessionID(100), req.SessionID)
	assert.Equal(t, "labelA", req.Label)
}

func TestSequencer_TimeoutAfterWaitingDecidedIgnored(t *testing.T) {
	// Simulation succeeds immediately
	eng := &fakeExecutionEngine{id: 1, steps: []simulateResp{}}
	net := &fakeSequencerNetwork{}
	inst := compose.Instance{
		XTRequest: compose.XTRequest{
			Transactions: []compose.TransactionRequest{
				{ChainID: 1, Transactions: [][]byte{[]byte("tx")}},
			},
		},
	}

	seq, err := NewSequencerInstance(inst, eng, net, compose.StateRoot{}, testLogger())
	require.NoError(t, err)
	require.NoError(t, seq.Run())

	impl := requireSequencerImpl(t, seq)

	// Should have voted true and be waiting for decision
	assert.Equal(t, SeqStateWaitingDecided, impl.state)
	if assert.Len(t, net.votes, 1) {
		assert.True(t, net.votes[0])
	}

	// Timeout should be ignored when in the WaitingDecided state
	seq.Timeout()

	// State should remain unchanged
	assert.Equal(t, SeqStateWaitingDecided, impl.state)
	assert.Equal(t, compose.DecisionStatePending, seq.DecisionState())

	// No additional votes should be sent
	assert.Len(t, net.votes, 1)
}

func TestSequencer_SimulationErrorVotesFalse(t *testing.T) {
	boom := simulateResp{read: nil, err: sentinelError("boom")}
	eng := &fakeExecutionEngine{id: 1, steps: []simulateResp{boom}}
	net := &fakeSequencerNetwork{}
	inst := compose.Instance{
		XTRequest: compose.XTRequest{
			Transactions: []compose.TransactionRequest{
				{ChainID: 1, Transactions: [][]byte{[]byte("x")}},
			},
		},
	}

	seq, err := NewSequencerInstance(inst, eng, net, compose.StateRoot{}, testLogger())
	require.NoError(t, err)
	// Run fails with a stable error string; still sends a vote(false).
	errRun := seq.Run()
	require.EqualError(t, errRun, "simulating sequencer failed: boom")
	if assert.Len(t, net.votes, 1) {
		assert.False(t, net.votes[0])
	}
}

// sentinelError is a sentinel error implementing error equality by message.
type sentinelError string

func (e sentinelError) Error() string { return string(e) }

func TestSequencer_FiltersTransactionsByChainID(t *testing.T) {
	eng := &fakeExecutionEngine{id: 42, steps: []simulateResp{}}
	net := &fakeSequencerNetwork{}
	inst := compose.Instance{
		XTRequest: compose.XTRequest{
			Transactions: []compose.TransactionRequest{
				{ChainID: 42, Transactions: [][]byte{[]byte("mine1"), []byte("mine2")}},
				{ChainID: 7, Transactions: [][]byte{[]byte("other")}},
			},
		},
	}

	seq, err := NewSequencerInstance(inst, eng, net, compose.StateRoot{}, testLogger())
	require.NoError(t, err)
	require.NoError(t, seq.Run())

	// Simulate should have only received my chain's transactions
	got := eng.lastReq.Transactions
	gotNames := make([]string, 0, len(got))
	for _, tr := range got {
		gotNames = append(gotNames, string(tr))
	}
	assert.Equal(t, []string{"mine1", "mine2"}, gotNames)
}

func TestSequencer_NoTransactions_ReturnsErrNoTransactions(t *testing.T) {
	eng := &fakeExecutionEngine{id: compose.ChainID(42)}
	net := &fakeSequencerNetwork{}
	// Only transactions for another chain
	inst := compose.Instance{
		XTRequest: compose.XTRequest{
			Transactions: []compose.TransactionRequest{
				{ChainID: 7, Transactions: [][]byte{[]byte("other")}},
			},
		},
	}

	seq, err := NewSequencerInstance(inst, eng, net, compose.StateRoot{}, testLogger())
	require.ErrorIs(t, err, ErrNoTransactions)
	assert.Nil(t, seq)
	assert.Empty(t, net.votes)
}
