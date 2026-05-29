package testbed

import (
	"context"
	"strings"
	"testing"

	cryptosuite "github.com/keithwegner/pq-fabric/core/crypto/suite"
)

func TestThreeHopCircuitEchoHTTPMultiplexAndVisibility(t *testing.T) {
	t.Setenv(cryptosuite.EnvVar, string(cryptosuite.Dev))
	topology, err := NewSevenRelayTopologyFromEnv()
	if err != nil {
		t.Fatal(err)
	}
	runtime, err := topology.BuildCircuit([]string{"relay-1", "relay-4", "relay-7"})
	if err != nil {
		t.Fatal(err)
	}
	defer runtime.Teardown()

	echo, err := runtime.Send(context.Background(), DestinationEcho, []byte("alpha"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(echo), "relay-7") || !strings.Contains(string(echo), "alpha") {
		t.Fatalf("unexpected echo response %q", string(echo))
	}
	httpResponse, err := runtime.Send(context.Background(), DestinationHTTP, []byte("GET / HTTP/1.1\r\n\r\n"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(string(httpResponse), "HTTP/1.1 200 OK") || !strings.Contains(string(httpResponse), "X-Exit-Relay: relay-7") {
		t.Fatalf("unexpected HTTP response %q", string(httpResponse))
	}
	second, err := runtime.Send(context.Background(), DestinationEcho, []byte("beta"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(second), "beta") {
		t.Fatalf("unexpected second stream response %q", string(second))
	}
	if runtime.Opened != 3 || runtime.Completed != 3 {
		t.Fatalf("expected 3 completed streams, got opened=%d completed=%d", runtime.Opened, runtime.Completed)
	}
	views := runtime.Visibility()
	if len(views) != 3 {
		t.Fatalf("expected 3 relay views, got %d", len(views))
	}
	if !views[0].KnowsClientConnection || views[0].KnowsFinalDestination || views[0].KnowsFullPath {
		t.Fatalf("entry visibility leaked: %+v", views[0])
	}
	if views[1].KnowsClientConnection || views[1].KnowsFinalDestination || views[1].KnowsFullPath {
		t.Fatalf("middle visibility leaked: %+v", views[1])
	}
	if !views[2].KnowsFinalDestination || views[2].KnowsClientConnection || views[2].KnowsFullPath {
		t.Fatalf("exit visibility unexpected: %+v", views[2])
	}
}

func TestDisallowedDestinationAndSOCKS5RoundTrip(t *testing.T) {
	t.Setenv(cryptosuite.EnvVar, string(cryptosuite.Dev))
	topology, err := NewSevenRelayTopologyFromEnv()
	if err != nil {
		t.Fatal(err)
	}
	runtime, err := topology.BuildCircuit([]string{"relay-1", "relay-4", "relay-7"})
	if err != nil {
		t.Fatal(err)
	}
	defer runtime.Teardown()
	if _, err := runtime.Send(context.Background(), "example.com:80", []byte("public")); err == nil {
		t.Fatal("expected disallowed destination to be rejected")
	}
	ok, err := runtime.SOCKS5RoundTrip(context.Background(), DestinationEcho, []byte("via socks5"))
	if err != nil {
		t.Fatal(err)
	}
	if !ok {
		t.Fatal("expected SOCKS5 round trip to succeed")
	}
}

func TestRoutingScenarioEvidence(t *testing.T) {
	t.Setenv(cryptosuite.EnvVar, string(cryptosuite.Dev))
	evidence, err := RunScenario(context.Background(), Options{})
	if err != nil {
		t.Fatal(err)
	}
	if !evidence.FinalSuccess || !evidence.HandshakeSuccess || !evidence.SOCKS5RoundTrip {
		t.Fatalf("expected successful evidence, got %+v", evidence)
	}
	if evidence.RejectedDestinationCount != 1 || evidence.MalformedHandshakeRejectionCount != 1 {
		t.Fatalf("unexpected rejection counts: %+v", evidence)
	}
}

func TestRoutingSuiteCanSelectPQ(t *testing.T) {
	t.Setenv(cryptosuite.EnvVar, string(cryptosuite.PQ))
	topology, err := NewSevenRelayTopologyFromEnv()
	if err != nil {
		t.Fatal(err)
	}
	runtime, err := topology.BuildCircuit([]string{"relay-1", "relay-4", "relay-7"})
	if err != nil {
		t.Fatal(err)
	}
	defer runtime.Teardown()
	if string(topology.Suite.Name) != "pq" {
		t.Fatalf("expected pq suite, got %s", topology.Suite.Name)
	}
}

func TestRoutingSuiteDefaultsToDev(t *testing.T) {
	t.Setenv(cryptosuite.EnvVar, "")
	topology, err := NewSevenRelayTopologyFromEnv()
	if err != nil {
		t.Fatal(err)
	}
	if string(topology.Suite.Name) != "dev" {
		t.Fatalf("expected dev suite, got %s", topology.Suite.Name)
	}
}
