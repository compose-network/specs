package sbcp

import (
	"github.com/compose-network/specs/compose"
)

type chainRequest struct {
	chain compose.ChainID
	txs   [][]byte
}

func chainReq(chain compose.ChainID, txs ...[]byte) chainRequest {
	copied := make([][]byte, len(txs))
	for i, tx := range txs {
		copied[i] = append([]byte(nil), tx...)
	}
	return chainRequest{chain: chain, txs: copied}
}

func makeXTRequest(entries ...chainRequest) compose.XTRequest {
	req := compose.XTRequest{
		Transactions: make([]compose.TransactionRequest, len(entries)),
	}
	for i, entry := range entries {
		txs := make([][]byte, len(entry.txs))
		for j, tx := range entry.txs {
			txs[j] = append([]byte(nil), tx...)
		}
		req.Transactions[i] = compose.TransactionRequest{
			ChainID:      entry.chain,
			Transactions: txs,
		}
	}
	return req
}

// fakeMessenger records broadcasts for assertions.
type fakeMessenger struct {
	startPeriods []struct {
		P compose.PeriodID
		T compose.SuperblockNumber
	}
	startInstances []compose.Instance
	rollbacks      []struct {
		P compose.PeriodID
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

func (m *fakeMessenger) BroadcastRollback(p compose.PeriodID, s compose.SuperblockNumber, h compose.SuperBlockHash) {
	m.rollbacks = append(m.rollbacks, struct {
		P compose.PeriodID
		S compose.SuperblockNumber
		H compose.SuperBlockHash
	}{p, s, h})
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
