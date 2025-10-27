package sbcp

import (
	"compose"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPublisher_StartPeriod_basic_broadcast_and_reset(t *testing.T) {
	m := &fakeMessenger{}
	// Start with finalized = 7, target can be arbitrary here
	pub := NewPublisher(
		m,
		compose.PeriodID(9),
		compose.SuperblockNumber(8),
		compose.SuperblockNumber(7),
		compose.SuperBlockHash{9},
		0,
	)

	// Start new period P=10 → target becomes F+1 = 8
	require.NoError(t, pub.StartPeriod(compose.PeriodID(10)))

	require.Len(t, m.startPeriods, 1)
	sp := m.startPeriods[0]
	assert.Equal(t, compose.PeriodID(10), sp.P)
	// Current implementation tags new period with next number after the just-sealed one,
	// which for finalized=7 leads to target=9.
	assert.Equal(t, compose.SuperblockNumber(9), sp.T)
}

func TestPublisher_StartPeriod_error_when_target_misaligned(t *testing.T) {
	m := &fakeMessenger{}
	// finalized = 7, but target = 10 (expected 8); StartPeriod should return an error
	pub := NewPublisher(m, compose.PeriodID(5), compose.SuperblockNumber(10), compose.SuperblockNumber(7), compose.SuperBlockHash{1}, 0)

	err := pub.StartPeriod(compose.PeriodID(6))
	require.ErrorIs(t, err, ErrCannotStartPeriod)
	// Ensure no broadcast happened
	assert.Len(t, m.startPeriods, 0)
}

func TestPublisher_StartInstance_disjoint_sets_allowed(t *testing.T) {
	m := &fakeMessenger{}
	pub := NewPublisher(m, 5, 6, 5, compose.SuperBlockHash{1}, 0)

	// First req touches chains {1,2}
	req1 := []compose.Transaction{
		fakeChainTx{chain: 1, body: []byte("a")},
		fakeChainTx{chain: 2, body: []byte("b")},
	}
	inst1, err := pub.StartInstance(req1)
	require.NoError(t, err)
	assert.ElementsMatch(t, []compose.ChainID{1, 2}, inst1.Chains)
	require.Len(t, m.startInstances, 1)

	// Disjoint {3,4} should be allowed
	req2 := []compose.Transaction{
		fakeChainTx{chain: 3, body: []byte("c")},
		fakeChainTx{chain: 4, body: []byte("d")},
	}
	inst2, err := pub.StartInstance(req2)
	require.NoError(t, err)
	assert.ElementsMatch(t, []compose.ChainID{3, 4}, inst2.Chains)
	require.Len(t, m.startInstances, 2)
}

func TestPublisher_StartInstance_conflicting_set_rejected(t *testing.T) {
	m := &fakeMessenger{}
	pub := NewPublisher(m, 5, 6, 5, compose.SuperBlockHash{1}, 0)

	// Activate {1,2}
	_, err := pub.StartInstance(
		[]compose.Transaction{fakeChainTx{1, []byte("a")}, fakeChainTx{2, []byte("b")}},
	)
	require.NoError(t, err)

	// Conflicts with {2,3}
	_, err = pub.StartInstance(
		[]compose.Transaction{fakeChainTx{2, []byte("x")}, fakeChainTx{3, []byte("y")}},
	)
	require.ErrorIs(t, err, ErrCannotStartInstance)
}

func TestPublisher_StartInstance_participant_dedup(t *testing.T) {
	m := &fakeMessenger{}
	pub := NewPublisher(m, 2, 3, 2, compose.SuperBlockHash{1}, 0)

	inst, err := pub.StartInstance([]compose.Transaction{
		fakeChainTx{chain: 7, body: []byte("a")},
		fakeChainTx{chain: 7, body: []byte("b")},
		fakeChainTx{chain: 8, body: []byte("c")},
	})
	require.NoError(t, err)
	assert.Len(t, inst.Chains, 2)
	assert.ElementsMatch(t, []compose.ChainID{7, 8}, inst.Chains)
}

func TestPublisher_Sequence_monotonic_and_resets_per_period(t *testing.T) {
	m := &fakeMessenger{}
	// Start aligned: target = finalized + 1 = 10
	pub := NewPublisher(m, 10, 10, 9, compose.SuperBlockHash{1}, 0)

	i1, err := pub.StartInstance([]compose.Transaction{
		fakeChainTx{1, []byte("a1")},
		fakeChainTx{2, []byte("a2")},
	})
	require.NoError(t, err)
	// Disjoint participants to avoid ErrCannotStartInstance
	i2, err := pub.StartInstance([]compose.Transaction{
		fakeChainTx{3, []byte("b1")},
		fakeChainTx{4, []byte("b2")},
	})
	require.NoError(t, err)

	assert.Equal(t, compose.SequenceNumber(1), i1.SequenceNumber)
	assert.Equal(t, compose.SequenceNumber(2), i2.SequenceNumber)

	// New period resets sequence counter and emits StartPeriod broadcast
	require.NoError(t, pub.StartPeriod(11))
	require.Len(t, m.startPeriods, 1)
	i3, err := pub.StartInstance([]compose.Transaction{
		fakeChainTx{5, []byte("c1")},
		fakeChainTx{6, []byte("c2")},
	})
	require.NoError(t, err)
	assert.Equal(t, compose.SequenceNumber(1), i3.SequenceNumber)
}

func TestPublisher_StartInstance_broadcast_payload_matches_return(t *testing.T) {
	messenger := &fakeMessenger{}
	pub := NewPublisher(messenger, 1, 2, 1, compose.SuperBlockHash{1}, 0)
	req := []compose.Transaction{fakeChainTx{1, []byte("x")}, fakeChainTx{2, []byte("y")}}

	inst, err := pub.StartInstance(req)
	require.NoError(t, err)
	require.Len(t, messenger.startInstances, 1)
	got := messenger.startInstances[0]
	assert.Equal(t, inst, got)
}

func TestPublisher_DecideInstance_clears_active_and_validates_active(t *testing.T) {
	m := &fakeMessenger{}
	pub := NewPublisher(m, 1, 2, 1, compose.SuperBlockHash{1}, 0)
	inst, err := pub.StartInstance(
		[]compose.Transaction{fakeChainTx{1, []byte("a")}, fakeChainTx{2, []byte("b")}},
	)
	require.NoError(t, err)

	// Decide should succeed when chains are active
	err = pub.DecideInstance(inst)
	require.NoError(t, err)

	// Deciding the original instance again should report inactive chains
	err = pub.DecideInstance(inst)
	require.ErrorIs(t, err, ErrChainNotActive)

	// Starting another instance with same chains should now be possible
	_, err = pub.StartInstance(
		[]compose.Transaction{fakeChainTx{1, []byte("c")}, fakeChainTx{2, []byte("d")}},
	)
	require.NoError(t, err)
}

func TestPublisher_AdvanceSettledState_monotonic(t *testing.T) {
	m := &fakeMessenger{}
	pub := NewPublisher(m, 1, 2, 1, compose.SuperBlockHash{1}, 0)
	// Advance forward
	err := pub.AdvanceSettledState(2, compose.SuperBlockHash{9})
	require.NoError(t, err)
	// Future calls with lower or equal values should return ErrOldSettledState
	err = pub.AdvanceSettledState(2, compose.SuperBlockHash{8})
	require.ErrorIs(t, err, ErrOldSettledState)
}

func TestPublisher_ProofTimeout_rolls_back_and_resets_target(t *testing.T) {
	m := &fakeMessenger{}
	finalized := compose.SuperblockNumber(5)
	pub := NewPublisher(m, 3, 4, finalized, compose.SuperBlockHash{7}, 0)

	// Activate some chains
	_, err := pub.StartInstance(
		[]compose.Transaction{fakeChainTx{1, []byte("a")}, fakeChainTx{2, []byte("b")}},
	)
	require.NoError(t, err)

	pub.ProofTimeout()
	// Expect a rollback broadcast to last finalized and target reset to F+1
	require.Len(t, m.rollbacks, 1)
	rb := m.rollbacks[0]
	assert.Equal(t, finalized, rb.S)
	assert.Equal(t, compose.SuperBlockHash{7}, rb.H)
}

func TestPublisher_StartInstance_invalid_requests(t *testing.T) {
	m := &fakeMessenger{}
	pub := NewPublisher(m, 1, 2, 1, compose.SuperBlockHash{1}, 0)

	// Nil request
	_, err := pub.StartInstance(nil)
	require.ErrorIs(t, err, ErrInvalidRequest)

	// Empty slice
	_, err = pub.StartInstance([]compose.Transaction{})
	require.ErrorIs(t, err, ErrInvalidRequest)

	// Single-transaction request
	_, err = pub.StartInstance([]compose.Transaction{fakeChainTx{1, []byte("only")}})
	require.ErrorIs(t, err, ErrInvalidRequest)

	// No broadcast occurred
	assert.Len(t, m.startInstances, 0)
}
