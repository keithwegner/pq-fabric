package messages

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
)

func CanonicalJSON(v any) ([]byte, error) {
	return json.Marshal(v)
}

func HashBytes(message []byte) string {
	h := sha256.Sum256(message)
	return hex.EncodeToString(h[:])
}

func HashCanonical(v any) (string, error) {
	b, err := CanonicalJSON(v)
	if err != nil {
		return "", err
	}
	return HashBytes(b), nil
}
