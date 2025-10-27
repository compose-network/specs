package sbcp

import (
	"compose"
	"errors"
	"fmt"
	"sync"
)

var (
	ErrCannotStartInstance = errors.New("can not start any instance")
	ErrInstanceNotFound    = errors.New("instance not found")
	ErrCannotStartPeriod   = errors.New("can not start period")
	ErrChainNotActive      = errors.New("chain not active")
	ErrOldSettledState     = errors.New("can not advance to older settled state")
	ErrInvalidRequest      = errors.New("invalid request")
)

type Publisher interface {
	// StartPeriod is called whenever a new period starts (i.e. CurrEthereumEpoch % 10 == 0).
	StartPeriod(newPeriodID compose.PeriodID) error
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
	BroadcastStartInstance(instance compose.Instance)
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

	ERChainID compose.ChainID // External Rollup ChainID
}

type publisher struct {
	mu        sync.Mutex
	messenger Messenger
	PublisherState
}

func NewPublisher(messenger Messenger,
	periodID compose.PeriodID,
	targetSuperblockNumber compose.SuperblockNumber,
	lastFinalizedSuperblockNumber compose.SuperblockNumber,
	lastFinalizedSuperblockHash compose.SuperBlockHash,
	erChainID compose.ChainID,
) Publisher {
	return &publisher{
		mu:        sync.Mutex{},
		messenger: messenger,
		PublisherState: PublisherState{
			PeriodID:               periodID,
			TargetSuperblockNumber: targetSuperblockNumber,

			// Last finalized state in L1
			LastFinalizedSuperblockNumber: lastFinalizedSuperblockNumber,
			LastFinalizedSuperblockHash:   lastFinalizedSuperblockHash,

			// Instances scheduling
			SequenceNumber: 0,
			ActiveChains:   make(map[compose.ChainID]bool),

			ERChainID: erChainID,
		},
	}
}

// StartPeriod is called whenever a new period starts (i.e. CurrEthereumEpoch % 10 == 0).
func (p *publisher) StartPeriod(newPeriodID compose.PeriodID) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.TargetSuperblockNumber != p.LastFinalizedSuperblockNumber+1 {
		return fmt.Errorf("target superblock is %d, expected %d: %w",
			p.TargetSuperblockNumber, p.LastFinalizedSuperblockNumber+1, ErrCannotStartPeriod)
	}

	p.PeriodID = newPeriodID
	p.TargetSuperblockNumber++

	p.messenger.BroadcastStartPeriod(p.PeriodID, p.TargetSuperblockNumber)

	p.SequenceNumber = 0
	return nil
}

// StartInstance loops through the queued XT requests and tries one instance if possible.
func (p *publisher) StartInstance(request []compose.Transaction) (compose.Instance, error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	// Requests must have at least 2 transactions
	if len(request) < 2 {
		return compose.Instance{}, ErrInvalidRequest
	}

	chains := getChainsFromRequest(request)
	// Can't start instance if any participant is already active
	if p.anyChainAlreadyActive(chains) {
		return compose.Instance{}, ErrCannotStartInstance
	}

	// Create instance
	p.SequenceNumber++
	instance := compose.Instance{
		// TODO: generate unique ID
		ID: generateInstanceID(
			p.PeriodID,
			p.SequenceNumber,
			request,
		),
		PeriodID:       p.PeriodID,
		SequenceNumber: p.SequenceNumber,
		Chains:         chains,
		XTRequest:      request,
	}
	// Set chains as active
	for _, chainID := range chains {
		p.ActiveChains[chainID] = true
	}
	// Broadcast
	p.messenger.BroadcastStartInstance(instance)
	return instance, nil
}

// DecideInstance removes the instance from being active.
func (p *publisher) DecideInstance(instance compose.Instance) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	for _, chainID := range instance.Chains {
		if _, ok := p.ActiveChains[chainID]; !ok {
			return ErrChainNotActive
		}
	}

	for _, chainID := range instance.Chains {
		delete(p.ActiveChains, chainID)
	}
	return nil
}

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

func (p *publisher) ProofTimeout() {
	p.mu.Lock()
	defer p.mu.Unlock()

	p.ActiveChains = make(map[compose.ChainID]bool)
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

func getChainsFromRequest(req []compose.Transaction) []compose.ChainID {
	chainsMap := make(map[compose.ChainID]bool)
	for _, r := range req {
		chainsMap[r.ChainID()] = true
	}
	chains := make([]compose.ChainID, 0, len(chainsMap))
	for chainID := range chainsMap {
		chains = append(chains, chainID)
	}
	return chains
}
