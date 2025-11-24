package scp

import (
	"bytes"

	"github.com/compose-network/specs/compose"
)

// MailboxMessage carries the data exchanged between sequencers for mailbox fulfillment.
type MailboxMessage struct {
	MailboxMessageHeader
	Data []byte
}

func (a MailboxMessage) Equal(b MailboxMessage) bool {
	if !a.MailboxMessageHeader.Equal(b.MailboxMessageHeader) {
		return false
	}
	return bytes.Equal(a.Data, b.Data)
}

type MailboxMessageHeader struct {
	SessionID     compose.SessionID
	SourceChainID compose.ChainID
	DestChainID   compose.ChainID
	Sender        compose.EthAddress
	Receiver      compose.EthAddress
	Label         string
}

func (a MailboxMessageHeader) Equal(b MailboxMessageHeader) bool {
	return a.SourceChainID == b.SourceChainID &&
		a.DestChainID == b.DestChainID &&
		a.Sender == b.Sender &&
		a.Receiver == b.Receiver &&
		a.SessionID == b.SessionID &&
		a.Label == b.Label
}
