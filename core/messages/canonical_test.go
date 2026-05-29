package messages

import (
	"math"
	"testing"
)

func TestCanonicalHashHelpers(t *testing.T) {
	type sample struct {
		Name string `json:"name"`
	}
	canonical, err := CanonicalJSON(sample{Name: "pq"})
	if err != nil {
		t.Fatal(err)
	}
	if string(canonical) != `{"name":"pq"}` {
		t.Fatalf("unexpected canonical JSON: %s", canonical)
	}
	if HashBytes([]byte("abc")) != "ba7816bf8f01cfea414140de5dae2223b00361a396177a9cb410ff61f20015ad" {
		t.Fatal("unexpected SHA-256 hash")
	}
	hash, err := HashCanonical(sample{Name: "pq"})
	if err != nil {
		t.Fatal(err)
	}
	if hash != HashBytes(canonical) {
		t.Fatal("HashCanonical should hash canonical JSON")
	}
	if _, err := HashCanonical(math.Inf(1)); err == nil {
		t.Fatal("expected unsupported value error")
	}
}
