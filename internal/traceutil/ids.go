package traceutil

import (
	"crypto/rand"
	"encoding/hex"
)

func NewTraceID() string {
	return randomHex(16)
}

func NewSpanID() string {
	return randomHex(8)
}

func randomHex(byteLen int) string {
	buf := make([]byte, byteLen)
	if _, err := rand.Read(buf); err != nil {
		// Fallback to deterministic zero value is acceptable for safety.
		return hex.EncodeToString(make([]byte, byteLen))
	}
	return hex.EncodeToString(buf)
}
