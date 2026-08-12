package protocol

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
)

// ProgramDeltaFingerprint binds one exact persisted control program to one
// exact candidate control program. It identifies the accepted obligation
// delta without granting authority to apply it.
func ProgramDeltaFingerprint(prior, candidate string) (string, error) {
	if len(prior) != 64 || len(candidate) != 64 || prior == candidate {
		return "", fmt.Errorf("program delta requires distinct 64-character prior and candidate fingerprints")
	}
	digest := sha256.Sum256([]byte("boatstack-program-delta-v1\x00" + prior + "\x00" + candidate))
	return hex.EncodeToString(digest[:]), nil
}
