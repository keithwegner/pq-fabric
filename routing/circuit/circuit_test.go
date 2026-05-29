package circuit

import (
	"bytes"
	"testing"

	pqcrypto "github.com/keithwegner/pq-fabric/core/crypto"
	cryptosuite "github.com/keithwegner/pq-fabric/core/crypto/suite"
)

func TestBuildPrivateTestbedCircuit(t *testing.T) {
	t.Setenv(cryptosuite.EnvVar, string(cryptosuite.Dev))
	relays := []string{"relay-1", "relay-4", "relay-7"}
	pubs := make(map[string][]byte)
	selected := cryptosuite.MustLookup(string(cryptosuite.Dev))
	for _, id := range relays {
		kem, err := selected.NewKEMPrivate(id)
		if err != nil {
			t.Fatal(err)
		}
		pubs[id] = kem.PublicKey()
	}
	circuit, err := BuildPrivateTestbedCircuit(relays, pubs)
	if err != nil {
		t.Fatal(err)
	}
	if len(circuit.Hops) != 3 {
		t.Fatalf("expected 3 hops, got %d", len(circuit.Hops))
	}
	if circuit.Hops[0].Role != Entry || circuit.Hops[1].Role != Middle || circuit.Hops[2].Role != Exit {
		t.Fatalf("unexpected hop roles: %+v", circuit.Hops)
	}
}

func TestLayeredEnvelopeOnlyRemovesOneLayerAtATime(t *testing.T) {
	layers := []LayerKey{
		{RelayID: "relay-7", Key: []byte("exit key material")},
		{RelayID: "relay-4", Key: []byte("middle key material")},
		{RelayID: "relay-1", Key: []byte("entry key material")},
	}
	encoded, err := WrapLayers(layers, "circuit-1", 1, "data", 1, []byte("payload"))
	if err != nil {
		t.Fatal(err)
	}
	entryCell, inner, err := UnwrapLayer(layers[2].Key, encoded)
	if err != nil {
		t.Fatal(err)
	}
	if entryCell.RelayID != "relay-1" {
		t.Fatalf("entry should remove only entry layer, got %s", entryCell.RelayID)
	}
	if bytes.Equal(inner, []byte("payload")) {
		t.Fatal("entry layer should not reveal final plaintext")
	}
	if _, _, err := UnwrapLayer(layers[2].Key, inner); err == nil {
		t.Fatal("entry key should not remove middle layer")
	}
	middleCell, inner, err := UnwrapLayer(layers[1].Key, inner)
	if err != nil {
		t.Fatal(err)
	}
	if middleCell.RelayID != "relay-4" {
		t.Fatalf("middle should remove only middle layer, got %s", middleCell.RelayID)
	}
	exitCell, plaintext, err := UnwrapLayer(layers[0].Key, inner)
	if err != nil {
		t.Fatal(err)
	}
	if exitCell.RelayID != "relay-7" || string(plaintext) != "payload" {
		t.Fatalf("exit should reveal final plaintext, cell=%+v plaintext=%q", exitCell, string(plaintext))
	}
}

func TestExtensionRejectsWrongRelayKey(t *testing.T) {
	t.Setenv(cryptosuite.EnvVar, string(cryptosuite.Dev))
	selected := cryptosuite.MustLookup(string(cryptosuite.Dev))
	entry, err := selected.NewKEMPrivate("relay-1")
	if err != nil {
		t.Fatal(err)
	}
	wrong, err := selected.NewKEMPrivate("relay-2")
	if err != nil {
		t.Fatal(err)
	}
	extension, _, err := CreateExtension("circuit-1", "relay-2", Entry, entry.PublicKey(), func(public []byte) (pqcrypto.KEMPublicKey, error) {
		return selected.NewKEMPublic(public)
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := VerifyExtension(extension, wrong); err == nil {
		t.Fatal("expected wrong relay key to fail extension proof")
	}
}

func TestBuildPrivateTestbedCircuitRejectsInvalidInputs(t *testing.T) {
	t.Setenv(cryptosuite.EnvVar, string(cryptosuite.Dev))
	if _, err := BuildPrivateTestbedCircuit([]string{"a", "b"}, nil); err == nil {
		t.Fatal("expected hop count error")
	}
	if _, err := BuildPrivateTestbedCircuit([]string{"a", "b", "c"}, map[string][]byte{"a": []byte("short")}); err == nil {
		t.Fatal("expected missing relay public key error")
	}
	if _, err := BuildPrivateTestbedCircuit([]string{"a", "b", "c"}, map[string][]byte{
		"a": []byte("short"),
		"b": []byte("short"),
		"c": []byte("short"),
	}); err == nil {
		t.Fatal("expected invalid public key error")
	}
}
