package sbcp

import (
	"context"
	"errors"
	"sync"

	"github.com/compose-network/specs/compose"
	"github.com/rs/zerolog"
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
	ErrPeriodIDMismatch         = errors.New("instance period ID does not match current block period ID")
	ErrLowSequencerNumber       = errors.New("instance sequence number is not greater than last sequence number")
)

type Sequencer interface {
	// StartPeriod and Rollback are called when the publisher sends their respective messages.
	StartPeriod(ctx context.Context, periodID compose.PeriodID, targetSuperblockNumber compose.SuperblockNumber) error
	Rollback(
		superblockNumber compose.SuperblockNumber,
		superblockHash compose.SuperblockHash,
		currentPeriodID compose.PeriodID,
	) (BlockHeader, error)

	// ReceiveXTRequest is called whenever a request from a user is received.
	ReceiveXTRequest(ctx context.Context, request compose.XTRequest) error

	// AdvanceSettledState is called when the L1 settlement event has occurred.
	AdvanceSettledState(SettledState)

	// Block builder policy
	// BeginBlock is called at start of a new block
	BeginBlock(blockNumber BlockNumber) error
	// CanIncludeLocalTx return whether a local tx is admissible right now.
	CanIncludeLocalTx() (include bool, err error)
	// OnStartInstance is an SCP start-up hook. Locks local txs from being added (internal logic).
	OnStartInstance(id compose.InstanceID, periodID compose.PeriodID, sequenceNumber compose.SequenceNumber) error
	// OnDecidedInstance is an SCP decision hook. Unlocks local txs (internal logic).
	OnDecidedInstance(id compose.InstanceID) error
	// EndBlock: hook for when block ends
	EndBlock(ctx context.Context, b BlockHeader) error
}

type SequencerProver interface {
	// RequestProofs starts the settlement pipeline using the provided block header as head.
	// If nil, it means there's no sealed block for the period.
	RequestProofs(ctx context.Context, blockHeader *BlockHeader, superblockNumber compose.SuperblockNumber) ([]byte, error)
}

type SequencerMessenger interface {
	ForwardRequest(ctx context.Context, request compose.XTRequest) error
	SendProof(ctx context.Context, periodID compose.PeriodID, superblockNumber compose.SuperblockNumber, proof []byte) error
}

type SequencerState struct {
	PeriodID               compose.PeriodID
	TargetSuperblockNumber compose.SuperblockNumber // from StartPeriod.target_superblock_number

	// PendingBlock represents the block being built.
	PendingBlock       *PendingBlock
	ActiveInstanceID   *compose.InstanceID     // nil if no active instance
	LastSequenceNumber *compose.SequenceNumber // nil if no started instance in this period

	// Head represents the highest sealed block number.
	Head BlockNumber

	SealedBlockHead map[compose.PeriodID]SealedBlockHeader
	SettledState    SettledState

	logger zerolog.Logger
}

type sequencer struct {
	mu        sync.Mutex
	prover    SequencerProver
	messenger SequencerMessenger
	SequencerState
}

func NewSequencer(
	prover SequencerProver,
	messenger SequencerMessenger,
	periodID compose.PeriodID,
	targetSuperblock compose.SuperblockNumber,
	settledState SettledState,
	logger zerolog.Logger,
) Sequencer {
	return &sequencer{
		mu:        sync.Mutex{},
		prover:    prover,
		messenger: messenger,
		SequencerState: SequencerState{
			PeriodID:               periodID,
			TargetSuperblockNumber: targetSuperblock,
			PendingBlock:           nil,
			ActiveInstanceID:       nil,
			LastSequenceNumber:     nil,
			Head:                   settledState.BlockHeader.Number,
			SealedBlockHead:        make(map[compose.PeriodID]SealedBlockHeader),
			SettledState:           settledState,
			logger:                 logger,
		},
	}
}

// ReceiveXTRequest is called whenever a request from a user is received.
// It should be forwarded to the publisher, who has the rights of starting an instance for it.
func (s *sequencer) ReceiveXTRequest(ctx context.Context, request compose.XTRequest) error {
	return s.messenger.ForwardRequest(ctx, request)
}

// StartPeriod starts a new period, which triggers the settlement pipeline if there's no active block.
func (s *sequencer) StartPeriod(
	ctx context.Context,
	periodID compose.PeriodID,
	targetSuperblockNumber compose.SuperblockNumber,
) error {
	s.mu.Lock()

	s.logger.Info().
		Uint64("new_period_id", uint64(periodID)).
		Uint64("target_superblock_number", uint64(targetSuperblockNumber)).
		Msg("Starting new period")

	s.PeriodID = periodID
	s.TargetSuperblockNumber = targetSuperblockNumber
	s.LastSequenceNumber = nil
	noPendingBlock := s.PendingBlock == nil

	s.mu.Unlock()

	// If there is an active block (with periodID PeriodID-1), the settlement pipeline for PeriodID-1 must wait until it's sealed.
	// Else, it can be triggered right away.
	if noPendingBlock {
		s.logger.Info().Msg("No pending block, triggering settlement pipeline")
		return s.startSettlement(ctx, periodID-1, targetSuperblockNumber-1)
	}

	s.logger.Info().Msg("Started new period, but pending block exists, settlement pipeline will wait")
	return nil
}

// startSettlement starts the settlement pipeline for the given period.
// It requests a proof from the prover. Note that this operation may take a while and thus it is done outside locks.
// Then, it sends the proof to the SP.
func (s *sequencer) startSettlement(ctx context.Context, periodID compose.PeriodID, superblockNumber compose.SuperblockNumber) error {
	s.mu.Lock()
	var header *BlockHeader
	block, ok := s.SealedBlockHead[periodID]
	if ok {
		header = &block.BlockHeader
	}
	s.mu.Unlock()
	// Request proof to prover
	proof, err := s.prover.RequestProofs(ctx, header, superblockNumber)
	if err != nil {
		s.logger.Error().Err(err).Msg("failed to request proofs from prover")
		return err
	}
	// Send proof to SP
	return s.messenger.SendProof(ctx, periodID, superblockNumber, proof)
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

	s.logger.Info().Uint64("new_block_number", uint64(blockNumber)).Msg("Beginning block")

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
func (s *sequencer) OnStartInstance(id compose.InstanceID, periodID compose.PeriodID, sequenceNumber compose.SequenceNumber) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.PendingBlock == nil {
		return NoPendingBlock
	}
	if s.ActiveInstanceID != nil {
		return ErrActiveInstanceExists
	}

	if s.PendingBlock.PeriodID != periodID {
		return ErrPeriodIDMismatch
	}

	if s.LastSequenceNumber != nil {
		if sequenceNumber <= *s.LastSequenceNumber {
			return ErrLowSequencerNumber
		}
	}

	s.LastSequenceNumber = &sequenceNumber

	s.logger.Info().
		Msg("Starting active instance, locking local tx inclusion")
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
	s.logger.Info().
		Msg("Decided active instance, unlocking local tx inclusion")
	s.ActiveInstanceID = nil
	return nil
}

func (s *sequencer) EndBlock(ctx context.Context, b BlockHeader) error {
	s.mu.Lock()
	if s.PendingBlock == nil {
		s.mu.Unlock()
		return NoPendingBlock
	}
	if s.PendingBlock.Number != b.Number {
		s.mu.Unlock()
		return ErrBlockSealMismatch
	}
	if s.ActiveInstanceID != nil {
		s.mu.Unlock()
		return ErrActiveInstanceExists
	}

	s.logger.Info().Msg("Ending block")
	s.SealedBlockHead[s.PendingBlock.PeriodID] = SealedBlockHeader{
		BlockHeader:      b,
		PeriodID:         s.PendingBlock.PeriodID,
		SuperblockNumber: s.PendingBlock.SuperblockNumber,
	}

	shouldStartSettlement := s.PendingBlock.PeriodID < s.PeriodID
	settlementPeriod := s.PeriodID - 1
	settlementSuperblock := s.TargetSuperblockNumber - 1

	s.PendingBlock = nil
	s.Head = b.Number

	s.mu.Unlock()

	// A block from the previous period has ended, which means the period has also ended,
	// therefore it's time to request proofs for it.
	if shouldStartSettlement {
		s.logger.Info().Msg("Period was ahead of sealed block, triggering settlement pipeline")
		return s.startSettlement(ctx, settlementPeriod, settlementSuperblock)
	}
	return nil
}

// AdvanceSettledState advances the settled state to the given block header.
func (s *sequencer) AdvanceSettledState(settledBlock SettledState) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if settledBlock.SuperblockNumber <= s.SettledState.SuperblockNumber {
		return
	}
	s.logger.Info().
		Uint64("new_settled_superblock_number", uint64(settledBlock.SuperblockNumber)).
		Msg("Advancing settled state")
	s.SettledState = settledBlock
}

// Rollback message is sent by the publisher to all sequencers.
// The sequencer must erase blocks beyond the given superblock number and hash, and return the safe head block header.
func (s *sequencer) Rollback(
	superblockNumber compose.SuperblockNumber,
	superblockHash compose.SuperblockHash,
	currentPeriodID compose.PeriodID,
) (BlockHeader, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !(superblockNumber == s.SettledState.SuperblockNumber && superblockHash == s.SettledState.SuperblockHash) {
		return BlockHeader{}, ErrMismatchedFinalizedState
	}

	s.logger.Info().
		Uint64("rollback_superblock_number", uint64(superblockNumber)).
		Msg("Rolling back to settled state")

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
