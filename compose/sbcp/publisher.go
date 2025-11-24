package sbcp

import (
	"errors"
	"fmt"
	"sync"

	"github.com/compose-network/specs/compose"
	"github.com/rs/zerolog"
)

var (
	ErrCannotStartInstance = errors.New("can not start any instance")
	ErrCannotStartPeriod   = errors.New("can not start period")
	ErrChainNotActive      = errors.New("chain not active")
	ErrOldSettledState     = errors.New("can not advance to older settled state")
	ErrInvalidRequest      = errors.New("invalid request")
)

type Publisher interface {
	// StartPeriod is called whenever a new period starts (i.e. CurrEthereumEpoch % 10 == 0).
	StartPeriod() error
	// StartInstance is called by the upper layer to try starting a new instance from the queued requests.
	StartInstance(req compose.XTRequest) (compose.Instance, error)
	// DecideInstance is called once an instance gets decided.
	DecideInstance(instance compose.Instance) error
	// AdvanceSettledState is called when L1 emits a new settled state event
	AdvanceSettledState(
		superblockNumber compose.SuperblockNumber,
		superblockHash compose.SuperblockHash,
	) error
	// ProofTimeout: Once a period starts, if the network ZK proof is not generated within 9 epochs,
	// the publisher must roll back to the last finalized superblock and discard any active settlement pipeline.
	ProofTimeout()
	// ReceiveProof is called whenever a proof is received from a sequencer.
	ReceiveProof(periodID compose.PeriodID, superblockNumber compose.SuperblockNumber, proof []byte, chainID compose.ChainID)
}

type PublisherProver interface {
	// RequestNetworkProof requests a proof for the given superblock number. It's called after all proofs from sequencers have been received.
	RequestNetworkProof(superblockNumber compose.SuperblockNumber, lastSuperblockHash compose.SuperblockHash, proofs [][]byte) ([]byte, error)
}

type PublisherMessenger interface {
	BroadcastStartPeriod(periodID compose.PeriodID, targetSuperblockNumber compose.SuperblockNumber)
	BroadcastRollback(
		periodID compose.PeriodID,
		superblockNumber compose.SuperblockNumber,
		superblockHash compose.SuperblockHash,
	)
}

type L1 interface {
	PublishProof(superblockNumber compose.SuperblockNumber, proof []byte)
}

type PublisherState struct {
	PeriodID               compose.PeriodID
	TargetSuperblockNumber compose.SuperblockNumber

	// Settlement state
	LastFinalizedSuperblockNumber compose.SuperblockNumber
	LastFinalizedSuperblockHash   compose.SuperblockHash
	Proofs                        map[compose.SuperblockNumber]map[compose.ChainID][]byte
	Chains                        map[compose.ChainID]struct{}

	// Instances scheduling
	SequenceNumber compose.SequenceNumber   // Per-period sequence counter (monotone)
	ActiveChains   map[compose.ChainID]bool // Chains with active instances

	// Proof window duration (in number of superblocks/periods) through which a pending superblock can be proven.
	// StartPeriods are rejected if the next superblock is bigger than LastFinalizedSuperblockNumber + ProofWindow.
	// 0 value means no window constrain.
	ProofWindow uint64

	logger zerolog.Logger
}

type publisher struct {
	mu        sync.Mutex
	prover    PublisherProver
	messenger PublisherMessenger
	l1        L1
	PublisherState
}

// NewPublisher creates a new Publisher instance given a config, a period ID and the last settled state.
// TargetSuperblockNumber is set to lastFinalizedSuperblockNumber initially.
// StartPeriod needs to be called to start the first period, automatically incrementing PeriodID and TargetSuperblockNumber.
// Thus, if the current period is N, call NewPublisher with periodID = N-1.
func NewPublisher(
	prover PublisherProver,
	messenger PublisherMessenger,
	l1 L1,
	periodID compose.PeriodID,
	lastFinalizedSuperblockNumber compose.SuperblockNumber,
	lastFinalizedSuperblockHash compose.SuperblockHash,
	proofWindow uint64,
	logger zerolog.Logger,
	chains map[compose.ChainID]struct{},
) Publisher {
	return &publisher{
		mu:        sync.Mutex{},
		prover:    prover,
		messenger: messenger,
		l1:        l1,
		PublisherState: PublisherState{
			PeriodID:               periodID,
			TargetSuperblockNumber: lastFinalizedSuperblockNumber,

			// Settlement state
			LastFinalizedSuperblockNumber: lastFinalizedSuperblockNumber,
			LastFinalizedSuperblockHash:   lastFinalizedSuperblockHash,
			Proofs:                        make(map[compose.SuperblockNumber]map[compose.ChainID][]byte),
			Chains:                        chains,

			// Instances scheduling
			SequenceNumber: 0,
			ActiveChains:   make(map[compose.ChainID]bool),

			ProofWindow: proofWindow,

			logger: logger,
		},
	}
}

// StartPeriod is called whenever a new period starts (i.e. CurrEthereumEpoch % 10 == 0).
// SequenceNumber is reset to 0, though ActiveChains is kept because instances may exist through the boundary until finished.
func (p *publisher) StartPeriod() error {
	p.mu.Lock()
	defer p.mu.Unlock()

	nextSuperblock := p.TargetSuperblockNumber + 1

	// Proof window constrain
	// If the oldest pending superblock is older than ProofWindow, reject starting the new period
	// as the upper layer should have called ProofTimeout already.
	if p.ProofWindow != 0 { // 0 means no constrain
		if nextSuperblock > p.LastFinalizedSuperblockNumber+compose.SuperblockNumber(1+p.ProofWindow) {
			return fmt.Errorf("target superblock is %d, expected %d: %w",
				p.TargetSuperblockNumber, p.LastFinalizedSuperblockNumber+1, ErrCannotStartPeriod)
		}
	}

	p.PeriodID++
	p.TargetSuperblockNumber = nextSuperblock

	p.logger.Info().
		Uint64("new_period_id", uint64(p.PeriodID)).
		Uint64("target_superblock_number", uint64(p.TargetSuperblockNumber)).
		Msg("Starting new period")

	p.messenger.BroadcastStartPeriod(p.PeriodID, p.TargetSuperblockNumber)

	p.SequenceNumber = 0
	return nil
}

// ReceiveProof is called whenever a proof is received from a sequencer.
func (p *publisher) ReceiveProof(periodID compose.PeriodID, superblockNumber compose.SuperblockNumber, proof []byte, chainID compose.ChainID) {
	p.mu.Lock()

	// If the proof is for an old superblock, ignore it.
	if superblockNumber <= p.LastFinalizedSuperblockNumber {
		p.logger.Warn().
			Uint64("superblock_number", uint64(superblockNumber)).
			Uint64("chain_id", uint64(chainID)).
			Msg("Received proof for old superblock, ignoring")
		p.mu.Unlock()
		return
	}

	// If the proof is for an non-terminated superblock, ignore it.
	if superblockNumber >= p.TargetSuperblockNumber {
		p.logger.Warn().
			Uint64("superblock_number", uint64(superblockNumber)).
			Uint64("chain_id", uint64(chainID)).
			Msg("Received proof for non-terminated superblock, ignoring")
		p.mu.Unlock()
		return
	}

	// If the proof is for a superblock that is not the next one, ignore it.
	if superblockNumber != p.LastFinalizedSuperblockNumber+1 {
		p.logger.Warn().
			Uint64("superblock_number", uint64(superblockNumber)).
			Uint64("chain_id", uint64(chainID)).
			Msg("Received proof for superblock that is not the next one, ignoring")
		p.mu.Unlock()
		return
	}

	// Check period is correct
	periodDiff := p.TargetSuperblockNumber - superblockNumber
	expectedPeriod := p.PeriodID - compose.PeriodID(periodDiff)
	if periodID != expectedPeriod {
		p.logger.Warn().
			Uint64("superblock_number", uint64(superblockNumber)).
			Uint64("chain_id", uint64(chainID)).
			Uint64("expected_period", uint64(expectedPeriod)).
			Uint64("received_period", uint64(periodID)).
			Msg("Received proof for wrong period, ignoring")
		p.mu.Unlock()
		return
	}

	// If proof has already been received, ignore it.
	if _, ok := p.Proofs[superblockNumber]; !ok {
		p.Proofs[superblockNumber] = make(map[compose.ChainID][]byte)
	}
	if _, ok := p.Proofs[superblockNumber][chainID]; ok {
		p.logger.Warn().
			Uint64("superblock_number", uint64(superblockNumber)).
			Uint64("chain_id", uint64(chainID)).
			Msg("Already received proof , ignoring")
		p.mu.Unlock()
		return
	}

	p.Proofs[superblockNumber][chainID] = proof

	// If didn't receive enough proofs, continue waiting.
	if len(p.Proofs[superblockNumber]) < len(p.Chains) {
		p.logger.Info().
			Uint64("superblock_number", uint64(superblockNumber)).
			Uint64("chain_id", uint64(chainID)).
			Int("received_proofs", len(p.Proofs[superblockNumber])).
			Int("total_chains", len(p.Chains)).
			Msg("Received proof, waiting for more")
		p.mu.Unlock()
		return
	}

	p.logger.Info().
		Uint64("superblock_number", uint64(superblockNumber)).
		Uint64("chain_id", uint64(chainID)).
		Msg("Received enough proofs, generating proof")

	seqProofs := make([][]byte, 0)
	for _, seqProof := range p.Proofs[superblockNumber] {
		seqProofs = append(seqProofs, seqProof)
	}

	lastSuperblockHash := p.LastFinalizedSuperblockHash
	p.mu.Unlock()

	networkProof, err := p.prover.RequestNetworkProof(superblockNumber, lastSuperblockHash, seqProofs)
	if err != nil {
		p.logger.Error().
			Err(err).
			Uint64("superblock_number", uint64(superblockNumber)).
			Uint64("chain_id", uint64(chainID)).
			Msg("Failed to generate network proof. Triggering rollback")
		p.rollback()
		return
	}
	p.mu.Lock()
	p.Proofs[superblockNumber] = nil
	p.mu.Unlock()
	p.l1.PublishProof(superblockNumber, networkProof)
}

// StartInstance is called by the upper layer to try starting a new instance.
// If the instance can not be started, it returns an error.
// Else, it returns the created instance.
func (p *publisher) StartInstance(request compose.XTRequest) (compose.Instance, error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	// Requests must have at least 2 transactions
	if len(request.Transactions) < 2 {
		return compose.Instance{}, ErrInvalidRequest
	}

	chains := compose.ChainsFromRequest(request)
	// Can't start instance if any participant is already active
	if p.anyChainAlreadyActive(chains) {
		return compose.Instance{}, ErrCannotStartInstance
	}

	// Create instance
	p.SequenceNumber++
	instance := compose.Instance{
		ID: GenerateInstanceID(
			p.PeriodID,
			p.SequenceNumber,
			request,
		),
		PeriodID:       p.PeriodID,
		SequenceNumber: p.SequenceNumber,
		XTRequest:      request,
	}
	// Set chains as active
	for _, chainID := range chains {
		p.ActiveChains[chainID] = true
	}

	p.logger.Info().
		Str("instance_id", instance.ID.String()).
		Uint64("period_id", uint64(instance.PeriodID)).
		Uint64("sequence_number", uint64(instance.SequenceNumber)).
		Any("chains", chains).
		Msg("Starting new instance")

	return instance, nil
}

// DecideInstance removes the instance from being active.
func (p *publisher) DecideInstance(instance compose.Instance) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	chains := instance.Chains()

	// Check instance chains are really active
	for _, chainID := range chains {
		if _, ok := p.ActiveChains[chainID]; !ok {
			return ErrChainNotActive
		}
	}

	// Remove chains from active
	for _, chainID := range chains {
		delete(p.ActiveChains, chainID)
	}

	p.logger.Info().
		Str("instance_id", instance.ID.String()).
		Msg("Decided instance, removing active chains")

	return nil
}

// AdvanceSettledState is called when L1 emits a new settled state event
func (p *publisher) AdvanceSettledState(
	superblockNumber compose.SuperblockNumber,
	superblockHash compose.SuperblockHash,
) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	if superblockNumber <= p.LastFinalizedSuperblockNumber {
		return ErrOldSettledState
	}

	p.logger.Info().
		Uint64("new_finalized_superblock_number", uint64(superblockNumber)).
		Msg("Advancing finalized settled state")

	p.LastFinalizedSuperblockNumber = superblockNumber
	p.LastFinalizedSuperblockHash = superblockHash
	return nil
}

// ProofTimeout is called whenever a pending superblock is not proven within the allowed proof window.
// It triggers a rollback to the last finalized superblock, resetting the active chains and sequence number.
func (p *publisher) ProofTimeout() {
	p.logger.Info().
		Uint64("finalized_superblock_number", uint64(p.LastFinalizedSuperblockNumber)).
		Msg("Proof timeout occurred, rolling back to last finalized superblock")
	p.rollback()
}

func (p *publisher) rollback() {
	p.mu.Lock()
	defer p.mu.Unlock()

	p.ActiveChains = make(map[compose.ChainID]bool)
	p.SequenceNumber = 0
	p.TargetSuperblockNumber = p.LastFinalizedSuperblockNumber + 1
	p.messenger.BroadcastRollback(p.PeriodID, p.LastFinalizedSuperblockNumber, p.LastFinalizedSuperblockHash)

	// Clear proofs
	for superblockNumber := range p.Proofs {
		delete(p.Proofs, superblockNumber)
	}
}

// Util functions

func (p *publisher) anyChainAlreadyActive(chains []compose.ChainID) bool {
	// Caller must hold the p mutex
	// Check if any chain is already active
	for _, chainID := range chains {
		if p.ActiveChains[chainID] {
			return true
		}
	}
	return false
}
