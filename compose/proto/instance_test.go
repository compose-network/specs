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
