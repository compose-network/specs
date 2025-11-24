package cdcp

import (
	"io"

	"github.com/compose-network/specs/compose"
	"github.com/compose-network/specs/compose/scp"
	"github.com/rs/zerolog"
)

func testLogger() zerolog.Logger {
	return zerolog.New(io.Discard)
}

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

func makeInstance(entries ...chainRequest) compose.Instance {
	return compose.Instance{
		ID:             compose.InstanceID{1},
		PeriodID:       compose.PeriodID(1),
		SequenceNumber: compose.SequenceNumber(1),
		XTRequest:      makeXTRequest(entries...),
	}
}

func makeXTRequest(entries ...chainRequest) compose.XTRequest {
	req := compose.XTRequest{
		Transactions: make([]compose.TransactionRequest, len(entries)),
	}
	for i, entry := range entries {
		copied := make([][]byte, len(entry.txs))
		for j, tx := range entry.txs {
			copied[j] = append([]byte(nil), tx...)
		}
		req.Transactions[i] = compose.TransactionRequest{
			ChainID:      entry.chain,
			Transactions: copied,
		}
	}
	return req
}

func cloneInstance(inst compose.Instance) compose.Instance {
	return compose.Instance{
		ID:             inst.ID,
		PeriodID:       inst.PeriodID,
		SequenceNumber: inst.SequenceNumber,
		XTRequest:      cloneXTRequest(inst.XTRequest),
	}
}

func cloneXTRequest(req compose.XTRequest) compose.XTRequest {
	out := compose.XTRequest{
		Transactions: make([]compose.TransactionRequest, len(req.Transactions)),
	}
	for i, tr := range req.Transactions {
		out.Transactions[i] = compose.TransactionRequest{
			ChainID:      tr.ChainID,
			Transactions: compose.CloneByteSlices(tr.Transactions),
		}
	}
	return out
}

type fakePublisherNetwork struct {
	startInstances []compose.Instance
	nativeDecided  []struct {
		ID     compose.InstanceID
		Result bool
	}
	decisions []struct {
		ID     compose.InstanceID
		Result bool
	}
}

func (f *fakePublisherNetwork) SendStartInstance(instance compose.Instance) {
	f.startInstances = append(f.startInstances, cloneInstance(instance))
}

func (f *fakePublisherNetwork) SendNativeDecided(instanceID compose.InstanceID, decided bool) {
	f.nativeDecided = append(f.nativeDecided, struct {
		ID     compose.InstanceID
		Result bool
	}{ID: instanceID, Result: decided})
}

func (f *fakePublisherNetwork) SendDecided(instanceID compose.InstanceID, decided bool) {
	f.decisions = append(f.decisions, struct {
		ID     compose.InstanceID
		Result bool
	}{ID: instanceID, Result: decided})
}

type fakeWSExecutionEngine struct {
	chainID   compose.ChainID
	responses []WSSimulationResponse
	requests  []WSSimulationRequest
}

func (f *fakeWSExecutionEngine) ChainID() compose.ChainID { return f.chainID }

func (f *fakeWSExecutionEngine) Simulate(req WSSimulationRequest) WSSimulationResponse {
	copied := WSSimulationRequest{
		SafeExecuteArguments: cloneSafeExecuteArgs(req.SafeExecuteArguments),
		Snapshot:             req.Snapshot,
	}
	f.requests = append(f.requests, copied)

	if len(f.responses) == 0 {
		return WSSimulationResponse{}
	}

	resp := f.responses[0]
	f.responses = f.responses[1:]
	return cloneSimulationResponse(resp)
}

type fakeWSNetwork struct {
	mailboxMessages []struct {
		to  compose.ChainID
		msg scp.MailboxMessage
	}
	decisions []bool
}

func (f *fakeWSNetwork) SendMailboxMessage(recipient compose.ChainID, msg scp.MailboxMessage) {
	f.mailboxMessages = append(f.mailboxMessages, struct {
		to  compose.ChainID
		msg scp.MailboxMessage
	}{to: recipient, msg: cloneMailboxMessage(msg)})
}

func (f *fakeWSNetwork) SendWSDecidedMessage(decided bool) {
	f.decisions = append(f.decisions, decided)
}

type fakeERClient struct {
	err   error
	calls []SafeExecuteArguments
}

func (f *fakeERClient) SubmitTransaction(args SafeExecuteArguments) error {
	f.calls = append(f.calls, cloneSafeExecuteArgs(args))
	if f.err != nil {
		return f.err
	}
	return nil
}

func cloneSafeExecuteArgs(args SafeExecuteArguments) SafeExecuteArguments {
	return SafeExecuteArguments{
		PutInboxMessages:  cloneMailboxMessages(args.PutInboxMessages),
		PutOutboxMessages: cloneMailboxMessages(args.PutOutboxMessages),
		Transactions:      compose.CloneByteSlices(args.Transactions),
	}
}

func cloneSimulationResponse(resp WSSimulationResponse) WSSimulationResponse {
	var read *scp.MailboxMessageHeader
	if resp.ReadMiss != nil {
		copied := *resp.ReadMiss
		read = &copied
	}
	var write *scp.MailboxMessage
	if resp.WriteMiss != nil {
		copied := cloneMailboxMessage(*resp.WriteMiss)
		write = &copied
	}
	return WSSimulationResponse{
		ReadMiss:        read,
		WriteMiss:       write,
		WrittenMessages: cloneMailboxMessages(resp.WrittenMessages),
		Err:             resp.Err,
	}
}

func cloneMailboxMessages(msgs []scp.MailboxMessage) []scp.MailboxMessage {
	out := make([]scp.MailboxMessage, len(msgs))
	for i, msg := range msgs {
		out[i] = cloneMailboxMessage(msg)
	}
	return out
}

func cloneMailboxMessage(msg scp.MailboxMessage) scp.MailboxMessage {
	return scp.MailboxMessage{
		MailboxMessageHeader: msg.MailboxMessageHeader,
		Data:                 append([]byte(nil), msg.Data...),
	}
}
