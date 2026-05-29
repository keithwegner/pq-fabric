package relay

import (
	"testing"

	cryptosuite "github.com/keithwegner/pq-fabric/core/crypto/suite"
	"github.com/keithwegner/pq-fabric/routing/circuit"
)

func TestDevelopmentRelayLifecycle(t *testing.T) {
	relay, err := NewDevelopmentRelay("validator-1", "nyc")
	if err != nil {
		t.Fatal(err)
	}
	if relay.ID != "validator-1" || relay.Region != "nyc" {
		t.Fatalf("unexpected relay: %+v", relay)
	}
	if len(relay.PublicKey()) == 0 {
		t.Fatal("expected public key")
	}
	relay.Start()
	if !relay.Running {
		t.Fatal("expected relay to be running")
	}
	relay.Stop()
	if relay.Running {
		t.Fatal("expected relay to stop")
	}
}

func TestRelayAcceptExtensionRejectsReplayAndTeardown(t *testing.T) {
	selected := cryptosuite.MustLookup(string(cryptosuite.Dev))
	relay, err := NewRelayForSuite("relay-1", "nyc", selected)
	if err != nil {
		t.Fatal(err)
	}
	relay.Start()
	extension, _, err := circuit.CreateExtension("circuit-1", relay.ID, circuit.Entry, relay.PublicKey(), selected.NewKEMPublic)
	if err != nil {
		t.Fatal(err)
	}
	state, err := relay.AcceptExtension(extension, "client", "relay-4")
	if err != nil {
		t.Fatal(err)
	}
	if state.PreviousHop != "client" || state.NextHop != "relay-4" {
		t.Fatalf("unexpected local state: %+v", state)
	}
	if _, err := relay.AcceptExtension(extension, "client", "relay-4"); err == nil {
		t.Fatal("expected replayed extension to be rejected")
	}
	visibility := relay.Visibility("circuit-1")
	if !visibility.KnowsClientConnection || visibility.KnowsFinalDestination || visibility.KnowsFullPath {
		t.Fatalf("unexpected entry visibility: %+v", visibility)
	}
	relay.Teardown("circuit-1")
	if _, ok := relay.Session("circuit-1"); ok {
		t.Fatal("expected teardown to remove circuit session")
	}
}

func TestExitPolicyRejectsDisallowedDestination(t *testing.T) {
	policy := NewExitPolicy([]string{"local-echo:7000"})
	if !policy.Allows("local-echo:7000") {
		t.Fatal("expected local echo destination to be allowed")
	}
	if policy.Allows("example.com:80") {
		t.Fatal("expected public destination to be rejected")
	}
}

func TestRelayRejectsUnknownCircuitCell(t *testing.T) {
	selected := cryptosuite.MustLookup(string(cryptosuite.Dev))
	relay, err := NewRelayForSuite("relay-1", "nyc", selected)
	if err != nil {
		t.Fatal(err)
	}
	relay.Start()
	encoded, err := circuit.WrapLayer(circuit.LayerKey{RelayID: "relay-1", Key: []byte("unknown circuit key")}, "missing-circuit", 1, "data", 1, []byte("payload"))
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := relay.ProcessCell(encoded); err == nil {
		t.Fatal("expected unknown circuit id to be rejected")
	}
}

func TestRelayForPQSuite(t *testing.T) {
	selected, err := cryptosuite.Lookup("pq")
	if err != nil {
		t.Fatal(err)
	}
	relay, err := NewRelayForSuite("validator-1", "nyc", selected)
	if err != nil {
		t.Fatal(err)
	}
	if relay.KEM.Algorithm() != selected.KEMAlgorithm || len(relay.PublicKey()) == 0 {
		t.Fatalf("unexpected PQ relay metadata: %+v", relay)
	}
}
