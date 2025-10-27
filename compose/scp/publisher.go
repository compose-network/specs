package scp

import (
	"errors"
	"sync"

	"github.com/compose-network/specs/compose"

	"github.com/rs/zerolog"
)

var (
	ErrDuplicatedVote = errors.New("duplicated vote")
)

type PublisherNetwork interface {
	SendStartInstance(instance compose.Instance)
	SendDecided(instanceID compose.InstanceID, decided bool)
}

type PublisherInstance struct {
	mu sync.Mutex

	// Dependencies
	network PublisherNetwork
	// SCP instance
	instance compose.Instance
	chains   []compose.ChainID

	// Protocol state
	decisionState compose.DecisionState
	votes         map[compose.ChainID]bool

	logger zerolog.Logger
}

func NewPublisherInstance(
	instance compose.Instance,
	network PublisherNetwork,
	logger zerolog.Logger,
) (*PublisherInstance, error) {

	// Build runner
	r := &PublisherInstance{
		mu:            sync.Mutex{},
		network:       network,
		instance:      instance,
		chains:        instance.Chains(),
		decisionState: compose.DecisionStatePending,
		votes:         make(map[compose.ChainID]bool),
		logger:        logger,
	}

	return r, nil
}

// Run performs the initial side-effect to start the instance with participants.
// Call this once after creation.
func (r *PublisherInstance) Run() {
	r.network.SendStartInstance(r.instance)
}

func (r *PublisherInstance) ProcessVote(sender compose.ChainID, vote bool) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.decisionState != compose.DecisionStatePending {
		r.logger.Info().
			Uint64("chain_id", uint64(sender)).
			Bool("vote", vote).
			Msg("Ignoring vote because already decided")
		return nil
	}

	if _, exists := r.votes[sender]; exists {
		r.logger.Info().
			Uint64("chain_id", uint64(sender)).
			Bool("vote", vote).
			Msg("Ignoring duplicated vote")
		return ErrDuplicatedVote
	}

	r.votes[sender] = vote

	// If any vote is false, decide false immediately
	if vote == false {
		r.logger.Info().
			Uint64("chain_id", uint64(sender)).
			Msg("Received reject vote, rejecting instance")
		r.decisionState = compose.DecisionStateRejected
		r.network.SendDecided(r.instance.ID, false)
		return nil
	}

	// Check if all votes are in
	if len(r.votes) == len(r.chains) {
		r.logger.Info().
			Msg("All votes received, accepting instance")
		r.decisionState = compose.DecisionStateAccepted
		r.network.SendDecided(r.instance.ID, true)
		return nil
	}

	return nil
}

func (r *PublisherInstance) Timeout() error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.decisionState != compose.DecisionStatePending {
		r.logger.Info().
			Msg("Ignoring timeout because already decided")
		return nil
	}

	r.logger.Info().
		Msg("Instance timed out, rejecting")
	r.decisionState = compose.DecisionStateRejected
	r.network.SendDecided(r.instance.ID, false)
	return nil
}
