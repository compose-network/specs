package sbcp

import (
	"errors"
	"testing"

	"github.com/compose-network/specs/compose"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newPublisherForTest(
	period compose.PeriodID,
	target compose.SuperblockNumber,
	finalized compose.SuperblockNumber,
	hash compose.SuperblockHash,
	window uint64,
	chains map[compose.ChainID]struct{},
) (Publisher, *fakePublisherMessenger, *fakePublisherProver, *fakeL1) {
	m := &fakePublisherMessenger{}
	p := &fakePublisherProver{}
	l1 := &fakeL1{}
	pub, err := NewPublisher(p, m, l1, period, target, finalized, hash, window, testLogger(), chains)
	if err != nil {
		panic(err)
	}
	return pub, m, p, l1
}

func TestNewPublisher_rejectsTargetLowerThanFinalized(t *testing.T) {
	_, err := NewPublisher(
		&fakePublisherProver{},
		&fakePublisherMessenger{},
		&fakeL1{},
		compose.PeriodID(3),
		compose.SuperblockNumber(4),
		compose.SuperblockNumber(5),
		compose.SuperblockHash{1},
		0,
		testLogger(),
		makeDefaultChainSet(),
	)
	require.Error(t, err)
}

func TestPublisher_StartPeriod_respectsExplicitTarget(t *testing.T) {
	pub, messenger, _, _ := newPublisherForTest(
		compose.PeriodID(4),
		compose.SuperblockNumber(10),
		compose.SuperblockNumber(7),
		compose.SuperblockHash{5},
		0,
		makeDefaultChainSet(),
	)

	require.NoError(t, pub.StartPeriod())
	require.Len(t, messenger.startPeriods, 1)
	start := messenger.startPeriods[0]
	assert.Equal(t, compose.PeriodID(5), start.PeriodID)
	assert.Equal(t, compose.SuperblockNumber(11), start.SuperblockNumber)

	impl := pub.(*publisher)
	assert.Equal(t, compose.SuperblockNumber(11), impl.TargetSuperblockNumber)
}

func TestPublisher_StartPeriod_rejectsInitialTargetPastWindow(t *testing.T) {
	pub, messenger, _, _ := newPublisherForTest(
		compose.PeriodID(2),
		compose.SuperblockNumber(12),
		compose.SuperblockNumber(5),
		compose.SuperblockHash{3},
		1,
		makeDefaultChainSet(),
	)

	err := pub.StartPeriod()
	require.ErrorIs(t, err, ErrCannotStartPeriod)
	assert.Len(t, messenger.startPeriods, 0)
}

func TestPublisher_StartPeriod_basic_broadcast_and_reset(t *testing.T) {
	// Start with finalized = 7 and target = 7
	pub, m, _, _ := newPublisherForTest(
		compose.PeriodID(9),
		compose.SuperblockNumber(7),
		compose.SuperblockNumber(7),
		compose.SuperblockHash{9},
		0,
		makeDefaultChainSet(),
	)

	// Start new period PeriodID=10 → target becomes F+1 = 8
	require.NoError(t, pub.StartPeriod())

	require.Len(t, m.startPeriods, 1)
	sp := m.startPeriods[0]
	assert.Equal(t, compose.PeriodID(10), sp.PeriodID)
	// Current implementation tags new period with next number after the just-sealed one,
	// which for finalized=7 leads to target=8.
	assert.Equal(t, compose.SuperblockNumber(8), sp.SuperblockNumber)
}

func TestPublisher_StartPeriod_error_when_target_exceeds_proof_window(t *testing.T) {
	// Trying to advance twice without a newer finalized superblock should fail.
	pub, m, _, _ := newPublisherForTest(
		compose.PeriodID(5),
		compose.SuperblockNumber(7),
		compose.SuperblockNumber(7),
		compose.SuperblockHash{1},
		1,
		makeDefaultChainSet(),
	)

	require.NoError(t, pub.StartPeriod())
	require.NoError(t, pub.StartPeriod())

	err := pub.StartPeriod()
	require.ErrorIs(t, err, ErrCannotStartPeriod)
	// Ensure only the successful StartPeriod calls were broadcast
	assert.Len(t, m.startPeriods, 2)
}

func TestPublisher_StartPeriod_no_window_constraint_when_disabled(t *testing.T) {
	// Proof window set to 0 should disable the constraint entirely.
	pub, m, _, _ := newPublisherForTest(
		compose.PeriodID(5),
		compose.SuperblockNumber(7),
		compose.SuperblockNumber(7),
		compose.SuperblockHash{1},
		0,
		makeDefaultChainSet(),
	)

	for i := 0; i < 3; i++ {
		require.NoError(t, pub.StartPeriod())
	}

	assert.Len(t, m.startPeriods, 3)
}

func TestPublisher_StartInstance_disjoint_sets_allowed(t *testing.T) {
	pub, _, _, _ := newPublisherForTest(
		compose.PeriodID(5),
		compose.SuperblockNumber(5),
		compose.SuperblockNumber(5),
		compose.SuperblockHash{1},
		0,
		makeDefaultChainSet(),
	)

	// First req touches chains {1,2}
	req1 := makeXTRequest(
		chainReq(1, []byte("a")),
		chainReq(2, []byte("b")),
	)
	inst1, err := pub.StartInstance(req1)
	require.NoError(t, err)
	assert.ElementsMatch(t, []compose.ChainID{1, 2}, inst1.Chains())

	// Disjoint {3,4} should be allowed
	req2 := makeXTRequest(
		chainReq(3, []byte("c")),
		chainReq(4, []byte("d")),
	)
	inst2, err := pub.StartInstance(req2)
	require.NoError(t, err)
	assert.ElementsMatch(t, []compose.ChainID{3, 4}, inst2.Chains())
}

func TestPublisher_StartInstance_conflicting_set_rejected(t *testing.T) {
	pub, _, _, _ := newPublisherForTest(
		compose.PeriodID(5),
		compose.SuperblockNumber(5),
		compose.SuperblockNumber(5),
		compose.SuperblockHash{1},
		0,
		makeDefaultChainSet(),
	)

	// Activate {1,2}
	_, err := pub.StartInstance(
		makeXTRequest(
			chainReq(1, []byte("a")),
			chainReq(2, []byte("b")),
		),
	)
	require.NoError(t, err)

	// Conflicts with {2,3}
	_, err = pub.StartInstance(
		makeXTRequest(
			chainReq(2, []byte("x")),
			chainReq(3, []byte("y")),
		),
	)
	require.ErrorIs(t, err, ErrCannotStartInstance)
}

func TestPublisher_StartInstance_participant_dedup(t *testing.T) {
	pub, _, _, _ := newPublisherForTest(
		compose.PeriodID(2),
		compose.SuperblockNumber(2),
		compose.SuperblockNumber(2),
		compose.SuperblockHash{1},
		0,
		makeDefaultChainSet(),
	)

	inst, err := pub.StartInstance(makeXTRequest(
		chainReq(7, []byte("a"), []byte("b")),
		chainReq(8, []byte("c")),
	))
	require.NoError(t, err)
	chains := inst.Chains()
	assert.Len(t, chains, 2)
	assert.ElementsMatch(t, []compose.ChainID{7, 8}, chains)
}

func TestPublisher_Sequence_monotonic_and_resets_per_period(t *testing.T) {
	// Start aligned: target = finalized + 1 = 10
	pub, m, _, _ := newPublisherForTest(
		compose.PeriodID(10),
		compose.SuperblockNumber(9),
		compose.SuperblockNumber(9),
		compose.SuperblockHash{1},
		0,
		makeDefaultChainSet(),
	)

	i1, err := pub.StartInstance(makeXTRequest(
		chainReq(1, []byte("a1")),
		chainReq(2, []byte("a2")),
	))
	require.NoError(t, err)
	// Disjoint participants to avoid ErrCannotStartInstance
	i2, err := pub.StartInstance(makeXTRequest(
		chainReq(3, []byte("b1")),
		chainReq(4, []byte("b2")),
	))
	require.NoError(t, err)

	assert.Equal(t, compose.SequenceNumber(1), i1.SequenceNumber)
	assert.Equal(t, compose.SequenceNumber(2), i2.SequenceNumber)

	// New period resets sequence counter and emits StartPeriod broadcast
	require.NoError(t, pub.StartPeriod())
	require.Len(t, m.startPeriods, 1)
	i3, err := pub.StartInstance(makeXTRequest(
		chainReq(5, []byte("c1")),
		chainReq(6, []byte("c2")),
	))
	require.NoError(t, err)
	assert.Equal(t, compose.SequenceNumber(1), i3.SequenceNumber)
}

func TestPublisher_StartInstance_populates_instance_fields(t *testing.T) {
	pub, messenger, _, _ := newPublisherForTest(
		compose.PeriodID(1),
		compose.SuperblockNumber(1),
		compose.SuperblockNumber(1),
		compose.SuperblockHash{1},
		0,
		makeDefaultChainSet(),
	)
	req := makeXTRequest(
		chainReq(1, []byte("x")),
		chainReq(2, []byte("y")),
	)

	inst, err := pub.StartInstance(req)
	require.NoError(t, err)
	assert.Equal(t, compose.PeriodID(1), inst.PeriodID)
	assert.Equal(t, compose.SequenceNumber(1), inst.SequenceNumber)
	assert.Equal(t, req, inst.XTRequest)
	assert.Len(t, messenger.startInstances, 0)
}

func TestPublisher_DecideInstance_clears_active_and_validates_active(t *testing.T) {
	pub, _, _, _ := newPublisherForTest(
		compose.PeriodID(1),
		compose.SuperblockNumber(1),
		compose.SuperblockNumber(1),
		compose.SuperblockHash{1},
		0,
		makeDefaultChainSet(),
	)
	inst, err := pub.StartInstance(
		makeXTRequest(
			chainReq(1, []byte("a")),
			chainReq(2, []byte("b")),
		),
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
		makeXTRequest(
			chainReq(1, []byte("c")),
			chainReq(2, []byte("d")),
		),
	)
	require.NoError(t, err)
}

func TestPublisher_AdvanceSettledState_monotonic(t *testing.T) {
	pub, _, _, _ := newPublisherForTest(
		compose.PeriodID(1),
		compose.SuperblockNumber(1),
		compose.SuperblockNumber(1),
		compose.SuperblockHash{1},
		0,
		makeDefaultChainSet(),
	)
	// Advance forward
	err := pub.AdvanceSettledState(2, compose.SuperblockHash{9})
	require.NoError(t, err)
	// Future calls with lower or equal values should return ErrOldSettledState
	err = pub.AdvanceSettledState(2, compose.SuperblockHash{8})
	require.ErrorIs(t, err, ErrOldSettledState)
}

func TestPublisher_ProofTimeout_rolls_back_and_resets_target(t *testing.T) {
	finalized := compose.SuperblockNumber(5)
	pub, m, _, _ := newPublisherForTest(
		compose.PeriodID(3),
		finalized,
		finalized,
		compose.SuperblockHash{7},
		0,
		makeDefaultChainSet(),
	)

	// Activate some chains
	_, err := pub.StartInstance(
		makeXTRequest(
			chainReq(1, []byte("a")),
			chainReq(2, []byte("b")),
		),
	)
	require.NoError(t, err)

	pub.ProofTimeout()
	// Expect a rollback broadcast to last finalized and target reset to F+1
	require.Len(t, m.rollbacks, 1)
	rb := m.rollbacks[0]
	assert.Equal(t, compose.PeriodID(3), rb.PeriodID)
	assert.Equal(t, finalized, rb.SuperblockNumber)
	assert.Equal(t, compose.SuperblockHash{7}, rb.SuperblockHash)

	impl := pub.(*publisher)
	assert.Equal(t, finalized+1, impl.TargetSuperblockNumber)
}

func TestPublisher_StartInstance_invalid_requests(t *testing.T) {
	pub, m, _, _ := newPublisherForTest(
		compose.PeriodID(1),
		compose.SuperblockNumber(1),
		compose.SuperblockNumber(1),
		compose.SuperblockHash{1},
		0,
		makeDefaultChainSet(),
	)

	// Empty request
	_, err := pub.StartInstance(compose.XTRequest{})
	require.ErrorIs(t, err, ErrInvalidRequest)

	// Single-transaction request
	_, err = pub.StartInstance(makeXTRequest(chainReq(1, []byte("only"))))
	require.ErrorIs(t, err, ErrInvalidRequest)

	// No broadcast occurred
	assert.Len(t, m.startInstances, 0)
}

func TestPublisher_ReceiveProof_aggregates_and_publishes(t *testing.T) {
	chains := makeChainSet(compose.ChainID(1), compose.ChainID(2))
	pub, _, prover, l1 := newPublisherForTest(
		compose.PeriodID(10),
		compose.SuperblockNumber(5),
		compose.SuperblockNumber(5),
		compose.SuperblockHash{1},
		0,
		chains,
	)
	prover.nextProof = []byte("network-proof")
	require.NoError(t, pub.StartPeriod())
	require.NoError(t, pub.StartPeriod())

	// Receive proofs from both chains
	pub.ReceiveProof(compose.PeriodID(11), compose.SuperblockNumber(6), []byte("proof-1"), compose.ChainID(1))
	assert.Len(t, prover.calls, 0)
	assert.Len(t, l1.published, 0)

	pub.ReceiveProof(compose.PeriodID(11), compose.SuperblockNumber(6), []byte("proof-2"), compose.ChainID(2))
	require.Len(t, prover.calls, 1)

	// Check prover was called
	call := prover.calls[0]
	assert.Equal(t, compose.SuperblockNumber(6), call.superblock)
	assert.Equal(t, compose.SuperblockHash{1}, call.hash)
	assert.Len(t, call.proofs, 2)
	assert.ElementsMatch(t, [][]byte{[]byte("proof-1"), []byte("proof-2")}, call.proofs)

	// Check L1 was called to publish
	require.Len(t, l1.published, 1)
	assert.Equal(t, compose.SuperblockNumber(6), l1.published[0].superblock)
	assert.Equal(t, []byte("network-proof"), l1.published[0].proof)

	// Check map has been cleared out
	impl := pub.(*publisher)
	assert.Nil(t, impl.Proofs[compose.SuperblockNumber(6)])
}

func TestPublisher_ReceiveProof_proverErrorTriggersRollback(t *testing.T) {
	chains := makeChainSet(compose.ChainID(1), compose.ChainID(2))
	pub, messenger, prover, l1 := newPublisherForTest(
		compose.PeriodID(10),
		compose.SuperblockNumber(5),
		compose.SuperblockNumber(5),
		compose.SuperblockHash{9},
		0,
		chains,
	)
	prover.err = errors.New("boom")
	require.NoError(t, pub.StartPeriod())
	require.NoError(t, pub.StartPeriod())

	// Receives proofs from sequencers and trigger call to prover
	pub.ReceiveProof(compose.PeriodID(11), compose.SuperblockNumber(6), []byte("proof-1"), compose.ChainID(1))
	pub.ReceiveProof(compose.PeriodID(11), compose.SuperblockNumber(6), []byte("proof-2"), compose.ChainID(2))

	// Check that didn't call publisher, and sent a rollback
	assert.Len(t, l1.published, 0)
	require.Len(t, messenger.rollbacks, 1)

	// Correct rollback msg
	rb := messenger.rollbacks[0]
	assert.Equal(t, compose.SuperblockNumber(5), rb.SuperblockNumber)
	assert.Equal(t, compose.SuperblockHash{9}, rb.SuperblockHash)
}

func TestPublisher_ReceiveProof_ignores_non_terminated_superblock(t *testing.T) {
	chains := makeChainSet(compose.ChainID(1), compose.ChainID(2))
	pub, _, prover, l1 := newPublisherForTest(
		compose.PeriodID(5),
		compose.SuperblockNumber(4),
		compose.SuperblockNumber(4),
		compose.SuperblockHash{3},
		0,
		chains,
	)
	require.NoError(t, pub.StartPeriod())

	// Receive proof for non-terminated superblock (5)
	superblock := compose.SuperblockNumber(5)
	pub.ReceiveProof(compose.PeriodID(6), superblock, []byte("proof"), compose.ChainID(1))

	assert.Len(t, prover.calls, 0)
	assert.Len(t, l1.published, 0)
	// Check that proof wasn't stored
	impl := pub.(*publisher)
	_, exists := impl.Proofs[superblock]
	assert.False(t, exists)
}
