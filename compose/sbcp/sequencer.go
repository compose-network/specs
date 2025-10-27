package sbcp

import (
	"errors"
	"sync"

	"github.com/compose-network/specs/compose"
)

var (
	ErrBlockSealMismatch = errors.New(
		"block number to be sealed does not match the current block number",
	)
	ErrBlockAlreadyOpen         = errors.New("there is already an open block")
	ErrBlockNotSequential       = errors.New("block number is not sequential")
	NoPendingBlock              = errors.New("no pending block")
	ErrActiveInstanceExists     = errors.New("there is already an active instance")
	ErrNoActiveInstance         = errors.New("no active instance")
	ErrActiveInstanceMismatch   = errors.New("mismatched active instance ID")
	ErrMismatchedFinalizedState = errors.New("mismatched finalized state")
)

type Sequencer interface {
	// StartPeriod and Rollback are called when the publisher sends their respective messages.
	StartPeriod(periodID compose.PeriodID, targetSuperblockNumber compose.SuperblockNumber) error
	Rollback(
		superblockNumber compose.SuperblockNumber,
		superblockHash compose.SuperBlockHash,
		currentPeriodID compose.PeriodID,
	) (BlockHeader, error)

	// AdvanceSettledState is called when the L1 settlement event has occurred.
	AdvanceSettledState(SettledState)

	// Block builder policy
	// BeginBlock is called at start of a new block
	BeginBlock(blockNumber BlockNumber) error
	// CanIncludeLocalTx return whether a local tx is admissible right now.
	CanIncludeLocalTx() (include bool, err error)
	// OnStartInstance is an SCP start-up hook. Locks local txs from being added (internal logic).
	OnStartInstance(id compose.InstanceID) error
	// OnDecidedInstance is an SCP decision hook. Unlocks local txs (internal logic).
	OnDecidedInstance(id compose.InstanceID) error
	// EndBlock: hook for when block ends
	EndBlock(b BlockHeader) error
}

type Prover interface {
	// RequestProofs starts the settlement pipeline using the provided block header as head.
	// If nil, it means there's no sealed block for the period.
	RequestProofs(blockHeader *BlockHeader, superblockNumber compose.SuperblockNumber)
}

type SequencerState struct {
	PeriodID               compose.PeriodID
	TargetSuperblockNumber compose.SuperblockNumber // from StartPeriod.target_superblock_number

	// PendingBlock represents the block being built.
	PendingBlock     *PendingBlock
	ActiveInstanceID *compose.InstanceID // nil if no active instance

	// Head represents the highest sealed block number.
	Head BlockNumber

	SealedBlockHead map[compose.PeriodID]SealedBlockHeader
	SettledState    SettledState
}

type sequencer struct {
	mu     sync.Mutex
	prover Prover
	SequencerState
}

func NewSequencer(
	prover Prover,
	periodID compose.PeriodID,
	targetSuperblock compose.SuperblockNumber,
	settledState SettledState,
) Sequencer {
	return &sequencer{
		mu:     sync.Mutex{},
		prover: prover,
		SequencerState: SequencerState{
			PeriodID:               periodID,
			TargetSuperblockNumber: targetSuperblock,
			PendingBlock:           nil,
			ActiveInstanceID:       nil,
			Head:                   settledState.BlockHeader.Number,
			SealedBlockHead:        make(map[compose.PeriodID]SealedBlockHeader),
			SettledState:           settledState,
		},
	}
}

// StartPeriod starts a new period, which triggers the settlement pipeline if there's no active block.
func (s *sequencer) StartPeriod(
	periodID compose.PeriodID,
	targetSuperblockNumber compose.SuperblockNumber,
) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.PeriodID = periodID
	s.TargetSuperblockNumber = targetSuperblockNumber

	// If there is an active block (with periodID P-1), the settlement pipeline for P-1 must wait until it's sealed.
	// Else, it can be triggered right away.
	if s.PendingBlock == nil {
		var header *BlockHeader
		block, ok := s.SealedBlockHead[s.PeriodID-1]
		if ok {
			header = &block.BlockHeader
		}
		s.prover.RequestProofs(header, targetSuperblockNumber-1)
	}
	return nil
}

// BeginBlock is a hook called at the start of a new L2 block.
func (s *sequencer) BeginBlock(blockNumber BlockNumber) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.PendingBlock != nil {
		return ErrBlockAlreadyOpen
	}

	if blockNumber != s.Head+1 {
		return ErrBlockNotSequential
	}

	// Add immutable tags to the new block
	s.PendingBlock = &PendingBlock{
		Number:           blockNumber,
		PeriodID:         s.PeriodID,
		SuperblockNumber: s.TargetSuperblockNumber,
	}
	return nil
}

// CanIncludeLocalTx returns whether a local tx is admissible right now.
func (s *sequencer) CanIncludeLocalTx() (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.PendingBlock == nil {
		return false, NoPendingBlock
	}
	return s.ActiveInstanceID == nil, nil
}

// OnStartInstance sets an active instance, locking local tx inclusion (SCP start-up hook).
func (s *sequencer) OnStartInstance(id compose.InstanceID) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.PendingBlock == nil {
		return NoPendingBlock
	}
	if s.ActiveInstanceID != nil {
		return ErrActiveInstanceExists
	}
	s.ActiveInstanceID = &id
	return nil
}

// OnDecidedInstance sets the active instance to nil, unlocking local tx inclusion (SCP decision hook).
func (s *sequencer) OnDecidedInstance(id compose.InstanceID) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.ActiveInstanceID == nil {
		return ErrNoActiveInstance
	}
	if *s.ActiveInstanceID != id {
		return ErrActiveInstanceMismatch
	}
	s.ActiveInstanceID = nil
	return nil
}

func (s *sequencer) EndBlock(b BlockHeader) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.PendingBlock.Number != b.Number {
		return ErrBlockSealMismatch
	}

	s.SealedBlockHead[s.PendingBlock.PeriodID] = SealedBlockHeader{
		BlockHeader:      b,
		PeriodID:         s.PendingBlock.PeriodID,
		SuperblockNumber: s.PendingBlock.SuperblockNumber,
	}

	// A block from the previous period has ended, which means the period has ended,
	// therefore it's time to request proofs for it.
	if s.PendingBlock.PeriodID < s.PeriodID {
		var header *BlockHeader
		block, ok := s.SealedBlockHead[s.PeriodID-1]
		if ok {
			header = &block.BlockHeader
		}
		s.prover.RequestProofs(header, s.TargetSuperblockNumber-1)
	}

	s.PendingBlock = nil
	s.Head = b.Number
	return nil
}

// AdvanceSettledState advances the settled state to the given block header.
func (s *sequencer) AdvanceSettledState(settledBlock SettledState) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if settledBlock.SuperblockNumber <= s.SettledState.SuperblockNumber {
		return
	}
	s.SettledState = settledBlock
}

// Rollback message is sent by the publisher to all sequencers.
// The sequencer must erase blocks beyond the given superblock number and hash, and return the safe head block header.
func (s *sequencer) Rollback(
	superblockNumber compose.SuperblockNumber,
	superblockHash compose.SuperBlockHash,
	currentPeriodID compose.PeriodID,
) (BlockHeader, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !(superblockNumber == s.SettledState.SuperblockNumber && superblockHash == s.SettledState.SuperblockHash) {
		return BlockHeader{}, ErrMismatchedFinalizedState
	}

	// Discard blocks with superblock number greater than the finalized one.
	for blockPeriodID, sealedBlock := range s.SealedBlockHead {
		if sealedBlock.SuperblockNumber > s.SettledState.SuperblockNumber {
			delete(s.SealedBlockHead, blockPeriodID)
		}
	}

	// Discard current block and active instance
	s.PendingBlock = nil
	s.ActiveInstanceID = nil
	s.Head = s.SettledState.BlockHeader.Number

	// Reset period and target superblock number
	s.PeriodID = currentPeriodID
	s.TargetSuperblockNumber = s.SettledState.SuperblockNumber + 1

	return s.SettledState.BlockHeader, nil
}
