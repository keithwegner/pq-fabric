package main

import "testing"

func TestParsePeers(t *testing.T) {
	peers := parsePeers(" validator-1=http://a/ , invalid , validator-2=http://b/ ")
	if len(peers) != 2 {
		t.Fatalf("expected two peers, got %+v", peers)
	}
	if peers["validator-1"] != "http://a" || peers["validator-2"] != "http://b" {
		t.Fatalf("unexpected parsed peers: %+v", peers)
	}
}

func TestGetenv(t *testing.T) {
	t.Setenv("PQ_TEST_VALUE", " configured ")
	if got := getenv("PQ_TEST_VALUE", "fallback"); got != "configured" {
		t.Fatalf("expected trimmed env value, got %q", got)
	}
	t.Setenv("PQ_TEST_EMPTY", " ")
	if got := getenv("PQ_TEST_EMPTY", "fallback"); got != "fallback" {
		t.Fatalf("expected fallback, got %q", got)
	}
}
