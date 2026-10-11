// Package approvals binds human approval to the exact immutable invoice.
package approvals

import (
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"time"

	"github.com/artheras/aria-plugins/packages/sdk"
)

func Digest(invoice sdk.Invoice) string {
	b, _ := json.Marshal(invoice)
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:])
}
func Approve(order *sdk.Order, reviewed string, now time.Time) error {
	actual := Digest(order.Invoice)
	if len(reviewed) != 64 || subtle.ConstantTimeCompare([]byte(actual), []byte(reviewed)) != 1 {
		return errors.New("the reviewed invoice has changed; reload and review it again")
	}
	if order.State == "approved" && order.ApprovedDigest == actual {
		return nil
	}
	if order.State != "draft" {
		return errors.New("only a draft can be approved")
	}
	order.ApprovedDigest = actual
	order.ApprovedAt = now.UTC().Format(time.RFC3339)
	order.State = "approved"
	return nil
}
func Check(order sdk.Order, now time.Time) error {
	if order.ApprovedDigest == "" || order.ApprovedDigest != Digest(order.Invoice) {
		return errors.New("human approval required")
	}
	at, err := time.Parse(time.RFC3339, order.ApprovedAt)
	if err != nil || now.Sub(at) > 30*time.Minute || at.After(now.Add(time.Minute)) {
		return errors.New("approval expired; create and review a new draft")
	}
	return nil
}
