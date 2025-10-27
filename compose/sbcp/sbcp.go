package sbcp

import (
	"bytes"
	"compose"
	"crypto/sha256"
	"encoding/binary"
)

type PriorityQueue [][]compose.Transaction

// generateInstanceID returns SHA256(periodID || seq || req[0].Bytes() || ... || req[n].Bytes())
func generateInstanceID(
	periodID compose.PeriodID,
	seq compose.SequenceNumber,
	req []compose.Transaction,
) compose.InstanceID {
	var b [8]byte
	buf := bytes.NewBuffer(nil)

	// Encode period timestamp (nanoseconds) as 8 bytes big-endian
	binary.BigEndian.PutUint64(b[:], uint64(periodID.Time().UnixNano()))
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
