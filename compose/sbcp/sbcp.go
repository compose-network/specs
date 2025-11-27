package sbcp

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"

	"github.com/compose-network/specs/compose"
)

// GenerateInstanceID returns SHA256(periodID || seq || tx1 || tx2 || ... || txn)
func GenerateInstanceID(
	periodID compose.PeriodID,
	seq compose.SequenceNumber,
	xtRequest compose.XTRequest,
) compose.InstanceID {
	var b [8]byte
	buf := bytes.NewBuffer(nil)

	// Encode period timestamp (nanoseconds) as 8 bytes big-endian
	binary.BigEndian.PutUint64(b[:], uint64(periodID))
	buf.Write(b[:])

	// Encode sequence number as 8 bytes big-endian
	binary.BigEndian.PutUint64(b[:], uint64(seq))
	buf.Write(b[:])

	// Append each transaction's chain ID, length and raw bytes
	for _, req := range xtRequest.Transactions {

		// Chain identifier
		binary.BigEndian.PutUint64(b[:], uint64(req.ChainID))
		buf.Write(b[:])

		// Number of transactions for the chain
		binary.BigEndian.PutUint64(b[:], uint64(len(req.Transactions)))
		buf.Write(b[:])

		for _, data := range req.Transactions {
			if len(data) > 0 {
				// Transaction length
				binary.BigEndian.PutUint64(b[:], uint64(len(data)))
				buf.Write(b[:])

				// Transaction bytes
				buf.Write(data)
			}
		}
	}

	sum := sha256.Sum256(buf.Bytes())
	return sum
}
