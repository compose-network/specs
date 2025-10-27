package scp

import (
	"io"
	"testing"

	"github.com/compose-network/specs/compose"
	"github.com/rs/zerolog"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func testLogger() zerolog.Logger {
	return zerolog.New(io.Discard)
}

func TestPublisher_AllTrueVotesDecidesTrue(t *testing.T) {
	net := &fakePublisherNetwork{}
	inst := compose.Instance{
		ID:             compose.InstanceID{1},
		PeriodID:       7,
		SequenceNumber: 3,
		XTRequest: []compose.Transaction{
			fakeTx{chain: 1, name: "a"},
			fakeTx{chain: 2, name: "b"},
		},
	}

	pub, err := NewPublisherInstance(inst, net, testLogger())
	require.NoError(t, err)

	// No start until Run is called
	assert.Equal(t, 0, net.startCalled, "unexpected start before Run")
	pub.Run()
	assert.Equal(t, 1, net.startCalled)

	// First vote true from chain 1
	require.NoError(t, pub.ProcessVote(compose.ChainID(1), true))
	assert.Equal(t, 0, net.decidedCalled, "should not decide yet")

	// Duplicate while still waiting should error with a stable message
	errDup := pub.ProcessVote(compose.ChainID(1), true)
	require.ErrorIs(t, errDup, ErrDuplicatedVote)

	// Second vote true from chain 2 triggers decide(true)
	require.NoError(t, pub.ProcessVote(compose.ChainID(2), true))
	assert.Equal(t, 1, net.decidedCalled)
	if assert.Len(t, net.decisions, 1) {
		assert.True(t, net.decisions[0].Value)
		assert.Equal(t, inst.ID, net.decisions[0].ID)
	}

	// Duplicate after decision is ignored
	require.NoError(t, pub.ProcessVote(compose.ChainID(1), true))

	// Vote after done is ignored (no extra decided)
	require.NoError(t, pub.ProcessVote(compose.ChainID(3), true))
	assert.Equal(t, 1, net.decidedCalled, "unexpected extra decided calls")
}

func TestPublisher_AnyFalseDecidesFalseEarly(t *testing.T) {
	net := &fakePublisherNetwork{}
	inst := compose.Instance{
		XTRequest: []compose.Transaction{
			fakeTx{chain: compose.ChainID(10), name: "a"},
			fakeTx{chain: compose.ChainID(11), name: "b"},
			fakeTx{chain: compose.ChainID(12), name: "c"},
		},
	}
	pub, err := NewPublisherInstance(inst, net, testLogger())
	require.NoError(t, err)
	pub.Run()

	// First false triggers immediate decision
	require.NoError(t, pub.ProcessVote(compose.ChainID(11), false))
	assert.Equal(t, 1, net.decidedCalled)
	if assert.Len(t, net.decisions, 1) {
		assert.False(t, net.decisions[0].Value)
		assert.Equal(t, inst.ID, net.decisions[0].ID)
	}

	// Further votes are ignored
	require.NoError(t, pub.ProcessVote(compose.ChainID(12), true))
	assert.Equal(t, 1, net.decidedCalled, "unexpected extra decided calls")
}

func TestPublisher_TimeoutDecidesFalse(t *testing.T) {
	net := &fakePublisherNetwork{}
	inst := compose.Instance{
		XTRequest: []compose.Transaction{
			fakeTx{chain: compose.ChainID(5), name: "a"},
			fakeTx{chain: compose.ChainID(6), name: "b"},
		},
	}
	pub, err := NewPublisherInstance(inst, net, testLogger())
	require.NoError(t, err)
	pub.Run()

	require.NoError(t, pub.Timeout())
	assert.Equal(t, 1, net.decidedCalled)
	if assert.Len(t, net.decisions, 1) {
		assert.False(t, net.decisions[0].Value)
		assert.Equal(t, inst.ID, net.decisions[0].ID)
	}

	// Timeout after done is ignored
	require.NoError(t, pub.Timeout())
	assert.Equal(t, 1, net.decidedCalled, "unexpected extra decided calls after second timeout")
}

func TestPublisher_TimeoutAfterTrueDecisionIgnored(t *testing.T) {
	net := &fakePublisherNetwork{}
	inst := compose.Instance{
		XTRequest: []compose.Transaction{
			fakeTx{chain: compose.ChainID(1), name: "a"},
			fakeTx{chain: compose.ChainID(2), name: "b"},
		},
	}
	pub, err := NewPublisherInstance(inst, net, testLogger())
	require.NoError(t, err)
	pub.Run()

	// Collect all true votes -> decide(true)
	require.NoError(t, pub.ProcessVote(compose.ChainID(1), true))
	require.NoError(t, pub.ProcessVote(compose.ChainID(2), true))
	assert.Equal(t, 1, net.decidedCalled)
	if assert.Len(t, net.decisions, 1) {
		assert.True(t, net.decisions[0].Value)
		assert.Equal(t, inst.ID, net.decisions[0].ID)
	}

	// Timeout after decision should be ignored
	require.NoError(t, pub.Timeout())
	assert.Equal(t, 1, net.decidedCalled)
	assert.Len(t, net.decisions, 1)
}

func TestPublisher_OneVoteThenTimeout_DecidesFalse(t *testing.T) {
	net := &fakePublisherNetwork{}
	inst := compose.Instance{
		XTRequest: []compose.Transaction{
			fakeTx{chain: compose.ChainID(1), name: "a"},
			fakeTx{chain: compose.ChainID(2), name: "b"},
		},
	}
	pub, err := NewPublisherInstance(inst, net, testLogger())
	require.NoError(t, err)
	pub.Run()

	// Only one participant votes true; not enough to decide true.
	require.NoError(t, pub.ProcessVote(compose.ChainID(1), true))
	assert.Equal(t, 0, net.decidedCalled, "should not decide yet with partial votes")

	// Timeout should decide false (not true).
	require.NoError(t, pub.Timeout())
	assert.Equal(t, 1, net.decidedCalled)
	if assert.Len(t, net.decisions, 1) {
		assert.False(t, net.decisions[0].Value)
		assert.Equal(t, inst.ID, net.decisions[0].ID)
	}
}
