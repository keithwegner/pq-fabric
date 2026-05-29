package main

import "testing"

func TestShortHash(t *testing.T) {
	if got := shortHash("GENESIS"); got != "GENESIS" {
		t.Fatalf("short hash should not be truncated, got %q", got)
	}
	if got := shortHash("1234567890abcdef"); got != "1234567890ab" {
		t.Fatalf("expected truncated hash, got %q", got)
	}
}

func TestMustAllowsNil(t *testing.T) {
	must(nil)
}
