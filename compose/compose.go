package compose

import (
	"encoding/hex"
	"time"
)

const (
	// Duration of a superblock period (10 Ethereum epochs)
	PeriodDuration = 10 * (32 * 12) * time.Second // 10 Ethereum epochs

	// Allowed window (in number of periods) to submit a valid ZK proof for a superblock.
	// After this window, the publisher should trigger a rollback.
	ProofWindow = 24 * 7
)

type EthAddress [20]byte
type TxHash [32]byte
type SuperBlockHash [32]byte
type BlockHash [32]byte
type StateRoot [32]byte
type ChainID uint64
type SessionID uint64
type PeriodID uint64
type SequenceNumber uint64
type SuperblockNumber uint64
type InstanceID [32]byte

func (id InstanceID) String() string {
	return hex.EncodeToString(id[:])
}

// Transaction represents a VM transaction payload.
type Transaction interface {
	ChainID() ChainID
	Bytes() []byte
}

type Instance struct {
	ID             InstanceID
	PeriodID       PeriodID
	SequenceNumber SequenceNumber
	XTRequest      []Transaction
}

func (i *Instance) Chains() []ChainID {
	return ChainsFromRequest(i.XTRequest)
}

func ChainsFromRequest(req []Transaction) []ChainID {
	chainsMap := make(map[ChainID]bool)
	for _, r := range req {
		chainsMap[r.ChainID()] = true
	}
	chains := make([]ChainID, 0, len(chainsMap))
	for chainID := range chainsMap {
		chains = append(chains, chainID)
	}
	return chains
}

type DecisionState int

const (
	DecisionStatePending DecisionState = iota
	DecisionStateAccepted
	DecisionStateRejected
)

func (d *DecisionState) String() string {
	switch *d {
	case DecisionStatePending:
		return "Pending"
	case DecisionStateAccepted:
		return "Accepted"
	case DecisionStateRejected:
		return "Rejected"
	default:
		return "Unknown"
	}
}
