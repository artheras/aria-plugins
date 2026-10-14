// Package identity separates durable ownership from replaceable credentials.
package identity

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"regexp"
)

var merchantID = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]{2,63}$`)
var legacyID = regexp.MustCompile(`^[a-f0-9]{64}$`)

func ValidateID(id string) error {
	if !merchantID.MatchString(id) {
		return errors.New("ARIA_MERCHANT_ID must contain 3 to 64 lowercase letters, digits, underscores or hyphens")
	}
	return nil
}

func ValidateLegacyOwner(owner string) bool { return legacyID.MatchString(owner) }

func Owner(id string) string { return "merchant_" + id }

// LegacyOwner is only for migration from releases that derived ownership from secrets.
func LegacyOwner(sessionSecret, merchantToken string) string {
	h := hmac.New(sha256.New, []byte(sessionSecret))
	h.Write([]byte("merchant:" + merchantToken))
	return hex.EncodeToString(h.Sum(nil))
}
