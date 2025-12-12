package proto

import (
	"encoding/hex"
	"fmt"
	"os"
	"strconv"
	"strings"
	"testing"

	goproto "google.golang.org/protobuf/proto"

	"github.com/cucumber/godog"
)

type wireField struct {
	Name   string
	Format string
	Value  string
}

type wireScenario struct {
	Name        string
	MessageType string
	Fields      []wireField
	ExpectedHex string
}

// testContext keeps state through Gherkin steps for a single scenario.
type testContext struct {
	actualMessageHex string
}

// theFollowingMessageIsSerialized implements the following Gherkin steps:
//
//	When the following "<Type>" message is serialized:
//	  | field | format | value |
//	  ...
//
// and
//
//	When the following "<Type>" wrapper is serialized:
//	  ...
func (c *testContext) theFollowingMessageIsSerialized(messageType string, table *godog.Table) error {
	if table == nil || len(table.Rows) < 2 {
		return fmt.Errorf("expected header and at least one data row in table")
	}

	// Convert godog table rows into wireField entries (skip header).
	var fields []wireField
	for i, row := range table.Rows {
		if i == 0 {
			// header: field | format | value
			continue
		}
		if len(row.Cells) < 3 {
			return fmt.Errorf("row %d must have at least 3 cells (field, format, value)", i)
		}
		fields = append(fields, wireField{
			Name:   row.Cells[0].Value,
			Format: row.Cells[1].Value,
			Value:  row.Cells[2].Value,
		})
	}

	sc := wireScenario{
		MessageType: messageType,
		Fields:      fields,
	}

	msg, err := buildMessageFromScenario(sc)
	if err != nil {
		return fmt.Errorf("build message: %w", err)
	}

	msgBytes, err := goproto.Marshal(msg)
	if err != nil {
		return fmt.Errorf("marshal message: %w", err)
	}

	c.actualMessageHex = hex.EncodeToString(msgBytes)
	return nil
}

// theWireHexShouldBe implements the following Gherkin step:
//
//	Then the wire hex should be "<hex>"
func (c *testContext) theWireHexShouldBe(expectedHex string) error {
	if c.actualMessageHex == "" {
		return fmt.Errorf("no message has been serialized in this scenario")
	}
	if expectedHex == "" {
		return fmt.Errorf("expected hex is empty, actual: %s", c.actualMessageHex)
	}
	if c.actualMessageHex != expectedHex {
		return fmt.Errorf("wire hex mismatch.\nGot:  %s\nWant: %s", c.actualMessageHex, expectedHex)
	}
	return nil
}

// InitializeScenario wires the Gherkin steps to the step implementations.
func InitializeScenario(ctx *godog.ScenarioContext) {
	state := &testContext{}

	ctx.Step(`^the following "([^"]*)" message is serialized:$`, state.theFollowingMessageIsSerialized)
	ctx.Step(`^the following "([^"]*)" wrapper is serialized:$`, state.theFollowingMessageIsSerialized)
	ctx.Step(`^the wire hex should be "([^"]*)"$`, state.theWireHexShouldBe)
}

// TestMain integrates godog with `go test` to run the gherkin/proto/wire.feature feature file
func TestMain(m *testing.M) {
	status := godog.TestSuite{
		Name:                 "wire-feature",
		ScenarioInitializer:  InitializeScenario,
		TestSuiteInitializer: nil,
		Options: &godog.Options{
			Format: "pretty",
			Paths:  []string{"../../gherkin/proto/wire.feature"},
		},
	}.Run()

	if st := m.Run(); st > status {
		status = st
	}
	os.Exit(status)
}

func buildMessageFromScenario(sc wireScenario) (goproto.Message, error) {
	switch sc.MessageType {
	case "HandshakeRequest":
		msg := &HandshakeRequest{}
		for _, f := range sc.Fields {
			switch f.Name {
			case "timestamp":
				v, err := parseInt64(f.Value)
				if err != nil {
					return nil, fmt.Errorf("HandshakeRequest.timestamp: %w", err)
				}
				msg.Timestamp = v
			case "public_key":
				b, err := decodeHexLikeGo(f.Value)
				if err != nil {
					return nil, fmt.Errorf("HandshakeRequest.public_key: %w", err)
				}
				msg.PublicKey = b
			case "signature":
				b, err := decodeHexLikeGo(f.Value)
				if err != nil {
					return nil, fmt.Errorf("HandshakeRequest.signature: %w", err)
				}
				msg.Signature = b
			case "client_id":
				msg.ClientId = f.Value
			case "nonce":
				b, err := decodeHexLikeGo(f.Value)
				if err != nil {
					return nil, fmt.Errorf("HandshakeRequest.nonce: %w", err)
				}
				msg.Nonce = b
			}
		}
		return msg, nil

	case "HandshakeResponse":
		msg := &HandshakeResponse{}
		for _, f := range sc.Fields {
			switch f.Name {
			case "accepted":
				v, err := strconv.ParseBool(f.Value)
				if err != nil {
					return nil, fmt.Errorf("HandshakeResponse.accepted: %w", err)
				}
				msg.Accepted = v
			case "error":
				msg.Error = f.Value
			case "session_id":
				msg.SessionId = f.Value
			}
		}
		return msg, nil

	case "Ping":
		msg := &Ping{}
		for _, f := range sc.Fields {
			if f.Name == "timestamp" {
				v, err := parseInt64(f.Value)
				if err != nil {
					return nil, fmt.Errorf("Ping.timestamp: %w", err)
				}
				msg.Timestamp = v
			}
		}
		return msg, nil

	case "Pong":
		msg := &Pong{}
		for _, f := range sc.Fields {
			if f.Name == "timestamp" {
				v, err := parseInt64(f.Value)
				if err != nil {
					return nil, fmt.Errorf("Pong.timestamp: %w", err)
				}
				msg.Timestamp = v
			}
		}
		return msg, nil

	case "TransactionRequest":
		msg := &TransactionRequest{}
		for _, f := range sc.Fields {
			switch {
			case f.Name == "chain_id":
				v, err := parseUint64(f.Value)
				if err != nil {
					return nil, fmt.Errorf("TransactionRequest.chain_id: %w", err)
				}
				msg.ChainId = v
			case strings.HasPrefix(f.Name, "transaction"):
				b, err := decodeHexLikeGo(f.Value)
				if err != nil {
					return nil, fmt.Errorf("TransactionRequest.%s: %w", f.Name, err)
				}
				msg.Transaction = append(msg.Transaction, b)
			}
		}
		return msg, nil

	case "XTRequest":
		msg := &XTRequest{}
		for _, f := range sc.Fields {
			if strings.HasPrefix(f.Name, "transaction") && f.Format == "nested" {
				tr, err := parseTransactionRequestNested(f.Value)
				if err != nil {
					return nil, fmt.Errorf("XTRequest.%s: %w", f.Name, err)
				}
				msg.TransactionRequests = append(msg.TransactionRequests, tr)
			}
		}
		return msg, nil

	case "StartInstance":
		msg := &StartInstance{}
		for _, f := range sc.Fields {
			switch f.Name {
			case "instance_id":
				b, err := decodeHexLikeGo(f.Value)
				if err != nil {
					return nil, fmt.Errorf("StartInstance.instance_id: %w", err)
				}
				msg.InstanceId = b
			case "period_id":
				v, err := parseUint64(f.Value)
				if err != nil {
					return nil, fmt.Errorf("StartInstance.period_id: %w", err)
				}
				msg.PeriodId = v
			case "sequence_number":
				v, err := parseUint64(f.Value)
				if err != nil {
					return nil, fmt.Errorf("StartInstance.sequence_number: %w", err)
				}
				msg.SequenceNumber = v
			case "xt_request":
				if f.Format != "nested" {
					return nil, fmt.Errorf("StartInstance.xt_request must be nested")
				}
				xt, err := parseXTRequestNested(f.Value)
				if err != nil {
					return nil, fmt.Errorf("StartInstance.xt_request: %w", err)
				}
				msg.XtRequest = xt
			}
		}
		return msg, nil

	case "Vote":
		msg := &Vote{}
		for _, f := range sc.Fields {
			switch f.Name {
			case "instance_id":
				b, err := decodeHexLikeGo(f.Value)
				if err != nil {
					return nil, fmt.Errorf("Vote.instance_id: %w", err)
				}
				msg.InstanceId = b
			case "chain_id":
				v, err := parseUint64(f.Value)
				if err != nil {
					return nil, fmt.Errorf("Vote.chain_id: %w", err)
				}
				msg.ChainId = v
			case "vote":
				v, err := strconv.ParseBool(f.Value)
				if err != nil {
					return nil, fmt.Errorf("Vote.vote: %w", err)
				}
				msg.Vote = v
			}
		}
		return msg, nil

	case "Decided":
		msg := &Decided{}
		for _, f := range sc.Fields {
			switch f.Name {
			case "instance_id":
				b, err := decodeHexLikeGo(f.Value)
				if err != nil {
					return nil, fmt.Errorf("Decided.instance_id: %w", err)
				}
				msg.InstanceId = b
			case "decision":
				v, err := strconv.ParseBool(f.Value)
				if err != nil {
					return nil, fmt.Errorf("Decided.decision: %w", err)
				}
				msg.Decision = v
			}
		}
		return msg, nil

	case "MailboxMessage":
		msg := &MailboxMessage{}
		for _, f := range sc.Fields {
			switch {
			case f.Name == "session_id":
				v, err := parseUint64(f.Value)
				if err != nil {
					return nil, fmt.Errorf("MailboxMessage.session_id: %w", err)
				}
				msg.SessionId = v
			case f.Name == "instance_id":
				b, err := decodeHexLikeGo(f.Value)
				if err != nil {
					return nil, fmt.Errorf("MailboxMessage.instance_id: %w", err)
				}
				msg.InstanceId = b
			case f.Name == "source_chain":
				v, err := parseUint64(f.Value)
				if err != nil {
					return nil, fmt.Errorf("MailboxMessage.source_chain: %w", err)
				}
				msg.SourceChain = v
			case f.Name == "destination_chain":
				v, err := parseUint64(f.Value)
				if err != nil {
					return nil, fmt.Errorf("MailboxMessage.destination_chain: %w", err)
				}
				msg.DestinationChain = v
			case f.Name == "source":
				b, err := decodeHexLikeGo(f.Value)
				if err != nil {
					return nil, fmt.Errorf("MailboxMessage.source: %w", err)
				}
				msg.Source = b
			case f.Name == "receiver":
				b, err := decodeHexLikeGo(f.Value)
				if err != nil {
					return nil, fmt.Errorf("MailboxMessage.receiver: %w", err)
				}
				msg.Receiver = b
			case f.Name == "label":
				msg.Label = f.Value
			case strings.HasPrefix(f.Name, "data"):
				b, err := decodeHexLikeGo(f.Value)
				if err != nil {
					return nil, fmt.Errorf("MailboxMessage.%s: %w", f.Name, err)
				}
				msg.Data = append(msg.Data, b)
			}
		}
		return msg, nil

	case "StartPeriod":
		msg := &StartPeriod{}
		for _, f := range sc.Fields {
			switch f.Name {
			case "period_id":
				v, err := parseUint64(f.Value)
				if err != nil {
					return nil, fmt.Errorf("StartPeriod.period_id: %w", err)
				}
				msg.PeriodId = v
			case "superblock_number":
				v, err := parseUint64(f.Value)
				if err != nil {
					return nil, fmt.Errorf("StartPeriod.superblock_number: %w", err)
				}
				msg.SuperblockNumber = v
			}
		}
		return msg, nil

	case "Rollback":
		msg := &Rollback{}
		for _, f := range sc.Fields {
			switch f.Name {
			case "period_id":
				v, err := parseUint64(f.Value)
				if err != nil {
					return nil, fmt.Errorf("Rollback.period_id: %w", err)
				}
				msg.PeriodId = v
			case "last_finalized_superblock_num":
				v, err := parseUint64(f.Value)
				if err != nil {
					return nil, fmt.Errorf("Rollback.last_finalized_superblock_num: %w", err)
				}
				msg.LastFinalizedSuperblockNumber = v
			case "last_finalized_superblock_hash":
				b, err := decodeHexLikeGo(f.Value)
				if err != nil {
					return nil, fmt.Errorf("Rollback.last_finalized_superblock_hash: %w", err)
				}
				msg.LastFinalizedSuperblockHash = b
			}
		}
		return msg, nil

	case "Proof":
		msg := &Proof{}
		for _, f := range sc.Fields {
			switch f.Name {
			case "period_id":
				v, err := parseUint64(f.Value)
				if err != nil {
					return nil, fmt.Errorf("Proof.period_id: %w", err)
				}
				msg.PeriodId = v
			case "superblock_number":
				v, err := parseUint64(f.Value)
				if err != nil {
					return nil, fmt.Errorf("Proof.superblock_number: %w", err)
				}
				msg.SuperblockNumber = v
			case "proof_data":
				b, err := decodeHexLikeGo(f.Value)
				if err != nil {
					return nil, fmt.Errorf("Proof.proof_data: %w", err)
				}
				msg.ProofData = b
			}
		}
		return msg, nil

	case "NativeDecided":
		msg := &NativeDecided{}
		for _, f := range sc.Fields {
			switch f.Name {
			case "instance_id":
				b, err := decodeHexLikeGo(f.Value)
				if err != nil {
					return nil, fmt.Errorf("NativeDecided.instance_id: %w", err)
				}
				msg.InstanceId = b
			case "decision":
				v, err := strconv.ParseBool(f.Value)
				if err != nil {
					return nil, fmt.Errorf("NativeDecided.decision: %w", err)
				}
				msg.Decision = v
			}
		}
		return msg, nil

	case "WSDecided":
		msg := &WSDecided{}
		for _, f := range sc.Fields {
			switch f.Name {
			case "instance_id":
				b, err := decodeHexLikeGo(f.Value)
				if err != nil {
					return nil, fmt.Errorf("WSDecided.instance_id: %w", err)
				}
				msg.InstanceId = b
			case "decision":
				v, err := strconv.ParseBool(f.Value)
				if err != nil {
					return nil, fmt.Errorf("WSDecided.decision: %w", err)
				}
				msg.Decision = v
			}
		}
		return msg, nil

	case "Message":
		msg := &Message{}
		for _, f := range sc.Fields {
			switch f.Name {
			case "sender_id":
				msg.SenderId = f.Value
			case "payload":
				if f.Format != "nested" {
					return nil, fmt.Errorf("Message.payload must be nested")
				}
				hr, err := parseHandshakeRequestNested(f.Value)
				if err != nil {
					return nil, fmt.Errorf("Message.payload: %w", err)
				}
				msg.Payload = &Message_HandshakeRequest{
					HandshakeRequest: hr,
				}
			}
		}
		return msg, nil
	}

	return nil, fmt.Errorf("unsupported message type %q", sc.MessageType)
}

func parseInt64(v string) (int64, error) {
	return strconv.ParseInt(v, 10, 64)
}

func parseUint64(v string) (uint64, error) {
	return strconv.ParseUint(v, 10, 64)
}

// decodeHexLikeGo mimics the behavior of hex.DecodeString in the existing tests.
// For errors due to odd-length strings, the content is still returned with no errors.
// Else, the error is returned.
func decodeHexLikeGo(s string) ([]byte, error) {
	b, err := hex.DecodeString(s)
	if err != nil {
		// Preserve the partial result for ErrLength but treat other errors as fatal.
		if err == hex.ErrLength {
			return b, nil
		}
		return nil, err
	}
	return b, nil
}

func parseTransactionRequestNested(v string) (*TransactionRequest, error) {
	s := strings.TrimSpace(v)
	switch {
	case strings.HasPrefix(s, "TransactionRequest(") && strings.HasSuffix(s, ")"):
		inner := strings.TrimSuffix(strings.TrimPrefix(s, "TransactionRequest("), ")")
		var chainID uint64
		var txs [][]byte
		for _, part := range strings.Split(inner, ",") {
			part = strings.TrimSpace(part)
			switch {
			case strings.HasPrefix(part, "chain_id="):
				val := strings.TrimSpace(strings.TrimPrefix(part, "chain_id="))
				id, err := parseUint64(val)
				if err != nil {
					return nil, err
				}
				chainID = id
			case strings.HasPrefix(part, "tx="):
				list := strings.TrimSpace(strings.TrimPrefix(part, "tx="))
				l, err := parseHexList(list)
				if err != nil {
					return nil, err
				}
				txs = append(txs, l...)
			}
		}
		return &TransactionRequest{
			ChainId:     chainID,
			Transaction: txs,
		}, nil

	case strings.HasPrefix(s, "TR(") && strings.HasSuffix(s, ")"):
		inner := strings.TrimSuffix(strings.TrimPrefix(s, "TR("), ")")
		parts := strings.SplitN(inner, ",", 2)
		if len(parts) != 2 {
			return nil, fmt.Errorf("invalid TR nested format: %q", v)
		}
		idStr := strings.TrimSpace(parts[0])
		id, err := parseUint64(idStr)
		if err != nil {
			return nil, err
		}
		list := strings.TrimSpace(parts[1])
		txs, err := parseHexList(list)
		if err != nil {
			return nil, err
		}
		return &TransactionRequest{
			ChainId:     id,
			Transaction: txs,
		}, nil
	}
	return nil, fmt.Errorf("unsupported TransactionRequest nested value: %q", v)
}

func parseXTRequestNested(v string) (*XTRequest, error) {
	s := strings.TrimSpace(v)
	if !strings.HasPrefix(s, "XTRequest(") || !strings.HasSuffix(s, ")") {
		return nil, fmt.Errorf("unsupported XTRequest nested value: %q", v)
	}
	inner := strings.TrimSuffix(strings.TrimPrefix(s, "XTRequest("), ")")
	inner = strings.TrimSpace(inner)
	if !strings.HasPrefix(inner, "transaction_requests=") {
		return nil, fmt.Errorf("XTRequest nested value missing transaction_requests: %q", v)
	}
	list := strings.TrimSpace(strings.TrimPrefix(inner, "transaction_requests="))
	if !strings.HasPrefix(list, "[") || !strings.HasSuffix(list, "]") {
		return nil, fmt.Errorf("XTRequest transaction_requests must be a list: %q", v)
	}
	list = strings.TrimSpace(strings.TrimPrefix(strings.TrimSuffix(list, "]"), "["))
	if list == "" {
		return &XTRequest{}, nil
	}

	var trs []*TransactionRequest
	// Split on "TR(" occurrences; keep the "TR(" prefix in each item.
	for _, item := range splitTopLevel(list, "TR(") {
		item = strings.TrimSpace(item)
		if item == "" {
			continue
		}
		if !strings.HasPrefix(item, "TR(") {
			item = "TR(" + item + ")"
		}
		tr, err := parseTransactionRequestNested(item)
		if err != nil {
			return nil, err
		}
		trs = append(trs, tr)
	}

	return &XTRequest{
		TransactionRequests: trs,
	}, nil
}

func parseHandshakeRequestNested(v string) (*HandshakeRequest, error) {
	s := strings.TrimSpace(v)
	if !strings.HasPrefix(s, "HandshakeRequest(") || !strings.HasSuffix(s, ")") {
		return nil, fmt.Errorf("unsupported HandshakeRequest nested value: %q", v)
	}
	inner := strings.TrimSuffix(strings.TrimPrefix(s, "HandshakeRequest("), ")")
	msg := &HandshakeRequest{}
	for _, part := range strings.Split(inner, ",") {
		part = strings.TrimSpace(part)
		switch {
		case strings.HasPrefix(part, "timestamp="):
			val := strings.TrimSpace(strings.TrimPrefix(part, "timestamp="))
			v, err := parseInt64(val)
			if err != nil {
				return nil, err
			}
			msg.Timestamp = v
		case strings.HasPrefix(part, "public_key="):
			val := strings.TrimSpace(strings.TrimPrefix(part, "public_key="))
			b, err := decodeHexLikeGo(val)
			if err != nil {
				return nil, err
			}
			msg.PublicKey = b
		case strings.HasPrefix(part, "signature="):
			val := strings.TrimSpace(strings.TrimPrefix(part, "signature="))
			b, err := decodeHexLikeGo(val)
			if err != nil {
				return nil, err
			}
			msg.Signature = b
		case strings.HasPrefix(part, "client_id="):
			val := strings.TrimSpace(strings.TrimPrefix(part, "client_id="))
			msg.ClientId = val
		case strings.HasPrefix(part, "nonce="):
			val := strings.TrimSpace(strings.TrimPrefix(part, "nonce="))
			b, err := decodeHexLikeGo(val)
			if err != nil {
				return nil, err
			}
			msg.Nonce = b
		}
	}
	return msg, nil
}

// parseHexList parses a list like "[aa1]" or "[aa1, aa2]" into a slice of
// byte slices, using decodeHexLikeGo for each element.
func parseHexList(list string) ([][]byte, error) {
	s := strings.TrimSpace(list)
	if strings.HasPrefix(s, "[") && strings.HasSuffix(s, "]") {
		s = strings.TrimPrefix(strings.TrimSuffix(s, "]"), "[")
	}
	s = strings.TrimSpace(s)
	if s == "" {
		return nil, nil
	}

	// Split on comma or whitespace.
	parts := strings.FieldsFunc(s, func(r rune) bool {
		return r == ',' || r == ' ' || r == '\t'
	})

	var out [][]byte
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		b, err := decodeHexLikeGo(p)
		if err != nil {
			return nil, err
		}
		out = append(out, b)
	}
	return out, nil
}

// splitTopLevel splits s on occurrences of sep, keeping the text after each sep
// as a separate element. It assumes that nested structures do not contain sep.
func splitTopLevel(s, sep string) []string {
	var out []string
	for {
		idx := strings.Index(s, sep)
		if idx < 0 {
			if strings.TrimSpace(s) != "" {
				out = append(out, strings.TrimSpace(s))
			}
			break
		}
		if idx > 0 {
			// discard leading text before the first sep
			s = s[idx:]
			idx = 0
		}
		// find next sep
		next := strings.Index(s[idx+len(sep):], sep)
		if next < 0 {
			out = append(out, strings.TrimSpace(s))
			break
		}
		out = append(out, strings.TrimSpace(s[:idx+len(sep)+next]))
		s = s[idx+len(sep)+next:]
	}
	return out
}
