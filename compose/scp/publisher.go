package scp

import (
	"errors"
	"github.com/compose-network/specs/compose"
	"sync"
)

type PublisherNetwork interface {
	SendStartInstance(instance compose.Instance, xTRequest []compose.Transaction)
	SendDecided(decided bool)
}

type PublisherInstance struct {
	mu sync.Mutex

	// Dependencies
	network PublisherNetwork
	// SCP instance
	instance  compose.Instance
	xTRequest []compose.Transaction

	// Protocol state
	decisionState compose.DecisionState
	votes         map[compose.ChainID]bool
}

func NewPublisherInstance(
	instance compose.Instance,
	network PublisherNetwork,
	xTRequest []compose.Transaction) (*PublisherInstance, error) {

	// Build runner
	r := &PublisherInstance{
		mu:            sync.Mutex{},
		network:       network,
		instance:      instance,
		xTRequest:     xTRequest,
		decisionState: compose.DecisionStatePending,
		votes:         make(map[compose.ChainID]bool),
	}

	return r, nil
}

// Run performs the initial side-effect to start the instance with participants.
// Call this once after creation.
func (r *PublisherInstance) Run() {
	r.network.SendStartInstance(r.instance, r.xTRequest)
}

func (r *PublisherInstance) ProcessVote(sender compose.ChainID, vote bool) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.decisionState != compose.DecisionStatePending {
		// TODO log ignoring vote because already decided
		return nil
	}

	if _, exists := r.votes[sender]; exists {
		return errors.New("Received duplicate vote")
	}

	r.votes[sender] = vote

	// If any vote is false, decide false immediately
	if vote == false {
		r.decisionState = compose.DecisionStateRejected
		r.network.SendDecided(false)
		return nil
	}

	// Check if all votes are in
	if len(r.votes) == len(r.instance.Chains) {
		r.decisionState = compose.DecisionStateAccepted
		r.network.SendDecided(true)
		return nil
	}

	return nil
}

func (r *PublisherInstance) Timeout() error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.decisionState != compose.DecisionStatePending {
		// TODO log ignoring timeout because already decided
		return nil
	}
	r.decisionState = compose.DecisionStateRejected
	r.network.SendDecided(false)
	return nil
}
