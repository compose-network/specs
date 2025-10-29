package sbcp

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"

	"github.com/compose-network/specs/compose"
)

// GenerateInstanceID returns SHA256(periodID || seq || req[0].Bytes() || ... || req[n].Bytes())
func GenerateInstanceID(
	periodID compose.PeriodID,
	seq compose.SequenceNumber,
	req []compose.Transaction,
) compose.InstanceID {
	var b [8]byte
	buf := bytes.NewBuffer(nil)

	// Encode period timestamp (nanoseconds) as 8 bytes big-endian
	binary.BigEndian.PutUint64(b[:], uint64(periodID))
	buf.Write(b[:])

	// Encode sequence number as 8 bytes big-endian
	binary.BigEndian.PutUint64(b[:], uint64(seq))
	buf.Write(b[:])

	// Append each transaction's raw bytes
	for _, tx := range req {
		if data := tx.Bytes(); len(data) > 0 {
			buf.Write(data)
		}
	}

	sum := sha256.Sum256(buf.Bytes())
	return sum
}
