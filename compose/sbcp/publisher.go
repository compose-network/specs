package sbcp

import (
	"errors"
	"fmt"
	"sync"

	"github.com/compose-network/specs/compose"
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
	StartInstance(req []compose.Transaction) (compose.Instance, error)
	// DecideInstance is called once an instance gets decided.
	DecideInstance(instance compose.Instance) error
	// AdvanceSettledState is called when L1 emits a new settled state event
	AdvanceSettledState(
		superblockNumber compose.SuperblockNumber,
		superblockHash compose.SuperBlockHash,
	) error
	// ProofTimeout: Once a period starts, if the network ZK proof is not generated within 9 epochs,
	// the publisher must roll back to the last finalized superblock and discard any active settlement pipeline.
	ProofTimeout()
}

type Messenger interface {
	BroadcastStartPeriod(periodID compose.PeriodID, targetSuperblockNumber compose.SuperblockNumber)
	BroadcastRollback(
		superblockNumber compose.SuperblockNumber,
		superblockHash compose.SuperBlockHash,
	)
}

type PublisherState struct {
	PeriodID               compose.PeriodID
	TargetSuperblockNumber compose.SuperblockNumber

	// Last finalized state in L1
	LastFinalizedSuperblockNumber compose.SuperblockNumber
	LastFinalizedSuperblockHash   compose.SuperBlockHash

	// Instances scheduling
	SequenceNumber compose.SequenceNumber   // Per-period sequence counter (monotone)
	ActiveChains   map[compose.ChainID]bool // Chains with active instances

	// Proof window duration (in number of superblocks/periods) through which a pending superblock can be proven.
	// StartPeriods are rejected if the next superblock is bigger than LastFinalizedSuperblockNumber + ProofWindow.
	// 0 value means no window constrain.
	ProofWindow uint64
}

type publisher struct {
	mu        sync.Mutex
	messenger Messenger
	PublisherState
}

// NewPublisher creates a new Publisher instance given a config, a period ID and the last settled state.
// TargetSuperblockNumber is set to lastFinalizedSuperblockNumber initially.
// StartPeriod needs to be called to start the first period, automatically incrementing PeriodID and TargetSuperblockNumber.
// Thus, if the current period is N, call NewPublisher with periodID = N-1.
func NewPublisher(messenger Messenger,
	periodID compose.PeriodID,
	lastFinalizedSuperblockNumber compose.SuperblockNumber,
	lastFinalizedSuperblockHash compose.SuperBlockHash,
	proofWindow uint64,
) Publisher {
	return &publisher{
		mu:        sync.Mutex{},
		messenger: messenger,
		PublisherState: PublisherState{
			PeriodID:               periodID,
			TargetSuperblockNumber: lastFinalizedSuperblockNumber,

			// Last finalized state in L1
			LastFinalizedSuperblockNumber: lastFinalizedSuperblockNumber,
			LastFinalizedSuperblockHash:   lastFinalizedSuperblockHash,

			// Instances scheduling
			SequenceNumber: 0,
			ActiveChains:   make(map[compose.ChainID]bool),

			ProofWindow: proofWindow,
		},
	}
}

// StartPeriod is called whenever a new period starts (i.e. CurrEthereumEpoch % 10 == 0).
// SequenceNumber is reset to 0, though ActiveChains is kept because instances may exist through the boundary until finished.
func (p *publisher) StartPeriod() error {
	p.mu.Lock()
	defer p.mu.Unlock()

	// Proof window constrain
	// If the oldest pending superblock is older than ProofWindow, reject starting the new period
	// as the upper layer should have called ProofTimeout already.
	nextSuperblock := p.TargetSuperblockNumber + 1
	if nextSuperblock > p.LastFinalizedSuperblockNumber+compose.SuperblockNumber(1+p.ProofWindow) {
		return fmt.Errorf("target superblock is %d, expected %d: %w",
			p.TargetSuperblockNumber, p.LastFinalizedSuperblockNumber+1, ErrCannotStartPeriod)
	}

	p.PeriodID++
	p.TargetSuperblockNumber = nextSuperblock

	p.messenger.BroadcastStartPeriod(p.PeriodID, p.TargetSuperblockNumber)

	p.SequenceNumber = 0
	return nil
}

// StartInstance is called by the upper layer to try starting a new instance.
// If the instance can not be started, it returns an error.
// Else, it returns the created instance.
func (p *publisher) StartInstance(request []compose.Transaction) (compose.Instance, error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	// Requests must have at least 2 transactions
	if len(request) < 2 {
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
		ID: generateInstanceID(
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
	return nil
}

// AdvanceSettledState is called when L1 emits a new settled state event
func (p *publisher) AdvanceSettledState(
	superblockNumber compose.SuperblockNumber,
	superblockHash compose.SuperBlockHash,
) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	if superblockNumber <= p.LastFinalizedSuperblockNumber {
		return ErrOldSettledState
	}
	p.LastFinalizedSuperblockNumber = superblockNumber
	p.LastFinalizedSuperblockHash = superblockHash
	return nil
}

// ProofTimeout is called whenever a pending superblock is not proven within the allowed proof window.
// It triggers a rollback to the last finalized superblock, resetting the active chains and sequence number.
func (p *publisher) ProofTimeout() {
	p.mu.Lock()
	defer p.mu.Unlock()

	p.ActiveChains = make(map[compose.ChainID]bool)
	p.SequenceNumber = 0
	p.TargetSuperblockNumber = p.LastFinalizedSuperblockNumber + 1
	p.messenger.BroadcastRollback(p.LastFinalizedSuperblockNumber, p.LastFinalizedSuperblockHash)
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
