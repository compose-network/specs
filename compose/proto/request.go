package proto

func (x *XTRequest) TransactionsByChain() map[uint64][][]byte {
	if x == nil {
		return nil
	}
	m := make(map[uint64][][]byte, len(x.GetTransactionRequests()))
	for _, req := range x.GetTransactionRequests() {
		m[req.GetChainId()] = req.GetTransaction()
	}
	return m
}
