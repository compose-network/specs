package compose

import (
	"encoding/hex"
	"time"
)

const (
	// Duration of a superblock period (10 Ethereum epochs).
	PeriodDuration = 10 * (32 * 12) * time.Second // 10 Ethereum epochs

	// Allowed window (in number of periods) to submit a valid ZK proof for a superblock.
	// After this window, the publisher should trigger a rollback.
	ProofWindow = 24 * 7
)

type (
	EthAddress       [20]byte
	TxHash           [32]byte
	SuperblockHash   [32]byte
	BlockHash        [32]byte
	StateRoot        [32]byte
	ChainID          uint64
	SessionID        uint64
	PeriodID         uint64
	SequenceNumber   uint64
	SuperblockNumber uint64
	InstanceID       [32]byte
)

func (id InstanceID) String() string {
	return hex.EncodeToString(id[:])
}

type TransactionRequest struct {
	ChainID      ChainID
	Transactions [][]byte
}

type XTRequest struct {
	Transactions []TransactionRequest
}

type Instance struct {
	ID             InstanceID
	PeriodID       PeriodID
	SequenceNumber SequenceNumber
	XTRequest      XTRequest
}

func (i *Instance) Chains() []ChainID {
	return ChainsFromRequest(i.XTRequest)
}

func ChainsFromRequest(xtRequest XTRequest) []ChainID {
	chainsMap := make(map[ChainID]bool)
	for _, r := range xtRequest.Transactions {
		chainsMap[r.ChainID] = true
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
