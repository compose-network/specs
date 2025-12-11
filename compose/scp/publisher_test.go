package scp

import (
	"io"
	"testing"

	"github.com/rs/zerolog"

	"github.com/compose-network/specs/compose"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func testLogger() zerolog.Logger {
	return zerolog.New(io.Discard)
}

func txReq(chain compose.ChainID, payloads ...string) compose.TransactionRequest {
	data := make([][]byte, len(payloads))
	for i, p := range payloads {
		data[i] = []byte(p)
	}
	return compose.TransactionRequest{
		ChainID:      chain,
		Transactions: data,
	}
}

func TestPublisher_AllTrueVotesDecidesTrue(t *testing.T) {
	net := &fakePublisherNetwork{}
	inst := compose.Instance{
		ID:             compose.InstanceID{1},
		PeriodID:       7,
		SequenceNumber: 3,
		XTRequest: compose.XTRequest{
			Transactions: []compose.TransactionRequest{
				txReq(1, "a"),
				txReq(2, "b"),
			},
		},
	}

	pub, err := NewPublisherInstance(inst, net, testLogger())
	require.NoError(t, err)
	assert.Equal(t, inst, pub.Instance())
	assert.Equal(t, compose.DecisionStatePending, pub.DecisionState())

	// No start until Run is called
	assert.Equal(t, 0, net.startCalled, "unexpected start before Run")
	pub.Run()
	assert.Equal(t, 1, net.startCalled)
	assert.Equal(t, compose.DecisionStatePending, pub.DecisionState())

	// First vote true from chain 1
	require.NoError(t, pub.ProcessVote(compose.ChainID(1), true))
	assert.Equal(t, 0, net.decidedCalled, "should not decide yet")
	assert.Equal(t, compose.DecisionStatePending, pub.DecisionState())

	// Duplicate while still waiting should error with a stable message
	errDup := pub.ProcessVote(compose.ChainID(1), true)
	require.ErrorIs(t, errDup, ErrDuplicatedVote)
	assert.Equal(t, compose.DecisionStatePending, pub.DecisionState())

	// Second vote true from chain 2 triggers decide(true)
	require.NoError(t, pub.ProcessVote(compose.ChainID(2), true))
	assert.Equal(t, 1, net.decidedCalled)
	assert.Equal(t, compose.DecisionStateAccepted, pub.DecisionState())
	if assert.Len(t, net.decisions, 1) {
		assert.True(t, net.decisions[0].Value)
		assert.Equal(t, inst.ID, net.decisions[0].ID)
	}

	// Duplicate after decision is ignored
	require.NoError(t, pub.ProcessVote(compose.ChainID(1), true))

	// Vote after done is ignored (no extra decided)
	require.NoError(t, pub.ProcessVote(compose.ChainID(3), true))
	assert.Equal(t, 1, net.decidedCalled, "unexpected extra decided calls")
	assert.Equal(t, compose.DecisionStateAccepted, pub.DecisionState())
}

func TestPublisher_NonParticipantVoteErrors(t *testing.T) {
	net := &fakePublisherNetwork{}
	inst := compose.Instance{
		ID: compose.InstanceID{2},
		XTRequest: compose.XTRequest{
			Transactions: []compose.TransactionRequest{
				txReq(1, "a"),
				txReq(2, "b"),
			},
		},
	}

	pub, err := NewPublisherInstance(inst, net, testLogger())
	require.NoError(t, err)
	pub.Run()

	err = pub.ProcessVote(compose.ChainID(99), true)
	require.ErrorIs(t, err, ErrSenderNotParticipant)
	assert.Equal(t, 0, net.decidedCalled)
	assert.Equal(t, compose.DecisionStatePending, pub.DecisionState())

	// Valid participants can still vote and reach a decision.
	require.NoError(t, pub.ProcessVote(compose.ChainID(1), true))
	require.NoError(t, pub.ProcessVote(compose.ChainID(2), true))
	assert.Equal(t, 1, net.decidedCalled)
	assert.Equal(t, compose.DecisionStateAccepted, pub.DecisionState())
}

func TestPublisher_AnyFalseDecidesFalseEarly(t *testing.T) {
	net := &fakePublisherNetwork{}
	inst := compose.Instance{
		XTRequest: compose.XTRequest{
			Transactions: []compose.TransactionRequest{
				txReq(10, "a"),
				txReq(11, "b"),
				txReq(12, "c"),
			},
		},
	}
	pub, err := NewPublisherInstance(inst, net, testLogger())
	require.NoError(t, err)
	assert.Equal(t, inst, pub.Instance())
	assert.Equal(t, compose.DecisionStatePending, pub.DecisionState())
	pub.Run()
	assert.Equal(t, compose.DecisionStatePending, pub.DecisionState())

	// First false triggers immediate decision
	require.NoError(t, pub.ProcessVote(compose.ChainID(11), false))
	assert.Equal(t, 1, net.decidedCalled)
	assert.Equal(t, compose.DecisionStateRejected, pub.DecisionState())
	if assert.Len(t, net.decisions, 1) {
		assert.False(t, net.decisions[0].Value)
		assert.Equal(t, inst.ID, net.decisions[0].ID)
	}

	// Further votes are ignored
	require.NoError(t, pub.ProcessVote(compose.ChainID(12), true))
	assert.Equal(t, 1, net.decidedCalled, "unexpected extra decided calls")
	assert.Equal(t, compose.DecisionStateRejected, pub.DecisionState())
}

func TestPublisher_TimeoutDecidesFalse(t *testing.T) {
	net := &fakePublisherNetwork{}
	inst := compose.Instance{
		XTRequest: compose.XTRequest{
			Transactions: []compose.TransactionRequest{
				txReq(5, "a"),
				txReq(6, "b"),
			},
		},
	}
	pub, err := NewPublisherInstance(inst, net, testLogger())
	require.NoError(t, err)
	assert.Equal(t, inst, pub.Instance())
	assert.Equal(t, compose.DecisionStatePending, pub.DecisionState())
	pub.Run()
	assert.Equal(t, compose.DecisionStatePending, pub.DecisionState())

	require.NoError(t, pub.Timeout())
	assert.Equal(t, 1, net.decidedCalled)
	assert.Equal(t, compose.DecisionStateRejected, pub.DecisionState())
	if assert.Len(t, net.decisions, 1) {
		assert.False(t, net.decisions[0].Value)
		assert.Equal(t, inst.ID, net.decisions[0].ID)
	}

	// Timeout after done is ignored
	require.NoError(t, pub.Timeout())
	assert.Equal(t, 1, net.decidedCalled, "unexpected extra decided calls after second timeout")
	assert.Equal(t, compose.DecisionStateRejected, pub.DecisionState())
}

func TestPublisher_TimeoutAfterTrueDecisionIgnored(t *testing.T) {
	net := &fakePublisherNetwork{}
	inst := compose.Instance{
		XTRequest: compose.XTRequest{
			Transactions: []compose.TransactionRequest{
				txReq(1, "a"),
				txReq(2, "b"),
			},
		},
	}
	pub, err := NewPublisherInstance(inst, net, testLogger())
	require.NoError(t, err)
	assert.Equal(t, inst, pub.Instance())
	assert.Equal(t, compose.DecisionStatePending, pub.DecisionState())
	pub.Run()
	assert.Equal(t, compose.DecisionStatePending, pub.DecisionState())

	// Collect all true votes -> decide(true)
	require.NoError(t, pub.ProcessVote(compose.ChainID(1), true))
	require.NoError(t, pub.ProcessVote(compose.ChainID(2), true))
	assert.Equal(t, 1, net.decidedCalled)
	assert.Equal(t, compose.DecisionStateAccepted, pub.DecisionState())
	if assert.Len(t, net.decisions, 1) {
		assert.True(t, net.decisions[0].Value)
		assert.Equal(t, inst.ID, net.decisions[0].ID)
	}

	// Timeout after decision should be ignored
	require.NoError(t, pub.Timeout())
	assert.Equal(t, 1, net.decidedCalled)
	assert.Len(t, net.decisions, 1)
	assert.Equal(t, compose.DecisionStateAccepted, pub.DecisionState())
}

func TestPublisher_OneVoteThenTimeout_DecidesFalse(t *testing.T) {
	net := &fakePublisherNetwork{}
	inst := compose.Instance{
		XTRequest: compose.XTRequest{
			Transactions: []compose.TransactionRequest{
				txReq(1, "a"),
				txReq(2, "b"),
			},
		},
	}
	pub, err := NewPublisherInstance(inst, net, testLogger())
	require.NoError(t, err)
	assert.Equal(t, inst, pub.Instance())
	assert.Equal(t, compose.DecisionStatePending, pub.DecisionState())
	pub.Run()
	assert.Equal(t, compose.DecisionStatePending, pub.DecisionState())

	// Only one participant votes true; not enough to decide true.
	require.NoError(t, pub.ProcessVote(compose.ChainID(1), true))
	assert.Equal(t, 0, net.decidedCalled, "should not decide yet with partial votes")
	assert.Equal(t, compose.DecisionStatePending, pub.DecisionState())

	// Timeout should decide false (not true).
	require.NoError(t, pub.Timeout())
	assert.Equal(t, 1, net.decidedCalled)
	assert.Equal(t, compose.DecisionStateRejected, pub.DecisionState())
	if assert.Len(t, net.decisions, 1) {
		assert.False(t, net.decisions[0].Value)
		assert.Equal(t, inst.ID, net.decisions[0].ID)
	}
}
