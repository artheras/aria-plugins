package identity

import "testing"

func TestStableOwnerAndIDValidation(t *testing.T) {
	if Owner("store-1") != "merchant_store-1" {
		t.Fatal("unexpected durable owner")
	}
	if LegacyOwner("old-secret", "old-token") == LegacyOwner("new-secret", "new-token") {
		t.Fatal("legacy ownership fixture is not sensitive to rotation")
	}
	for _, bad := range []string{"", "ab", "store:1", "../store", "Store", "store/name"} {
		if ValidateID(bad) == nil {
			t.Fatalf("accepted invalid ID %q", bad)
		}
	}
	if ValidateID("store-1") != nil {
		t.Fatal("valid ID rejected")
	}
}
