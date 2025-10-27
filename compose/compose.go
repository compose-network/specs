package compose

import (
	"time"
)

var (
	GenesisTime    = time.Date(2025, 10, 25, 0, 0, 0, 0, time.UTC)
	PeriodDuration = 10 * (32 * 12) * time.Second // 10 Ethereum epochs

	// Allowed proving time since boundary (default: two-thirds of period)
	ProofWindow = 24 * 7
)

type TxHash [32]byte
type SuperBlockHash [32]byte
type BlockHash [32]byte
type StateRoot [32]byte
type ChainID uint64
type SessionID uint64
type InstanceID [32]byte
type PeriodID uint64

func (p PeriodID) Time() time.Time {
	return GenesisTime.Add(time.Duration(p) * PeriodDuration)
}

type SequenceNumber uint64
type SuperblockNumber uint64

// Transaction represents a VM transaction payload.
type Transaction interface {
	ChainID() ChainID
	Bytes() []byte
}

type EthAddress [20]byte

type Instance struct {
	ID             InstanceID
	PeriodID       PeriodID
	SequenceNumber SequenceNumber
	Chains         []ChainID
	XTRequest      []Transaction
}

type DecisionState int

const (
	DecisionStatePending DecisionState = iota
	DecisionStateAccepted
	DecisionStateRejected
)
