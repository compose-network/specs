package proto

import (
	"encoding/hex"
	"testing"

	goproto "google.golang.org/protobuf/proto"
)

// Placeholders for test data
var (
	pubkey, _     = hex.DecodeString("abc")
	signature, _  = hex.DecodeString("def")
	nonce, _      = hex.DecodeString("123")
	instanceid, _ = hex.DecodeString("01")
	proof, _      = hex.DecodeString("156")
	sbhash, _     = hex.DecodeString("1123768")
	source, _     = hex.DecodeString("adecb123")
	receiver, _   = hex.DecodeString("192bca")
	data, _       = hex.DecodeString("aaabbb")
	tx1, _        = hex.DecodeString("aa1")
	tx2, _        = hex.DecodeString("aa2")
)

// assertSerializationHex marshals msg and compares its hex encoding with expectedHex.
func assertSerializationHex(t *testing.T, name string, msg goproto.Message, expectedHex string) {
	t.Helper()

	// Marshal the message
	serialized, err := goproto.Marshal(msg)
	if err != nil {
		t.Fatalf("failed to marshal %s: %v", name, err)
	}

	// Compute msg hex
	actualHex := hex.EncodeToString(serialized)
	t.Logf("%s serialized bytes (hex): %s", name, actualHex)

	if expectedHex == "" {
		t.Fatalf("expected hex for %s not set; got: %s", name, actualHex)
	}

	// Compare computes hex vs. expected
	if actualHex != expectedHex {
		t.Fatalf("%s serialized bytes do not match expected.\nGot:  %s\nWant: %s", name, actualHex, expectedHex)
	}
}

func TestHandshakeRequestSerialization(t *testing.T) {
	msg := &HandshakeRequest{
		Timestamp: 1234567890,
		PublicKey: pubkey,
		Signature: signature,
		ClientId:  "client-1",
		Nonce:     nonce,
	}
	const expectedHex = "08d285d8cc041201ab1a01de2208636c69656e742d312a0112"
	assertSerializationHex(t, "HandshakeRequest", msg, expectedHex)
}

func TestHandshakeResponseSerialization(t *testing.T) {
	msg := &HandshakeResponse{
		Accepted:  true,
		Error:     "error-message",
		SessionId: "session-123",
	}

	const expectedHex = "0801120d6572726f722d6d6573736167651a0b73657373696f6e2d313233"
	assertSerializationHex(t, "HandshakeResponse", msg, expectedHex)
}

func TestPingSerialization(t *testing.T) {
	msg := &Ping{
		Timestamp: 42,
	}

	const expectedHex = "082a"
	assertSerializationHex(t, "Ping", msg, expectedHex)
}

func TestPongSerialization(t *testing.T) {
	msg := &Pong{
		Timestamp: 43,
	}

	const expectedHex = "082b"
	assertSerializationHex(t, "Pong", msg, expectedHex)
}

func TestTransactionRequestSerialization(t *testing.T) {
	msg := &TransactionRequest{
		ChainId: 1,
		Transaction: [][]byte{
			tx1,
			tx2,
		},
	}

	const expectedHex = "08011201aa1201aa"
	assertSerializationHex(t, "TransactionRequest", msg, expectedHex)
}

func TestXTRequestSerialization(t *testing.T) {
	msg := &XTRequest{
		TransactionRequests: []*TransactionRequest{
			{
				ChainId: 10,
				Transaction: [][]byte{
					tx1,
				},
			},
			{
				ChainId: 11,
				Transaction: [][]byte{
					tx2,
				},
			},
		},
	}

	const expectedHex = "0a05080a1201aa0a05080b1201aa"
	assertSerializationHex(t, "XTRequest", msg, expectedHex)
}

func TestStartInstanceSerialization(t *testing.T) {
	msg := &StartInstance{
		InstanceId:     instanceid,
		PeriodId:       100,
		SequenceNumber: 7,
		XtRequest: &XTRequest{
			TransactionRequests: []*TransactionRequest{
				{
					ChainId:     1,
					Transaction: [][]byte{tx1},
				},
			},
		},
	}

	const expectedHex = "0a01011064180722070a0508011201aa"
	assertSerializationHex(t, "StartInstance", msg, expectedHex)
}

func TestVoteSerialization(t *testing.T) {
	msg := &Vote{
		InstanceId: instanceid,
		ChainId:    2,
		Vote:       true,
	}

	const expectedHex = "0a010110021801"
	assertSerializationHex(t, "Vote", msg, expectedHex)
}

func TestDecidedSerialization(t *testing.T) {
	msg := &Decided{
		InstanceId: instanceid,
		Decision:   false,
	}

	const expectedHex = "0a0101"
	assertSerializationHex(t, "Decided", msg, expectedHex)
}

func TestMailboxMessageSerialization(t *testing.T) {
	msg := &MailboxMessage{
		SessionId:        123,
		InstanceId:       instanceid,
		SourceChain:      1,
		DestinationChain: 2,
		Source:           source,
		Receiver:         receiver,
		Label:            "label-1",
		Data: [][]byte{
			data,
		},
	}

	const expectedHex = "087b120101180120022a04adecb1233203192bca3a076c6162656c2d314203aaabbb"
	assertSerializationHex(t, "MailboxMessage", msg, expectedHex)
}

func TestStartPeriodSerialization(t *testing.T) {
	msg := &StartPeriod{
		PeriodId:         5,
		SuperblockNumber: 42,
	}

	const expectedHex = "0805102a"
	assertSerializationHex(t, "StartPeriod", msg, expectedHex)
}

func TestRollbackSerialization(t *testing.T) {
	msg := &Rollback{
		PeriodId:                      6,
		LastFinalizedSuperblockNumber: 41,
		LastFinalizedSuperblockHash:   sbhash,
	}

	const expectedHex = "080610291a03112376"
	assertSerializationHex(t, "Rollback", msg, expectedHex)
}

func TestProofSerialization(t *testing.T) {
	msg := &Proof{
		PeriodId:         7,
		SuperblockNumber: 99,
		ProofData:        proof,
	}

	const expectedHex = "080710631a0115"
	assertSerializationHex(t, "Proof", msg, expectedHex)
}

func TestNativeDecidedSerialization(t *testing.T) {
	msg := &NativeDecided{
		InstanceId: instanceid,
		Decision:   true,
	}

	const expectedHex = "0a01011001"
	assertSerializationHex(t, "NativeDecided", msg, expectedHex)
}

func TestWSDecidedSerialization(t *testing.T) {
	msg := &WSDecided{
		InstanceId: instanceid,
		Decision:   false,
	}

	const expectedHex = "0a0101"
	assertSerializationHex(t, "WSDecided", msg, expectedHex)
}

func TestMessageSerialization(t *testing.T) {
	msg := &Message{
		SenderId: "sender-1",
		Payload: &Message_HandshakeRequest{
			HandshakeRequest: &HandshakeRequest{
				Timestamp: 987654321,
				PublicKey: pubkey,
				Signature: signature,
				ClientId:  "client-in-message",
				Nonce:     nonce,
			},
		},
	}

	const expectedHex = "0a0873656e6465722d31122208b1d1f9d6031201ab1a01de2211636c69656e742d696e2d6d6573736167652a0112"
	assertSerializationHex(t, "Message", msg, expectedHex)
}
