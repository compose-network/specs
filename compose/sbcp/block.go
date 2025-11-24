package sbcp

import "github.com/compose-network/specs/compose"

type BlockNumber uint64

type PendingBlock struct {
	Number           BlockNumber
	PeriodID         compose.PeriodID
	SuperblockNumber compose.SuperblockNumber
}

type BlockHeader struct {
	Number    BlockNumber
	BlockHash compose.BlockHash
	StateRoot compose.StateRoot
}

// SealedBlockHeader represents a block that has been sealed and included in the superblock chain.
type SealedBlockHeader struct {
	BlockHeader      BlockHeader
	PeriodID         compose.PeriodID
	SuperblockNumber compose.SuperblockNumber
}

type SettledState struct {
	BlockHeader      BlockHeader
	SuperblockNumber compose.SuperblockNumber
	SuperblockHash   compose.SuperblockHash
}
