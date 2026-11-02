package ledgerlib

import "testing"

func TestAccountFromURL(t *testing.T) {
	got, err := AccountFromURL("https://ledger.example/view?account=a1")
	if err != nil || got != "a1" {
		t.Fatalf("got %q, %v", got, err)
	}
}
