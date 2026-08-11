package protocol

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
)

func contentID(prefix string, value any) (string, error) {
	raw, err := json.Marshal(value)
	if err != nil {
		return "", fmt.Errorf("canonical encoding: %w", err)
	}
	digest := sha256.Sum256(raw)
	return prefix + hex.EncodeToString(digest[:]), nil
}
