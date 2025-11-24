package sbcp

import "github.com/compose-network/specs/compose"

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

// fakePublisherMessenger records broadcasts for assertions.
type fakePublisherMessenger struct {
	startPeriods []struct {
		PeriodID         compose.PeriodID
		SuperblockNumber compose.SuperblockNumber
	}
	startInstances []compose.Instance
	rollbacks      []struct {
		PeriodID         compose.PeriodID
		SuperblockNumber compose.SuperblockNumber
		SuperblockHash   compose.SuperblockHash
	}
}

func (m *fakePublisherMessenger) BroadcastStartPeriod(p compose.PeriodID, t compose.SuperblockNumber) {
	m.startPeriods = append(m.startPeriods, struct {
		PeriodID         compose.PeriodID
		SuperblockNumber compose.SuperblockNumber
	}{p, t})
}

func (m *fakePublisherMessenger) BroadcastStartInstance(inst compose.Instance) {
	m.startInstances = append(m.startInstances, inst)
}

func (m *fakePublisherMessenger) BroadcastRollback(p compose.PeriodID, s compose.SuperblockNumber, h compose.SuperblockHash) {
	m.rollbacks = append(m.rollbacks, struct {
		PeriodID         compose.PeriodID
		SuperblockNumber compose.SuperblockNumber
		SuperblockHash   compose.SuperblockHash
	}{p, s, h})
}

type fakePublisherProver struct {
	calls []struct {
		superblock compose.SuperblockNumber
		hash       compose.SuperblockHash
		proofs     [][]byte
	}
	nextProof []byte
	err       error
}

func (p *fakePublisherProver) RequestNetworkProof(superblockNumber compose.SuperblockNumber, hash compose.SuperblockHash, proofs [][]byte) ([]byte, error) {
	copied := make([][]byte, len(proofs))
	for i, proof := range proofs {
		copied[i] = append([]byte(nil), proof...)
	}
	p.calls = append(p.calls, struct {
		superblock compose.SuperblockNumber
		hash       compose.SuperblockHash
		proofs     [][]byte
	}{superblockNumber, hash, copied})
	if p.err != nil {
		return nil, p.err
	}
	return append([]byte(nil), p.nextProof...), nil
}

type fakeL1 struct {
	published []struct {
		superblock compose.SuperblockNumber
		proof      []byte
	}
}

func (l *fakeL1) PublishProof(superblockNumber compose.SuperblockNumber, proof []byte) {
	l.published = append(l.published, struct {
		superblock compose.SuperblockNumber
		proof      []byte
	}{superblockNumber, append([]byte(nil), proof...)})
}

// fakeSequencerProver records settlement requests for assertions.
type fakeSequencerProver struct {
	calls []struct {
		hdr *BlockHeader
		sb  compose.SuperblockNumber
	}
	nextProof []byte
}

func (p *fakeSequencerProver) RequestProofs(hdr *BlockHeader, sb compose.SuperblockNumber) []byte {
	p.calls = append(p.calls, struct {
		hdr *BlockHeader
		sb  compose.SuperblockNumber
	}{hdr, sb})
	return append([]byte(nil), p.nextProof...)
}

type fakeSequencerMessenger struct {
	requests []compose.XTRequest
	proofs   []struct {
		periodID         compose.PeriodID
		superblockNumber compose.SuperblockNumber
		proof            []byte
	}
}

func (m *fakeSequencerMessenger) ForwardRequest(request compose.XTRequest) {
	m.requests = append(m.requests, request)
}

func (m *fakeSequencerMessenger) SendProof(periodID compose.PeriodID, superblockNumber compose.SuperblockNumber, proof []byte) {
	m.proofs = append(m.proofs, struct {
		periodID         compose.PeriodID
		superblockNumber compose.SuperblockNumber
		proof            []byte
	}{periodID, superblockNumber, append([]byte(nil), proof...)})
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
		SuperblockHash:   compose.SuperblockHash{1, 2, 3},
	}
}

func makeChainSet(ids ...compose.ChainID) map[compose.ChainID]struct{} {
	m := make(map[compose.ChainID]struct{}, len(ids))
	for _, id := range ids {
		m[id] = struct{}{}
	}
	return m
}

func makeDefaultChainSet() map[compose.ChainID]struct{} {
	return makeChainSet(
		compose.ChainID(1),
		compose.ChainID(2),
		compose.ChainID(3),
		compose.ChainID(4),
		compose.ChainID(5),
		compose.ChainID(6),
		compose.ChainID(7),
		compose.ChainID(8),
		compose.ChainID(9),
		compose.ChainID(10),
	)
}
