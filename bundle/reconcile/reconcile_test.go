package reconcile

import "testing"

func TestDeterministicMatch(t *testing.T) {
	digest := StateDigest{Height: 3, LastHash: "abc"}
	if err := DeterministicMatch(digest, digest); err != nil {
		t.Fatal(err)
	}
	if err := DeterministicMatch(digest, StateDigest{Height: 4, LastHash: "abc"}); err == nil {
		t.Fatal("expected height divergence")
	}
	if err := DeterministicMatch(digest, StateDigest{Height: 3, LastHash: "def"}); err == nil {
		t.Fatal("expected hash divergence")
	}
}
