package scp

import (
	"errors"
	"sync"

	"github.com/compose-network/specs/compose"

	"github.com/rs/zerolog"
)

var (
	ErrDuplicatedVote       = errors.New("duplicated vote")
	ErrSenderNotParticipant = errors.New("sender is not a participant")
)

type PublisherInstance interface {
	Instance() compose.Instance
	DecisionState() compose.DecisionState
	Run()
	ProcessVote(sender compose.ChainID, vote bool) error
	Timeout() error
}

type PublisherNetwork interface {
	SendStartInstance(instance compose.Instance)
	SendDecided(instanceID compose.InstanceID, decided bool)
}

type publisherInstance struct {
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
) (PublisherInstance, error) {

	// Build runner
	r := &publisherInstance{
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

	if !r.chainInInstance(sender) {
		r.logger.Info().
			Uint64("chain_id", uint64(sender)).
			Bool("vote", vote).
			Msg("Ignoring vote from non-participant")
		return ErrSenderNotParticipant
	}

	r.votes[sender] = vote

	// If any vote is false, decide false immediately
	if !vote {
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

func (r *publisherInstance) Timeout() error {
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

func (r *publisherInstance) chainInInstance(chainID compose.ChainID) bool {
	for _, cid := range r.chains {
		if cid == chainID {
			return true
		}
	}
	return false
}
