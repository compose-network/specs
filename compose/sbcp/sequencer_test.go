package sbcp

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

func newSequencerForTest(
	period compose.PeriodID,
	target compose.SuperblockNumber,
	settled SettledState,
) (*sequencer, *fakeSequencerProver, *fakeSequencerMessenger) {
	prover := &fakeSequencerProver{}
	messenger := &fakeSequencerMessenger{}
	s := NewSequencer(prover, messenger, period, target, settled, testLogger()).(*sequencer)
	return s, prover, messenger
}

func TestSequencer_NewSequencer_initial_state(t *testing.T) {
	settled := mkSettled(4, 100)
	s, _, _ := newSequencerForTest(compose.PeriodID(10), compose.SuperblockNumber(11), settled)

	assert.Equal(t, compose.PeriodID(10), s.PeriodID)
	assert.Equal(t, compose.SuperblockNumber(11), s.TargetSuperblockNumber)
	assert.Equal(t, BlockNumber(100), s.Head)
	assert.Nil(t, s.PendingBlock)
	assert.Nil(t, s.ActiveInstanceID)
	assert.Empty(t, s.SealedBlockHead)
}

func TestSequencer_BeginBlock_ok_and_errors(t *testing.T) {
	s, _, _ := newSequencerForTest(compose.PeriodID(5), compose.SuperblockNumber(6), mkSettled(3, 10))

	// OK path
	require.NoError(t, s.BeginBlock(11)) // Head=10 => next=11
	require.NotNil(t, s.PendingBlock)
	assert.Equal(t, BlockNumber(11), s.PendingBlock.Number)
	assert.Equal(t, compose.PeriodID(5), s.PendingBlock.PeriodID)
	assert.Equal(t, compose.SuperblockNumber(6), s.PendingBlock.SuperblockNumber)

	// Already open
	assert.ErrorIs(t, s.BeginBlock(12), ErrBlockAlreadyOpen)

	// Seal to clear pending
	require.NoError(t, s.EndBlock(mkHeader(11)))

	// Not sequential
	assert.ErrorIs(t, s.BeginBlock(13), ErrBlockNotSequential)
}

func TestSequencer_CanIncludeLocalTx_and_InstanceHooks(t *testing.T) {
	s, _, _ := newSequencerForTest(compose.PeriodID(7), compose.SuperblockNumber(8), mkSettled(2, 20))

	// No pending block -> false, NoPendingBlock
	ok, err := s.CanIncludeLocalTx()
	require.ErrorIs(t, err, NoPendingBlock)
	assert.False(t, ok)
	assert.ErrorIs(t, err, NoPendingBlock)

	// Open block
	require.NoError(t, s.BeginBlock(21))

	// No active instance -> can include
	ok, err = s.CanIncludeLocalTx()
	require.NoError(t, err)
	assert.True(t, ok)

	// Start instance -> now blocked
	var id compose.InstanceID = compose.InstanceID{1}
	require.NoError(t, s.OnStartInstance(id))
	ok, err = s.CanIncludeLocalTx()
	require.NoError(t, err)
	assert.False(t, ok)

	// Wrong decided id -> mismatch
	assert.ErrorIs(t, s.OnDecidedInstance(compose.InstanceID{2}), ErrActiveInstanceMismatch)

	// Correct decided id -> unblocks
	require.NoError(t, s.OnDecidedInstance(id))
	ok, err = s.CanIncludeLocalTx()
	require.NoError(t, err)
	assert.True(t, ok)

	// No active instance now
	assert.ErrorIs(t, s.OnDecidedInstance(id), ErrNoActiveInstance)
}

func TestSequencer_EndBlock_seals_and_updates_head(t *testing.T) {
	s, _, _ := newSequencerForTest(compose.PeriodID(3), compose.SuperblockNumber(4), mkSettled(1, 30))

	require.NoError(t, s.BeginBlock(31))
	// Seal mismatch
	assert.ErrorIs(t, s.EndBlock(mkHeader(32)), ErrBlockSealMismatch)

	// Seal ok
	require.NoError(t, s.EndBlock(mkHeader(31)))
	assert.Nil(t, s.PendingBlock)
	assert.Equal(t, BlockNumber(31), s.Head)
	// SealedBlockHead entry for current period exists
	sb, ok := s.SealedBlockHead[s.PeriodID]
	require.True(t, ok)
	assert.Equal(t, BlockNumber(31), sb.BlockHeader.Number)
}

func TestSequencer_EndBlock_triggers_prev_period_settlement(t *testing.T) {
	// Start at period 9, target 10, settled head 40
	s, p, messenger := newSequencerForTest(compose.PeriodID(9), compose.SuperblockNumber(10), mkSettled(5, 40))
	p.nextProof = []byte("seq-proof")

	// Build a block in period 9
	require.NoError(t, s.BeginBlock(41))

	// Period rolls to 10 while block is open; no immediate prover call due to pending block
	require.NoError(t, s.StartPeriod(compose.PeriodID(10), compose.SuperblockNumber(11)))
	assert.Len(t, p.calls, 0)

	// Seal the block → should trigger settlement for period 9 with sb target (11-1)=10
	require.NoError(t, s.EndBlock(mkHeader(41)))
	require.Len(t, p.calls, 1)
	call := p.calls[0]
	if assert.NotNil(t, call.hdr) {
		assert.Equal(t, BlockNumber(41), call.hdr.Number)
	}
	assert.Equal(t, compose.SuperblockNumber(10), call.sb)

	// Check that sequencer sent proof message
	require.Len(t, messenger.proofs, 1)
	assert.Equal(t, compose.PeriodID(9), messenger.proofs[0].periodID)
	assert.Equal(t, compose.SuperblockNumber(10), messenger.proofs[0].superblockNumber)
	assert.Equal(t, []byte("seq-proof"), messenger.proofs[0].proof)
}

func TestSequencer_StartPeriod_triggers_immediate_settlement_when_no_pending(t *testing.T) {
	// Period 10, target 11, settled at 50
	s, p, messenger := newSequencerForTest(compose.PeriodID(10), compose.SuperblockNumber(11), mkSettled(6, 50))
	p.nextProof = []byte("seq-proof")

	// Case 1: no sealed block for previous period → nil header
	require.NoError(t, s.StartPeriod(compose.PeriodID(11), compose.SuperblockNumber(12)))
	require.Len(t, p.calls, 1)
	// Prover was called with nil header and superblock number 11
	assert.Nil(t, p.calls[0].hdr)
	assert.Equal(t, compose.SuperblockNumber(11), p.calls[0].sb)
	// Check that proof was sent to SP
	require.Len(t, messenger.proofs, 1)
	assert.Equal(t, compose.PeriodID(10), messenger.proofs[0].periodID)
	assert.Equal(t, compose.SuperblockNumber(11), messenger.proofs[0].superblockNumber)
	assert.Equal(t, []byte("seq-proof"), messenger.proofs[0].proof)

	// Case 2: add sealed block for previous period, then start next period
	p.calls = nil
	messenger.proofs = nil
	// Creates a block for superblock 12
	s.SealedBlockHead[11] = SealedBlockHeader{
		BlockHeader:      mkHeader(51),
		PeriodID:         11,
		SuperblockNumber: 12,
	}
	require.NoError(t, s.StartPeriod(compose.PeriodID(12), compose.SuperblockNumber(13)))
	require.Len(t, p.calls, 1)
	// Check prover call content
	if assert.NotNil(t, p.calls[0].hdr) {
		assert.Equal(t, BlockNumber(51), p.calls[0].hdr.Number)
	}
	assert.Equal(t, compose.SuperblockNumber(12), p.calls[0].sb)
	// Check that proof was sent to SP
	require.Len(t, messenger.proofs, 1)
	assert.Equal(t, compose.PeriodID(11), messenger.proofs[0].periodID)
	assert.Equal(t, compose.SuperblockNumber(12), messenger.proofs[0].superblockNumber)
	assert.Equal(t, []byte("seq-proof"), messenger.proofs[0].proof)
}

func TestSequencer_StartPeriod_active_instance_does_not_defer_in_impl(t *testing.T) {
	// Spec would defer on active instance; impl only checks PendingBlock.
	s, p, _ := newSequencerForTest(compose.PeriodID(2), compose.SuperblockNumber(3), mkSettled(1, 10))
	// No pending block, set active instance only
	s.ActiveInstanceID = &compose.InstanceID{1}

	require.NoError(t, s.StartPeriod(compose.PeriodID(3), compose.SuperblockNumber(4)))
	// Implementation triggers immediately
	require.Len(t, p.calls, 1)
}

func TestSequencer_ReceiveXTRequest_forwardsToPublisher(t *testing.T) {
	s, _, messenger := newSequencerForTest(compose.PeriodID(4), compose.SuperblockNumber(5), mkSettled(2, 10))
	req := makeXTRequest(
		chainReq(1, []byte("a")),
		chainReq(2, []byte("b")),
	)

	s.ReceiveXTRequest(req)

	require.Len(t, messenger.requests, 1)
	assert.Equal(t, req, messenger.requests[0])
}

func TestSequencer_AdvanceSettledState_monotonic(t *testing.T) {
	s, _, _ := newSequencerForTest(compose.PeriodID(1), compose.SuperblockNumber(2), mkSettled(1, 5))
	// No update for same number
	s.AdvanceSettledState(mkSettled(1, 5))
	// Advance forward
	s.AdvanceSettledState(mkSettled(2, 6))
}

func TestSequencer_Rollback_rejects_if_mismatch(t *testing.T) {
	settled := mkSettled(4, 100)
	s, _, _ := newSequencerForTest(compose.PeriodID(9), compose.SuperblockNumber(10), settled)

	_, err := s.Rollback(5, compose.SuperBlockHash{9}, compose.PeriodID(9))
	require.ErrorIs(t, err, ErrMismatchedFinalizedState)
}

func TestSequencer_Rollback_discards_newer_and_resets(t *testing.T) {
	settled := mkSettled(4, 100)
	s, _, _ := newSequencerForTest(compose.PeriodID(9), compose.SuperblockNumber(10), settled)

	// Seed sealed blocks across periods with various targets
	s.SealedBlockHead[8] = SealedBlockHeader{
		BlockHeader:      mkHeader(90),
		PeriodID:         8,
		SuperblockNumber: 3,
	}
	s.SealedBlockHead[9] = SealedBlockHeader{
		BlockHeader:      mkHeader(95),
		PeriodID:         9,
		SuperblockNumber: 4,
	}
	s.SealedBlockHead[10] = SealedBlockHeader{
		BlockHeader:      mkHeader(99),
		PeriodID:         10,
		SuperblockNumber: 5,
	}

	// Open and set an active instance to ensure they are cleared
	require.NoError(t, s.BeginBlock(101))
	id := compose.InstanceID{7}
	require.NoError(t, s.OnStartInstance(id))

	head, err := s.Rollback(4, settled.SuperblockHash, compose.PeriodID(12))
	require.NoError(t, err)
	assert.Equal(t, BlockNumber(100), head.Number)
	assert.Nil(t, s.PendingBlock)
	assert.Nil(t, s.ActiveInstanceID)
	// Blocks with SB > 4 removed
	_, ok := s.SealedBlockHead[10]
	assert.False(t, ok)
	_, ok = s.SealedBlockHead[9]
	assert.True(t, ok)
	// Period and target updated
	assert.Equal(t, compose.PeriodID(12), s.PeriodID)
	assert.Equal(t, compose.SuperblockNumber(5), s.TargetSuperblockNumber)
}
