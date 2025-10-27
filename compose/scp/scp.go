package scp

import "compose"

// MailboxMessage carries the data exchanged between sequencers for mailbox fulfillment.
type MailboxMessage struct {
	MailboxMessageHeader
	Data []byte
}

type MailboxMessageHeader struct {
	SourceChainID compose.ChainID
	DestChainID   compose.ChainID
	Sender        compose.EthAddress
	Receiver      compose.EthAddress
	SessionID     compose.SessionID
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
