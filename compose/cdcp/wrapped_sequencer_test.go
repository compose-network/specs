package cdcp

import (
	"errors"
	"testing"

	"github.com/compose-network/specs/compose"
	"github.com/compose-network/specs/compose/scp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func makeMailboxMsg(src, dst compose.ChainID, label string, data []byte) scp.MailboxMessage {
	return scp.MailboxMessage{
		MailboxMessageHeader: scp.MailboxMessageHeader{
			SourceChainID: src,
			DestChainID:   dst,
			SessionID:     compose.SessionID(1),
			Sender:        compose.EthAddress{1},
			Receiver:      compose.EthAddress{2},
			Label:         label,
		},
		Data: append([]byte(nil), data...),
	}
}

func TestWrappedSequencer_NewInstanceValidatesTransactions(t *testing.T) {
	instance := makeInstance(
		chainReq(1, []byte("a1")),
		chainReq(2, []byte("a2")),
	)
	exec := &fakeWSExecutionEngine{chainID: compose.ChainID(2)}
	net := &fakeWSNetwork{}
	er := &fakeERClient{}

	ws, err := NewWrappedSequencerInstance(instance, exec, net, er, compose.StateRoot{}, testLogger())
	require.NoError(t, err)
	require.NotNil(t, ws)

	_, err = NewWrappedSequencerInstance(
		makeInstance(chainReq(1, []byte("only"))),
		&fakeWSExecutionEngine{chainID: compose.ChainID(9)},
		net,
		er,
		compose.StateRoot{},
		testLogger(),
	)
	require.ErrorIs(t, err, ErrNoTransactions)
}

func TestWrappedSequencer_RunSuccessSendsMailboxWrites(t *testing.T) {
	msg := makeMailboxMsg(10, 11, "hello", []byte("payload"))
	exec := &fakeWSExecutionEngine{
		chainID: compose.ChainID(11),
		responses: []WSSimulationResponse{
			{WrittenMessages: []scp.MailboxMessage{msg}},
		},
	}
	net := &fakeWSNetwork{}
	ws, err := NewWrappedSequencerInstance(
		makeInstance(
			chainReq(10, []byte("other")),
			chainReq(11, []byte("tx1")),
		),
		exec,
		net,
		&fakeERClient{},
		compose.StateRoot{},
		testLogger(),
	)
	require.NoError(t, err)

	require.NoError(t, ws.Run())
	require.Len(t, exec.requests, 1)
	require.Len(t, net.mailboxMessages, 1)
	assert.True(t, net.mailboxMessages[0].msg.Equal(msg))
	assert.Equal(t, compose.DecisionStatePending, ws.DecisionState())
	assert.Empty(t, net.decisions)
}

func TestWrappedSequencer_RunSimulationErrorRejects(t *testing.T) {
	exec := &fakeWSExecutionEngine{
		chainID: compose.ChainID(11),
		responses: []WSSimulationResponse{
			{Err: errors.New("boom")},
		},
	}
	net := &fakeWSNetwork{}
	ws, err := NewWrappedSequencerInstance(
		makeInstance(
			chainReq(11, []byte("tx1")),
		),
		exec,
		net,
		&fakeERClient{},
		compose.StateRoot{},
		testLogger(),
	)
	require.NoError(t, err)

	runErr := ws.Run()
	require.EqualError(t, runErr, "simulating sequencer failed: boom")
	require.Len(t, net.decisions, 1)
	assert.False(t, net.decisions[0])
	assert.Equal(t, compose.DecisionStateRejected, ws.DecisionState())
}

func TestWrappedSequencer_ReadMissThenMailboxDelivery(t *testing.T) {
	header := scp.MailboxMessageHeader{
		SourceChainID: compose.ChainID(5),
		DestChainID:   compose.ChainID(7),
		SessionID:     compose.SessionID(9),
		Sender:        compose.EthAddress{1},
		Receiver:      compose.EthAddress{2},
		Label:         "need-msg",
	}
	exec := &fakeWSExecutionEngine{
		chainID: compose.ChainID(7),
		responses: []WSSimulationResponse{
			{ReadMiss: &header},
			{}, // success once mailbox fulfilled
		},
	}
	net := &fakeWSNetwork{}
	ws, err := NewWrappedSequencerInstance(
		makeInstance(chainReq(7, []byte("tx1"))),
		exec,
		net,
		&fakeERClient{},
		compose.StateRoot{},
		testLogger(),
	)
	require.NoError(t, err)

	require.NoError(t, ws.Run())
	require.Len(t, exec.requests, 1)

	msg := scp.MailboxMessage{
		MailboxMessageHeader: header,
		Data:                 []byte("payload"),
	}
	require.NoError(t, ws.ProcessMailboxMessage(msg))
	require.Len(t, exec.requests, 2)
}

func TestWrappedSequencer_WriteMissResimulatesWithPrepopulation(t *testing.T) {
	writeMiss := makeMailboxMsg(7, 9, "write", []byte("payload"))
	exec := &fakeWSExecutionEngine{
		chainID: compose.ChainID(9),
		responses: []WSSimulationResponse{
			{WriteMiss: &writeMiss},
			{}, // success after prepopulation
		},
	}
	net := &fakeWSNetwork{}
	ws, err := NewWrappedSequencerInstance(
		makeInstance(chainReq(9, []byte("tx1"))),
		exec,
		net,
		&fakeERClient{},
		compose.StateRoot{},
		testLogger(),
	)
	require.NoError(t, err)

	require.NoError(t, ws.Run())
	require.Len(t, exec.requests, 2)
	require.Len(t, exec.requests[1].PutOutboxMessages, 1)
	assert.True(t, exec.requests[1].PutOutboxMessages[0].Equal(writeMiss))
}

func TestWrappedSequencer_ProcessNativeDecidedTrueTriggersERFlow(t *testing.T) {
	exec := &fakeWSExecutionEngine{
		chainID: compose.ChainID(9),
	}
	net := &fakeWSNetwork{}
	er := &fakeERClient{}
	instance := makeInstance(
		chainReq(8, []byte("other")),
		chainReq(9, []byte("tx1"), []byte("tx2")),
	)
	ws, err := NewWrappedSequencerInstance(instance, exec, net, er, compose.StateRoot{}, testLogger())
	require.NoError(t, err)

	require.NoError(t, ws.Run())
	require.NoError(t, ws.ProcessNativeDecidedMessage(true))

	require.Len(t, er.calls, 1)
	call := er.calls[0]
	require.Len(t, call.Transactions, 2)
	assert.Equal(t, [][]byte{[]byte("tx1"), []byte("tx2")}, call.Transactions)
	require.Len(t, net.decisions, 1)
	assert.True(t, net.decisions[0])
	assert.Equal(t, compose.DecisionStateAccepted, ws.DecisionState())
}

func TestWrappedSequencer_ProcessNativeDecidedFalseCancels(t *testing.T) {
	exec := &fakeWSExecutionEngine{
		chainID: compose.ChainID(5),
	}
	net := &fakeWSNetwork{}
	er := &fakeERClient{}
	ws, err := NewWrappedSequencerInstance(
		makeInstance(chainReq(5, []byte("tx1"))),
		exec,
		net,
		er,
		compose.StateRoot{},
		testLogger(),
	)
	require.NoError(t, err)

	require.NoError(t, ws.Run())
	require.NoError(t, ws.ProcessNativeDecidedMessage(false))

	assert.Equal(t, compose.DecisionStateRejected, ws.DecisionState())
	assert.Len(t, er.calls, 0)
	assert.Len(t, net.decisions, 0)
}

func TestWrappedSequencer_DuplicateNativeDecidedErrors(t *testing.T) {
	exec := &fakeWSExecutionEngine{
		chainID: compose.ChainID(8),
	}
	net := &fakeWSNetwork{}
	ws, err := NewWrappedSequencerInstance(
		makeInstance(chainReq(8, []byte("tx1"))),
		exec,
		net,
		&fakeERClient{},
		compose.StateRoot{},
		testLogger(),
	)
	require.NoError(t, err)

	// Native decided arrives before simulation completes.
	require.NoError(t, ws.ProcessNativeDecidedMessage(true))
	err = ws.ProcessNativeDecidedMessage(false)
	require.ErrorIs(t, err, ErrDuplicateWSDecided)
}

func TestWrappedSequencer_ERFailureSendsFalseDecision(t *testing.T) {
	exec := &fakeWSExecutionEngine{
		chainID: compose.ChainID(6),
	}
	net := &fakeWSNetwork{}
	er := &fakeERClient{err: errors.New("submit failed")}
	ws, err := NewWrappedSequencerInstance(
		makeInstance(chainReq(6, []byte("tx1"))),
		exec,
		net,
		er,
		compose.StateRoot{},
		testLogger(),
	)
	require.NoError(t, err)

	require.NoError(t, ws.Run())
	require.NoError(t, ws.ProcessNativeDecidedMessage(true))

	require.Len(t, er.calls, 1)
	require.Len(t, net.decisions, 1)
	assert.False(t, net.decisions[0])
	assert.Equal(t, compose.DecisionStateRejected, ws.DecisionState())
}

func TestWrappedSequencer_ProcessMailboxMessageIgnoredWhenNotSimulating(t *testing.T) {
	exec := &fakeWSExecutionEngine{
		chainID: compose.ChainID(4),
	}
	net := &fakeWSNetwork{}
	ws, err := NewWrappedSequencerInstance(
		makeInstance(chainReq(4, []byte("tx1"))),
		exec,
		net,
		&fakeERClient{},
		compose.StateRoot{},
		testLogger(),
	)
	require.NoError(t, err)

	require.NoError(t, ws.Run())
	require.Equal(t, 1, len(exec.requests))

	msg := makeMailboxMsg(1, 4, "late", []byte("payload"))
	require.NoError(t, ws.ProcessMailboxMessage(msg))
	assert.Equal(t, 1, len(exec.requests))
}

func TestWrappedSequencer_TimeoutBehaviour(t *testing.T) {
	exec := &fakeWSExecutionEngine{
		chainID: compose.ChainID(12),
	}
	net := &fakeWSNetwork{}
	ws, err := NewWrappedSequencerInstance(
		makeInstance(chainReq(12, []byte("tx1"))),
		exec,
		net,
		&fakeERClient{},
		compose.StateRoot{},
		testLogger(),
	)
	require.NoError(t, err)

	require.NoError(t, ws.Run())
	ws.Timeout()
	require.Len(t, net.decisions, 1)
	assert.False(t, net.decisions[0])
	assert.Equal(t, compose.DecisionStateRejected, ws.DecisionState())

	// Timeout again should be ignored now that instance is done.
	ws.Timeout()
	assert.Len(t, net.decisions, 1)
}
