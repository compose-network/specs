package proto

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestInstanceIDHex(t *testing.T) {
	id := []byte{0xde, 0xad, 0xbe, 0xef}

	si := &StartInstance{InstanceId: id}
	assert.Equal(t, "deadbeef", si.InstanceIDHex())

	v := &Vote{InstanceId: id}
	assert.Equal(t, "deadbeef", v.InstanceIDHex())

	d := &Decided{InstanceId: id}
	assert.Equal(t, "deadbeef", d.InstanceIDHex())
}

func TestInstanceIDHex_Nil(t *testing.T) {
	var si *StartInstance
	assert.Empty(t, si.InstanceIDHex())

	var v *Vote
	assert.Empty(t, v.InstanceIDHex())

	var d *Decided
	assert.Empty(t, d.InstanceIDHex())
}

func TestTransactionsByChain(t *testing.T) {
	req := &XTRequest{
		TransactionRequests: []*TransactionRequest{
			{ChainId: 901, Transaction: [][]byte{{0x01}, {0x02}}},
			{ChainId: 902, Transaction: [][]byte{{0x03}}},
		},
	}

	m := req.TransactionsByChain()
	assert.Len(t, m, 2)
	assert.Equal(t, [][]byte{{0x01}, {0x02}}, m[901])
	assert.Equal(t, [][]byte{{0x03}}, m[902])
}

func TestTransactionsByChain_Nil(t *testing.T) {
	var req *XTRequest
	assert.Nil(t, req.TransactionsByChain())
}
