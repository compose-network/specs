package proto

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

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
