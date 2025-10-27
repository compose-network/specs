package scp

import (
	"compose"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func makeMsg(
	src, dst compose.ChainID,
	session compose.SessionID,
	label string,
	data []byte,
) MailboxMessage {
	return MailboxMessage{
		MailboxMessageHeader: MailboxMessageHeader{
			SourceChainID: src,
			DestChainID:   dst,
			Sender:        compose.EthAddress{1},
			Receiver:      compose.EthAddress{2},
			SessionID:     session,
			Label:         label,
		},
		Data: append([]byte(nil), data...),
	}
}

func TestSequencer_VoteTrueOnImmediateSuccess(t *testing.T) {
	eng := &fakeExecutionEngine{id: 1, steps: []simulateResp{ /* default success */ }}
	net := &fakeSequencerNetwork{}
	inst := compose.Instance{
		XTRequest: []compose.Transaction{fakeTx{chain: 1, name: "a"}, fakeTx{chain: 2, name: "b"}},
	}

	seq, err := NewSequencerInstance(inst, eng, net, compose.StateRoot{9})
	require.NoError(t, err)
	require.NoError(t, seq.Run())

	if assert.Len(t, net.votes, 1) {
		assert.True(t, net.votes[0])
	}

	// After success it should be waiting for decided
	assert.Equal(t, SeqStateWaitingDecided, seq.state)

	// Decided true moves to done accepted
	require.NoError(t, seq.ProcessDecidedMessage(true))
	assert.Equal(t, SeqStateDone, seq.state)
	assert.Equal(t, compose.DecisionStateAccepted, seq.decisionState)

	// Subsequent decided is ignored
	require.NoError(t, seq.ProcessDecidedMessage(false))
}

func TestSequencer_ReadThenMailboxThenSuccess(t *testing.T) {
	// First call returns a read request, second call success.
	need := makeMsg(compose.ChainID(2), compose.ChainID(1), compose.SessionID(1), "X", []byte("d1"))
	eng := &fakeExecutionEngine{
		id:    1,
		steps: []simulateResp{{read: &need.MailboxMessageHeader}, {read: nil}},
	}
	net := &fakeSequencerNetwork{}
	inst := compose.Instance{XTRequest: []compose.Transaction{fakeTx{chain: 1, name: "a"}}}

	seq, err := NewSequencerInstance(inst, eng, net, compose.StateRoot{})
	require.NoError(t, err)
	require.NoError(t, seq.Run())

	assert.Len(t, net.votes, 0)

	require.NoError(t, seq.ProcessMailboxMessage(need))

	if assert.Len(t, net.votes, 1) {
		assert.True(t, net.votes[0])
	}
}

func TestSequencer_MultipleReadsOutOfOrder(t *testing.T) {
	a := makeMsg(compose.ChainID(2), compose.ChainID(1), compose.SessionID(1), "A", []byte("a"))
	b := makeMsg(compose.ChainID(3), compose.ChainID(1), compose.SessionID(1), "B", []byte("b"))
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
	inst := compose.Instance{XTRequest: []compose.Transaction{fakeTx{chain: 1, name: "x"}}}

	seq, err := NewSequencerInstance(inst, eng, net, compose.StateRoot{})
	require.NoError(t, err)
	require.NoError(t, seq.Run())

	// Deliver B first (out of order) — should not trigger until A arrives.
	require.NoError(t, seq.ProcessMailboxMessage(b))
	assert.Len(t, net.votes, 0)

	// Now deliver A; engine will ask for B, which is already buffered; it should resimulate and vote true
	require.NoError(t, seq.ProcessMailboxMessage(a))

	if assert.Len(t, net.votes, 1) {
		assert.True(t, net.votes[0])
	}
}

func TestSequencer_TimeoutBeforeVoteSendsFalse(t *testing.T) {
	// Engine keeps asking for one read, so we remain simulating
	need := makeMsg(compose.ChainID(2), compose.ChainID(1), compose.SessionID(1), "NEED", nil)
	eng := &fakeExecutionEngine{id: 1, steps: []simulateResp{{read: &need.MailboxMessageHeader}}}
	net := &fakeSequencerNetwork{}
	inst := compose.Instance{XTRequest: []compose.Transaction{fakeTx{chain: 1, name: "x"}}}

	seq, err := NewSequencerInstance(inst, eng, net, compose.StateRoot{})
	require.NoError(t, err)
	require.NoError(t, seq.Run())
	assert.Equal(t, SeqStateSimulating, seq.state)

	seq.Timeout()
	if assert.Len(t, net.votes, 1) {
		assert.False(t, net.votes[0])
	}
	assert.Equal(t, SeqStateDone, seq.state)
	assert.Equal(t, compose.DecisionStateRejected, seq.decisionState)

	// Now mailbox is ignored
	require.NoError(t, seq.ProcessMailboxMessage(need))
}

func TestSequencer_SimulationErrorVotesFalse(t *testing.T) {
	boom := simulateResp{read: nil, err: assertErr("boom")}
	eng := &fakeExecutionEngine{id: 1, steps: []simulateResp{boom}}
	net := &fakeSequencerNetwork{}
	inst := compose.Instance{XTRequest: []compose.Transaction{fakeTx{chain: 1, name: "x"}}}

	seq, err := NewSequencerInstance(inst, eng, net, compose.StateRoot{})
	require.NoError(t, err)
	// Run fails with a stable error string; still sends a vote(false).
	errRun := seq.Run()
	require.EqualError(t, errRun, "unknown simulation failure")
	if assert.Len(t, net.votes, 1) {
		assert.False(t, net.votes[0])
	}
}

// assertErr is a sentinel error implementing error equality by message.
type assertErr string

func (e assertErr) Error() string { return string(e) }

func TestSequencer_FiltersTransactionsByChainID(t *testing.T) {
	eng := &fakeExecutionEngine{id: 42, steps: []simulateResp{}}
	net := &fakeSequencerNetwork{}
	txs := []compose.Transaction{
		fakeTx{chain: 42, name: "mine1"},
		fakeTx{chain: 7, name: "other"},
		fakeTx{chain: 42, name: "mine2"},
	}
	inst := compose.Instance{XTRequest: txs}

	seq, err := NewSequencerInstance(inst, eng, net, compose.StateRoot{})
	require.NoError(t, err)
	require.NoError(t, seq.Run())

	// Simulate should have only received my chain's transactions
	got := eng.lastReq.Transactions
	var gotNames []string
	for _, tr := range got {
		gotNames = append(gotNames, tr.(fakeTx).name)
	}
	assert.Equal(t, []string{"mine1", "mine2"}, gotNames)
}

func TestSequencer_NoTransactions_ReturnsErrNoTransactions(t *testing.T) {
	eng := &fakeExecutionEngine{id: compose.ChainID(42)}
	net := &fakeSequencerNetwork{}
	// Only transactions for another chain
	inst := compose.Instance{XTRequest: []compose.Transaction{
		fakeTx{chain: compose.ChainID(7), name: "other"},
	}}

	seq, err := NewSequencerInstance(inst, eng, net, compose.StateRoot{})
	require.ErrorIs(t, err, ErrNoTransactions)
	assert.Nil(t, seq)
	assert.Len(t, net.votes, 0)
}
