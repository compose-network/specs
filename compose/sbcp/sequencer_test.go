package sbcp

import (
	"github.com/compose-network/specs/compose"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSequencer_NewSequencer_initial_state(t *testing.T) {
	p := &fakeProver{}
	settled := mkSettled(4, 100)
	s := NewSequencer(p, 10, 11, settled)

	// Downcast to access state for assertions
	impl := s.(*sequencer)
	assert.Equal(t, compose.PeriodID(10), impl.PeriodID)
	assert.Equal(t, compose.SuperblockNumber(11), impl.TargetSuperblockNumber)
	assert.Equal(t, BlockNumber(100), impl.Head)
	assert.Nil(t, impl.PendingBlock)
	assert.Nil(t, impl.ActiveInstanceID)
	assert.Empty(t, impl.SealedBlocks)
}

func TestSequencer_BeginBlock_ok_and_errors(t *testing.T) {
	p := &fakeProver{}
	s := NewSequencer(p, 5, 6, mkSettled(3, 10)).(*sequencer)

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
	p := &fakeProver{}
	s := NewSequencer(p, 7, 8, mkSettled(2, 20)).(*sequencer)

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
	p := &fakeProver{}
	s := NewSequencer(p, 3, 4, mkSettled(1, 30)).(*sequencer)

	require.NoError(t, s.BeginBlock(31))
	// Seal mismatch
	assert.ErrorIs(t, s.EndBlock(mkHeader(32)), ErrBlockSealMismatch)

	// Seal ok
	require.NoError(t, s.EndBlock(mkHeader(31)))
	assert.Nil(t, s.PendingBlock)
	assert.Equal(t, BlockNumber(31), s.Head)
	// SealedBlocks entry for current period exists
	sb, ok := s.SealedBlocks[s.PeriodID]
	require.True(t, ok)
	assert.Equal(t, BlockNumber(31), sb.BlockHeader.Number)
}

func TestSequencer_EndBlock_triggers_prev_period_settlement(t *testing.T) {
	p := &fakeProver{}
	// Start at period 9, target 10, settled head 40
	s := NewSequencer(p, 9, 10, mkSettled(5, 40)).(*sequencer)

	// Build a block in period 9
	require.NoError(t, s.BeginBlock(41))

	// Period rolls to 10 while block is open; no immediate prover call due to pending block
	require.NoError(t, s.StartPeriod(10, 11))
	assert.Len(t, p.calls, 0)

	// Seal the block → should trigger settlement for period 9 with sb target (11-1)=10
	require.NoError(t, s.EndBlock(mkHeader(41)))
	require.Len(t, p.calls, 1)
	call := p.calls[0]
	if assert.NotNil(t, call.hdr) {
		assert.Equal(t, BlockNumber(41), call.hdr.Number)
	}
	assert.Equal(t, compose.SuperblockNumber(10), call.sb)
}

func TestSequencer_StartPeriod_triggers_immediate_settlement_when_no_pending(t *testing.T) {
	p := &fakeProver{}
	// Period 10, target 11, settled at 50
	s := NewSequencer(p, 10, 11, mkSettled(6, 50)).(*sequencer)

	// Case 1: no sealed block for previous period → nil header
	require.NoError(t, s.StartPeriod(11, 12))
	require.Len(t, p.calls, 1)
	assert.Nil(t, p.calls[0].hdr)
	assert.Equal(t, compose.SuperblockNumber(11), p.calls[0].sb)

	// Case 2: add sealed block for previous period, then start next period
	p.calls = nil
	s.SealedBlocks[11] = SealedBlockHeader{
		BlockHeader:      mkHeader(51),
		PeriodID:         11,
		SuperblockNumber: 12,
	}
	require.NoError(t, s.StartPeriod(12, 13))
	require.Len(t, p.calls, 1)
	if assert.NotNil(t, p.calls[0].hdr) {
		assert.Equal(t, BlockNumber(51), p.calls[0].hdr.Number)
	}
	assert.Equal(t, compose.SuperblockNumber(12), p.calls[0].sb)
}

func TestSequencer_StartPeriod_active_instance_does_not_defer_in_impl(t *testing.T) {
	// Spec would defer on active instance; impl only checks PendingBlock.
	p := &fakeProver{}
	s := NewSequencer(p, 2, 3, mkSettled(1, 10)).(*sequencer)
	// No pending block, set active instance only
	s.ActiveInstanceID = &compose.InstanceID{1}

	require.NoError(t, s.StartPeriod(3, 4))
	// Implementation triggers immediately
	require.Len(t, p.calls, 1)
}

func TestSequencer_AdvanceSettledState_monotonic(t *testing.T) {
	p := &fakeProver{}
	s := NewSequencer(p, 1, 2, mkSettled(1, 5)).(*sequencer)
	// No update for same number
	s.AdvanceSettledState(mkSettled(1, 5))
	// Advance forward
	s.AdvanceSettledState(mkSettled(2, 6))
}

func TestSequencer_Rollback_rejects_if_mismatch(t *testing.T) {
	p := &fakeProver{}
	settled := mkSettled(4, 100)
	s := NewSequencer(p, 9, 10, settled).(*sequencer)

	_, err := s.Rollback(5, compose.SuperBlockHash{9}, 9)
	require.ErrorIs(t, err, ErrMismatchedFinalizedState)
}

func TestSequencer_Rollback_discards_newer_and_resets(t *testing.T) {
	p := &fakeProver{}
	settled := mkSettled(4, 100)
	s := NewSequencer(p, 9, 10, settled).(*sequencer)

	// Seed sealed blocks across periods with various targets
	s.SealedBlocks[8] = SealedBlockHeader{
		BlockHeader:      mkHeader(90),
		PeriodID:         8,
		SuperblockNumber: 3,
	}
	s.SealedBlocks[9] = SealedBlockHeader{
		BlockHeader:      mkHeader(95),
		PeriodID:         9,
		SuperblockNumber: 4,
	}
	s.SealedBlocks[10] = SealedBlockHeader{
		BlockHeader:      mkHeader(99),
		PeriodID:         10,
		SuperblockNumber: 5,
	}

	// Open and set an active instance to ensure they are cleared
	require.NoError(t, s.BeginBlock(101))
	id := compose.InstanceID{7}
	require.NoError(t, s.OnStartInstance(id))

	head, err := s.Rollback(4, settled.SuperblockHash, 12)
	require.NoError(t, err)
	assert.Equal(t, BlockNumber(100), head.Number)
	assert.Nil(t, s.PendingBlock)
	assert.Nil(t, s.ActiveInstanceID)
	// Blocks with SB > 4 removed
	_, ok := s.SealedBlocks[10]
	assert.False(t, ok)
	_, ok = s.SealedBlocks[9]
	assert.True(t, ok)
	// Period and target updated
	assert.Equal(t, compose.PeriodID(12), s.PeriodID)
	assert.Equal(t, compose.SuperblockNumber(5), s.TargetSuperblockNumber)
}
