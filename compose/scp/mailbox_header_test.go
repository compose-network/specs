package scp

import (
	"testing"

	"github.com/compose-network/specs/compose"

	"github.com/stretchr/testify/assert"
)

func TestMailboxMessageHeader_Equal_and_IgnoresData(t *testing.T) {
	a := MailboxMessage{
		MailboxMessageHeader: MailboxMessageHeader{
			SourceChainID: 1,
			DestChainID:   2,
			Sender:        compose.EthAddress{1},
			Receiver:      compose.EthAddress{2},
			SessionID:     10,
			Label:         "L",
		},
		Data: []byte("payload-A"),
	}
	b := MailboxMessage{
		MailboxMessageHeader: a.MailboxMessageHeader,
		Data:                 []byte("payload-B"), // different data
	}
	// Verify Data fields are different but headers are equal
	assert.NotEqual(t, a.Data, b.Data, "Data should be different for this test")
	assert.True(
		t,
		a.MailboxMessageHeader.Equal(b.MailboxMessageHeader),
		"Data must be ignored for equality",
	)

	// Now flip each field and assert inequality
	b = a
	b.SourceChainID = 99
	assert.False(t, a.MailboxMessageHeader.Equal(b.MailboxMessageHeader))

	b = a
	b.DestChainID = 99
	assert.False(t, a.MailboxMessageHeader.Equal(b.MailboxMessageHeader))

	b = a
	b.Sender = compose.EthAddress{9}
	assert.False(t, a.MailboxMessageHeader.Equal(b.MailboxMessageHeader))

	b = a
	b.Receiver = compose.EthAddress{9}
	assert.False(t, a.MailboxMessageHeader.Equal(b.MailboxMessageHeader))

	b = a
	b.SessionID = 999
	assert.False(t, a.MailboxMessageHeader.Equal(b.MailboxMessageHeader))

	b = a
	b.Label = "other"
	assert.False(t, a.MailboxMessageHeader.Equal(b.MailboxMessageHeader))
}
