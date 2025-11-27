package cdcp

import (
	"errors"
	"sync"

	"github.com/compose-network/specs/compose"

	"github.com/rs/zerolog"
)

var (
	ErrERNotFound               = errors.New("ER chain not found in instance")
	ErrNotEnoughChains          = errors.New("not enough chains in instance")
	ErrDuplicatedVote           = errors.New("duplicated vote")
	ErrVoteSenderNotNativeChain = errors.New("vote sender is not a native chain")
	ErrNotERChain               = errors.New("not ER chain")
	ErrDuplicatedWSDecided      = errors.New("duplicated WSDecided")
	ErrInvalidStateForWSDecided = errors.New("invalid protocol state: WSDecided true received while waiting for votes")
)

// PublisherInstance represents the publisher logic for the CDCP protocol
type PublisherInstance interface {
	Instance() compose.Instance
	DecisionState() compose.DecisionState
	Run()
	ProcessVote(sender compose.ChainID, vote bool) error
	ProcessWSDecided(sender compose.ChainID, decision bool) error
	Timeout() error
}

type PublisherNetwork interface {
	SendStartInstance(instance compose.Instance)
	SendNativeDecided(instanceID compose.InstanceID, decided bool)
	SendDecided(instanceID compose.InstanceID, decided bool)
}

// PublisherState tracks the state machine for a publisher in a CDCP session.
type PublisherState int

const (
	PublisherStateWaitingVotes PublisherState = iota
	PublisherStateWaitingWSDecided
	PublisherStateDone
)

type publisherInstance struct {
	mu sync.Mutex

	// Dependencies
	network PublisherNetwork

	// SCP instance
	instance     compose.Instance
	nativeChains map[compose.ChainID]struct{}
	erChainID    compose.ChainID

	// Protocol state
	state         PublisherState
	decisionState compose.DecisionState
	votes         map[compose.ChainID]bool
	wsDecision    *bool

	logger zerolog.Logger
}

func NewPublisherInstance(
	instance compose.Instance,
	network PublisherNetwork,
	erChainID compose.ChainID,
	logger zerolog.Logger,
) (PublisherInstance, error) {

	nativeChains, err := validateChains(instance, erChainID)
	if err != nil {
		return nil, err
	}

	// Build runner
	r := &publisherInstance{
		mu:            sync.Mutex{},
		network:       network,
		instance:      instance,
		nativeChains:  nativeChains,
		erChainID:     erChainID,
		state:         PublisherStateWaitingVotes, // initial state
		decisionState: compose.DecisionStatePending,
		votes:         make(map[compose.ChainID]bool),
		wsDecision:    nil,
		logger:        logger,
	}

	return r, nil
}

// validateChains validates the instance and returns the set of native chains.
func validateChains(instance compose.Instance, erChainID compose.ChainID) (nativeChains map[compose.ChainID]struct{}, err error) {

	// Ensure there are at least 2 chains
	instanceChains := instance.Chains()
	if len(instanceChains) < 2 {
		return nil, ErrNotEnoughChains
	}

	// build native_chains set
	foundER := false
	nativeChains = make(map[compose.ChainID]struct{})
	for _, chainID := range instanceChains {
		if chainID == erChainID {
			foundER = true
			continue
		}
		nativeChains[chainID] = struct{}{}
	}

	// Ensure the ER chain belongs to instance
	if !foundER {
		return nil, ErrERNotFound
	}

	return nativeChains, nil
}

func (r *publisherInstance) DecisionState() compose.DecisionState {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.decisionState
}

func (r *publisherInstance) Instance() compose.Instance {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.instance
}

// Run performs launches the instance by sending a message to all participants.
// Call this once after creation.
func (r *publisherInstance) Run() {
	r.network.SendStartInstance(r.instance)
}

func (r *publisherInstance) ProcessVote(sender compose.ChainID, vote bool) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.state != PublisherStateWaitingVotes {
		r.logger.Info().
			Uint64("chain_id", uint64(sender)).
			Bool("vote", vote).
			Msg("Ignoring vote because not waiting anymore")
		return nil
	}

	// Ensure no duplicates
	if _, exists := r.votes[sender]; exists {
		r.logger.Info().
			Uint64("chain_id", uint64(sender)).
			Bool("vote", vote).
			Msg("Ignoring duplicated vote")
		return ErrDuplicatedVote
	}

	// Ensure it's a native chain
	if _, ok := r.nativeChains[sender]; !ok {
		r.logger.Info().
			Uint64("chain_id", uint64(sender)).
			Bool("vote", vote).
			Msg("Ignoring vote from non-native chain")
		return ErrVoteSenderNotNativeChain
	}

	r.votes[sender] = vote

	// If any vote is false, decide false immediately
	if !vote {
		r.logger.Info().
			Uint64("chain_id", uint64(sender)).
			Msg("Received reject vote, rejecting instance")
		r.decisionState = compose.DecisionStateRejected
		r.state = PublisherStateDone
		r.network.SendDecided(r.instance.ID, false)
		r.network.SendNativeDecided(r.instance.ID, false)
		return nil
	}

	// Check if all votes are in
	if len(r.votes) == len(r.nativeChains) {
		r.logger.Info().
			Msg("All votes(true) received, sending native decided as true")
		r.state = PublisherStateWaitingWSDecided
		r.network.SendNativeDecided(r.instance.ID, true)
		return nil
	}

	return nil
}

// ProcessWSDecided processes a WSDecided.
// If decision is false, it can already terminate the instance even it has not yet received all native votes.
func (r *publisherInstance) ProcessWSDecided(sender compose.ChainID, decision bool) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.wsDecision != nil {
		r.logger.Error().
			Uint64("chain_id", uint64(sender)).
			Bool("ws_decided", decision).
			Bool("previous_ws_decided", *r.wsDecision).
			Msg("Duplicated WSDecided")
		return ErrDuplicatedWSDecided
	}

	if r.state == PublisherStateDone {
		r.logger.Info().
			Uint64("chain_id", uint64(sender)).
			Bool("ws_decided", decision).
			Msg("Ignoring WSDecided because already done")
		return nil
	}

	// It can't receive decision == true and still be waiting for votes
	// as the protocol specifies that WSDecided(true) can only be sent
	// after the WS receives the NativeDecided
	if r.state == PublisherStateWaitingVotes && decision {
		r.logger.Error().
			Uint64("chain_id", uint64(sender)).
			Bool("ws_decided", decision).
			Msg("WSDecided true received, but still waiting for votes. Impossible protocol state.")
		return ErrInvalidStateForWSDecided
	}

	// Ensure it's ER chain
	if sender != r.erChainID {
		r.logger.Warn().
			Uint64("chain_id", uint64(sender)).
			Bool("ws_decided", decision).
			Msg("WSDecided received from non ER chain")
		return ErrNotERChain
	}

	// Valid message -> store decision
	r.wsDecision = &decision

	// If decision is true, decide true
	if decision {
		r.logger.Info().
			Uint64("chain_id", uint64(sender)).
			Bool("ws_decided", decision).
			Msg("Received successful WSDecided, accepting and terminating instance")
		r.state = PublisherStateDone
		r.decisionState = compose.DecisionStateAccepted
		r.network.SendDecided(r.instance.ID, true)
		return nil
	}

	// Else, reject instance
	r.logger.Info().
		Uint64("chain_id", uint64(sender)).
		Bool("ws_decided", decision).
		Msg("Received failed WSDecided, rejecting and terminating instance")

	r.state = PublisherStateDone
	r.decisionState = compose.DecisionStateRejected
	r.network.SendDecided(r.instance.ID, false)
	return nil
}

func (r *publisherInstance) Timeout() error {
	r.mu.Lock()
	defer r.mu.Unlock()

	// If state is waiting for WS or Done, it can't time out
	if r.state != PublisherStateWaitingVotes {
		r.logger.Info().
			Msg("Ignoring timeout because not waiting for native votes anymore")
		return nil
	}

	// Reject instance
	r.logger.Info().
		Msg("Instance timed out, rejecting")
	r.decisionState = compose.DecisionStateRejected
	r.state = PublisherStateDone
	r.network.SendDecided(r.instance.ID, false)
	r.network.SendNativeDecided(r.instance.ID, false)
	return nil
}
