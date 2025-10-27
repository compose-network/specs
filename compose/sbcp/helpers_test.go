package sbcp

import (
	"compose"
)

// fakeChainTx is a minimal compose.Transaction for SBCP tests.
type fakeChainTx struct {
	chain compose.ChainID
	body  []byte
}

func (t fakeChainTx) ChainID() compose.ChainID { return t.chain }
func (t fakeChainTx) Bytes() []byte            { return append([]byte(nil), t.body...) }

// fakeMessenger records broadcasts for assertions.
type fakeMessenger struct {
	startPeriods []struct {
		P compose.PeriodID
		T compose.SuperblockNumber
	}
	startInstances []compose.Instance
	rollbacks      []struct {
		S compose.SuperblockNumber
		H compose.SuperBlockHash
	}
}

func (m *fakeMessenger) BroadcastStartPeriod(p compose.PeriodID, t compose.SuperblockNumber) {
	m.startPeriods = append(m.startPeriods, struct {
		P compose.PeriodID
		T compose.SuperblockNumber
	}{p, t})
}

func (m *fakeMessenger) BroadcastStartInstance(inst compose.Instance) {
	m.startInstances = append(m.startInstances, inst)
}

func (m *fakeMessenger) BroadcastRollback(s compose.SuperblockNumber, h compose.SuperBlockHash) {
	m.rollbacks = append(m.rollbacks, struct {
		S compose.SuperblockNumber
		H compose.SuperBlockHash
	}{s, h})
}

// fakeProver records settlement requests for assertions.
type fakeProver struct {
	calls []struct {
		hdr *BlockHeader
		sb  compose.SuperblockNumber
	}
}

func (p *fakeProver) RequestProofs(hdr *BlockHeader, sb compose.SuperblockNumber) {
	p.calls = append(p.calls, struct {
		hdr *BlockHeader
		sb  compose.SuperblockNumber
	}{hdr, sb})
}

// mkHeader creates a minimal BlockHeader for tests.
func mkHeader(n BlockNumber) BlockHeader {
	return BlockHeader{Number: n}
}

// mkSettled constructs a SettledState with the given superblock number and head block number.
func mkSettled(sb compose.SuperblockNumber, head BlockNumber) SettledState {
	return SettledState{
		BlockHeader:      mkHeader(head),
		SuperblockNumber: sb,
		SuperblockHash:   compose.SuperBlockHash{1, 2, 3},
	}
}
