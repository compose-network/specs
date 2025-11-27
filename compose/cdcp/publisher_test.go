package cdcp

import (
	"testing"

	"github.com/compose-network/specs/compose"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPublisher_NewInstanceValidatesChains(t *testing.T) {
	instance := makeInstance(chainReq(1, []byte("a")))
	_, err := NewPublisherInstance(instance, &fakePublisherNetwork{}, compose.ChainID(1), testLogger())
	require.ErrorIs(t, err, ErrNotEnoughChains)

	instance = makeInstance(
		chainReq(1, []byte("a")),
		chainReq(2, []byte("b")),
	)
	_, err = NewPublisherInstance(instance, &fakePublisherNetwork{}, compose.ChainID(3), testLogger())
	// creation errors if the ER chain has no transaction.
	require.ErrorIs(t, err, ErrERNotFound)
}

func TestPublisher_AllNativeVotesTrueThenWSDecidedTrue(t *testing.T) {
	instance := makeInstance(
		chainReq(1, []byte("a1")),
		chainReq(2, []byte("a2")),
		chainReq(3, []byte("er")), // ER chain
	)
	net := &fakePublisherNetwork{}
	pub, err := NewPublisherInstance(instance, net, compose.ChainID(3), testLogger())
	require.NoError(t, err)

	pub.Run()
	// StartInstance message should be sent
	require.Len(t, net.startInstances, 1)
	assert.Equal(t, instance.ID, net.startInstances[0].ID)

	// Processed votes
	require.NoError(t, pub.ProcessVote(compose.ChainID(1), true))
	require.NoError(t, pub.ProcessVote(compose.ChainID(2), true))
	// NativeDecided shoudl be sent with result true
	assert.Len(t, net.nativeDecided, 1)
	assert.True(t, net.nativeDecided[0].Result)
	assert.Equal(t, compose.DecisionStatePending, pub.DecisionState())

	// Timeout while waiting for WS should be ignored.
	require.NoError(t, pub.Timeout())
	assert.Len(t, net.decisions, 0)

	// WSDecided should decide the state
	require.NoError(t, pub.ProcessWSDecided(compose.ChainID(3), true))
	assert.Equal(t, compose.DecisionStateAccepted, pub.DecisionState())
	require.Len(t, net.decisions, 1)
	assert.True(t, net.decisions[0].Result)

	// Further WS decisions error with ErrDuplicatedWSDecided.
	err = pub.ProcessWSDecided(compose.ChainID(3), true)
	require.ErrorIs(t, err, ErrDuplicatedWSDecided)
}

func TestPublisher_VoteFalseRejectsImmediately(t *testing.T) {
	instance := makeInstance(
		chainReq(1, []byte("a1")),
		chainReq(2, []byte("a2")),
		chainReq(3, []byte("er")),
	)
	net := &fakePublisherNetwork{}
	pub, err := NewPublisherInstance(instance, net, compose.ChainID(3), testLogger())
	require.NoError(t, err)

	pub.Run()
	// A vote of false immediately rejects the instance.
	require.NoError(t, pub.ProcessVote(compose.ChainID(1), false))
	assert.Equal(t, compose.DecisionStateRejected, pub.DecisionState())
	require.Len(t, net.decisions, 1)
	assert.False(t, net.decisions[0].Result)
	// NativeDecided is sent with result false
	require.Len(t, net.nativeDecided, 1)
	assert.False(t, net.nativeDecided[0].Result)

	// Further votes do not change the outcome.
	require.NoError(t, pub.ProcessVote(compose.ChainID(2), true))
	assert.Len(t, net.decisions, 1)
	assert.Len(t, net.nativeDecided, 1)
}

func TestPublisher_VoteErrorsAndTimeout(t *testing.T) {
	instance := makeInstance(
		chainReq(1, []byte("a1")),
		chainReq(2, []byte("a2")),
		chainReq(3, []byte("er")),
	)
	net := &fakePublisherNetwork{}
	pub, err := NewPublisherInstance(instance, net, compose.ChainID(3), testLogger())
	require.NoError(t, err)

	pub.Run()
	// Process vote
	require.NoError(t, pub.ProcessVote(compose.ChainID(1), true))

	// Duplicated vote should error
	err = pub.ProcessVote(compose.ChainID(1), true)
	require.ErrorIs(t, err, ErrDuplicatedVote)

	// Vote from non-native chain should error
	err = pub.ProcessVote(compose.ChainID(3), true)
	require.ErrorIs(t, err, ErrVoteSenderNotNativeChain)

	// Timeout while waiting for votes should reject the instance.
	require.NoError(t, pub.Timeout())
	assert.Equal(t, compose.DecisionStateRejected, pub.DecisionState())
	require.Len(t, net.decisions, 1)
	assert.False(t, net.decisions[0].Result)
	require.Len(t, net.nativeDecided, 1)
	assert.False(t, net.nativeDecided[0].Result)

	// Timeout after done is ignored.
	require.NoError(t, pub.Timeout())
	assert.Len(t, net.decisions, 1)
	assert.Len(t, net.nativeDecided, 1)
}

func TestPublisher_WSDecidedFalseBeforeVotesComplete(t *testing.T) {
	instance := makeInstance(
		chainReq(1, []byte("a1")),
		chainReq(2, []byte("a2")),
		chainReq(3, []byte("er")),
	)
	net := &fakePublisherNetwork{}
	pub, err := NewPublisherInstance(instance, net, compose.ChainID(3), testLogger())
	require.NoError(t, err)

	pub.Run()
	// WSDecided of false should reject the instance even though not all votes arrived.
	require.NoError(t, pub.ProcessVote(compose.ChainID(1), true))
	require.NoError(t, pub.ProcessWSDecided(compose.ChainID(3), false))
	assert.Equal(t, compose.DecisionStateRejected, pub.DecisionState())
	require.Len(t, net.decisions, 1)
	assert.False(t, net.decisions[0].Result)
	// Native decided should not have been sent because WS terminated early.
	assert.Len(t, net.nativeDecided, 0)
}

func TestPublisher_AllNativeVotesTrueThenWSDecidedFalse(t *testing.T) {
	instance := makeInstance(
		chainReq(1, []byte("a1")),
		chainReq(2, []byte("a2")),
		chainReq(3, []byte("er")),
	)
	net := &fakePublisherNetwork{}
	pub, err := NewPublisherInstance(instance, net, compose.ChainID(3), testLogger())
	require.NoError(t, err)

	pub.Run()
	// All votes true
	require.NoError(t, pub.ProcessVote(compose.ChainID(1), true))
	require.NoError(t, pub.ProcessVote(compose.ChainID(2), true))
	require.Len(t, net.nativeDecided, 1)
	assert.True(t, net.nativeDecided[0].Result)

	// WSDecided false should reject the instance.
	require.NoError(t, pub.ProcessWSDecided(compose.ChainID(3), false))
	assert.Equal(t, compose.DecisionStateRejected, pub.DecisionState())
	require.Len(t, net.decisions, 1)
	assert.False(t, net.decisions[0].Result)
}

func TestPublisher_WSDecidedFromNonERChainErrors(t *testing.T) {
	instance := makeInstance(
		chainReq(1, []byte("a1")),
		chainReq(2, []byte("a2")),
		chainReq(3, []byte("er")),
	)
	net := &fakePublisherNetwork{}
	pub, err := NewPublisherInstance(instance, net, compose.ChainID(3), testLogger())
	require.NoError(t, err)

	pub.Run()
	// WSDecided from non-ER chain should error.
	err = pub.ProcessWSDecided(compose.ChainID(1), false)
	require.ErrorIs(t, err, ErrNotERChain)
	assert.Equal(t, compose.DecisionStatePending, pub.DecisionState())

	// Valid WSDecided still works after the error.
	require.NoError(t, pub.ProcessWSDecided(compose.ChainID(3), false))
	assert.Equal(t, compose.DecisionStateRejected, pub.DecisionState())
	require.Len(t, net.decisions, 1)
}

func TestPublisher_WSDecidedTrueWhileWaitingVotesErrors(t *testing.T) {
	instance := makeInstance(
		chainReq(1, []byte("a1")),
		chainReq(2, []byte("a2")),
		chainReq(3, []byte("er")),
	)
	net := &fakePublisherNetwork{}
	pub, err := NewPublisherInstance(instance, net, compose.ChainID(3), testLogger())
	require.NoError(t, err)

	pub.Run()
	// WSDecided true should error if not all votes have arrived yet.
	err = pub.ProcessWSDecided(compose.ChainID(3), true)
	require.ErrorIs(t, err, ErrInvalidStateForWSDecided)
	assert.Equal(t, compose.DecisionStatePending, pub.DecisionState())
	assert.Len(t, net.decisions, 0)

	// Once native votes complete, WSDecided true is accepted.
	require.NoError(t, pub.ProcessVote(compose.ChainID(1), true))
	require.NoError(t, pub.ProcessVote(compose.ChainID(2), true))
	require.NoError(t, pub.ProcessWSDecided(compose.ChainID(3), true))
	assert.Equal(t, compose.DecisionStateAccepted, pub.DecisionState())
	require.Len(t, net.decisions, 1)
}

func TestPublisher_DuplicateWSDecidedErrors(t *testing.T) {
	instance := makeInstance(
		chainReq(1, []byte("a1")),
		chainReq(2, []byte("a2")),
		chainReq(3, []byte("er")),
	)
	net := &fakePublisherNetwork{}
	pub, err := NewPublisherInstance(instance, net, compose.ChainID(3), testLogger())
	require.NoError(t, err)

	pub.Run()
	require.NoError(t, pub.ProcessVote(compose.ChainID(1), true))
	require.NoError(t, pub.ProcessVote(compose.ChainID(2), true))
	require.NoError(t, pub.ProcessWSDecided(compose.ChainID(3), true))
	assert.Equal(t, compose.DecisionStateAccepted, pub.DecisionState())
	// Duplicate WSDecided should error.
	err = pub.ProcessWSDecided(compose.ChainID(3), true)
	require.ErrorIs(t, err, ErrDuplicatedWSDecided)
	assert.Len(t, net.decisions, 1)
}
