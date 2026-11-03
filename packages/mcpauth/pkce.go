package mcpauth

import (
	"crypto/sha256"
	"encoding/base64"
)

func pkce(s string) string {
	h := sha256.Sum256([]byte(s))
	return base64.RawURLEncoding.EncodeToString(h[:])
}
