package plugin

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"strings"
)

// signaturePrefix is optional so operators can paste either the raw base64
// digest or the self-describing form emitted by scripts/pack-plugin.sh.
const signaturePrefix = "sha256:"

// SignPayload returns the base64-encoded HMAC-SHA256 of payload. The key is
// supplied by the operator's plugins.signing_key configuration; it is never
// written into a plugin package.
func SignPayload(key, payload []byte) string {
	mac := hmac.New(sha256.New, key)
	_, _ = mac.Write(payload)

	return base64.StdEncoding.EncodeToString(mac.Sum(nil))
}

// VerifySignature constant-time checks a signature over payload. An empty key
// or signature always fails, so an unconfigured deployment never accepts an
// unsigned community package by accident.
func VerifySignature(payload []byte, signature string, key []byte) bool {
	trimmed := strings.TrimSpace(signature)
	trimmed = strings.TrimPrefix(trimmed, signaturePrefix)
	if len(key) == 0 || trimmed == "" {
		return false
	}
	got, err := base64.StdEncoding.DecodeString(trimmed)
	if err != nil {
		return false
	}
	want, err := base64.StdEncoding.DecodeString(SignPayload(key, payload))
	if err != nil {
		return false
	}

	return hmac.Equal(got, want)
}
