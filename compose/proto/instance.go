package proto

import "encoding/hex"

// InstanceIDHex returns the instance ID as a lowercase hex string.
// Returns empty string if receiver is nil.
func (x *StartInstance) InstanceIDHex() string {
	if x == nil {
		return ""
	}
	return hex.EncodeToString(x.GetInstanceId())
}

// InstanceIDHex returns the instance ID as a lowercase hex string.
// Returns empty string if receiver is nil.
func (x *Vote) InstanceIDHex() string {
	if x == nil {
		return ""
	}
	return hex.EncodeToString(x.GetInstanceId())
}

// InstanceIDHex returns the instance ID as a lowercase hex string.
// Returns empty string if receiver is nil.
func (x *Decided) InstanceIDHex() string {
	if x == nil {
		return ""
	}
	return hex.EncodeToString(x.GetInstanceId())
}
