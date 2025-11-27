package sbcp

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestGenerateInstanceID_stability_and_sensitivity(t *testing.T) {
	req1 := makeXTRequest(
		chainReq(1, []byte{0x01, 0x02}),
		chainReq(2, []byte{0x03}),
	)
	req1Copy := makeXTRequest(
		chainReq(1, []byte{0x01, 0x02}),
		chainReq(2, []byte{0x03}),
	)
	idA := GenerateInstanceID(10, 1, req1)
	idA2 := GenerateInstanceID(10, 1, req1Copy)
	assert.Equal(t, idA, idA2, "same inputs must yield same ID")

	// Period change
	idB := GenerateInstanceID(11, 1, req1)
	assert.NotEqual(t, idA, idB)

	// Sequence change
	idC := GenerateInstanceID(10, 2, req1)
	assert.NotEqual(t, idA, idC)

	// Tx bytes change (single byte)
	reqMut := makeXTRequest(
		chainReq(1, []byte{0x01, 0x02}),
		chainReq(2, []byte{0xFF}),
	)
	idD := GenerateInstanceID(10, 1, reqMut)
	assert.NotEqual(t, idA, idD)

	// Order matters
	reqReordered := makeXTRequest(
		chainReq(2, []byte{0x03}),
		chainReq(1, []byte{0x01, 0x02}),
	)
	idE := GenerateInstanceID(10, 1, reqReordered)
	assert.NotEqual(t, idA, idE)

	// Empty tx bytes is not ignored
	reqWithEmpty := makeXTRequest(
		chainReq(1, []byte{0x01, 0x02}),
		chainReq(2, []byte{}),
	)
	reqOmit := makeXTRequest(
		chainReq(1, []byte{0x01, 0x02}),
	)
	idF := GenerateInstanceID(10, 3, reqWithEmpty)
	idG := GenerateInstanceID(10, 3, reqOmit)
	assert.NotEqual(t, idF, idG, "empty tx bytes are not ignored in ID")
}
