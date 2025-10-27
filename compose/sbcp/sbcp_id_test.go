package sbcp

import (
	"compose"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestGenerateInstanceID_stability_and_sensitivity(t *testing.T) {
	req1 := []compose.Transaction{
		fakeChainTx{chain: 1, body: []byte{0x01, 0x02}},
		fakeChainTx{chain: 2, body: []byte{0x03}},
	}
	req1Copy := []compose.Transaction{
		fakeChainTx{chain: 1, body: []byte{0x01, 0x02}},
		fakeChainTx{chain: 2, body: []byte{0x03}},
	}
	idA := generateInstanceID(10, 1, req1)
	idA2 := generateInstanceID(10, 1, req1Copy)
	assert.Equal(t, idA, idA2, "same inputs must yield same ID")

	// Period change
	idB := generateInstanceID(11, 1, req1)
	assert.NotEqual(t, idA, idB)

	// Sequence change
	idC := generateInstanceID(10, 2, req1)
	assert.NotEqual(t, idA, idC)

	// Tx bytes change (single byte)
	reqMut := []compose.Transaction{
		fakeChainTx{chain: 1, body: []byte{0x01, 0x02}},
		fakeChainTx{chain: 2, body: []byte{0xFF}},
	}
	idD := generateInstanceID(10, 1, reqMut)
	assert.NotEqual(t, idA, idD)

	// Order matters
	reqReordered := []compose.Transaction{
		fakeChainTx{chain: 2, body: []byte{0x03}},
		fakeChainTx{chain: 1, body: []byte{0x01, 0x02}},
	}
	idE := generateInstanceID(10, 1, reqReordered)
	assert.NotEqual(t, idA, idE)

	// Empty tx bytes ignored vs omitted
	reqWithEmpty := []compose.Transaction{
		fakeChainTx{chain: 1, body: []byte{0x01, 0x02}},
		fakeChainTx{chain: 2, body: []byte{}},
	}
	reqOmit := []compose.Transaction{
		fakeChainTx{chain: 1, body: []byte{0x01, 0x02}},
	}
	idF := generateInstanceID(10, 3, reqWithEmpty)
	idG := generateInstanceID(10, 3, reqOmit)
	assert.Equal(t, idF, idG, "empty tx bytes are ignored in ID")
}
