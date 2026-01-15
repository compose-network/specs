package proto

import "encoding/hex"

func (x *StartInstance) InstanceIDHex() string {
	if x == nil {
		return ""
	}
	return hex.EncodeToString(x.GetInstanceId())
}

func (x *Vote) InstanceIDHex() string {
	if x == nil {
		return ""
	}
	return hex.EncodeToString(x.GetInstanceId())
}

func (x *Decided) InstanceIDHex() string {
	if x == nil {
		return ""
	}
	return hex.EncodeToString(x.GetInstanceId())
}
