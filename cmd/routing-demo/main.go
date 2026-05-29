package main

import (
	"context"
	"fmt"
	"log"
	"path/filepath"

	"github.com/keithwegner/pq-fabric/routing/testbed"
)

func main() {
	evidence, err := testbed.RunScenario(context.Background(), testbed.Options{
		WriteArtifacts:   true,
		EvidenceJSONPath: filepath.Join("tmp", "routing-evidence.json"),
		EvidenceTextPath: filepath.Join("tmp", "routing-evidence.txt"),
	})
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println("pq-fabric private routing testbed demo")
	fmt.Printf("seven relays initialized; selected path entry=%s middle=%s exit=%s\n", evidence.EntryRelay, evidence.MiddleRelay, evidence.ExitRelay)
	fmt.Printf("circuit=%s suite=%s handshake_success=%t\n", evidence.CircuitID, evidence.CryptoSuite, evidence.HandshakeSuccess)
	fmt.Printf("streams opened=%d completed=%d socks5_round_trip=%t\n", evidence.StreamsOpened, evidence.StreamsCompleted, evidence.SOCKS5RoundTrip)
	fmt.Printf("echo_response=%s\n", evidence.EchoResponse)
	fmt.Printf("http_status=%s\n", evidence.HTTPResponseStatus)
	fmt.Printf("rejected_destinations=%d malformed_handshake_rejections=%d final_success=%t\n", evidence.RejectedDestinationCount, evidence.MalformedHandshakeRejectionCount, evidence.FinalSuccess)
	fmt.Println("relay-local visibility:")
	for _, view := range evidence.RelayVisibility {
		fmt.Printf("  %-7s role=%-6s previous=%-7s next=%-7s knows_client=%-5t knows_destination=%-5t full_path=%-5t destination=%s\n",
			view.RelayID, view.Role, view.PreviousHop, view.NextHop, view.KnowsClientConnection, view.KnowsFinalDestination, view.KnowsFullPath, view.FinalDestination)
	}
	fmt.Printf("evidence_json=%s\n", evidence.EvidenceJSONPath)
	fmt.Printf("evidence_text=%s\n", evidence.EvidenceTextPath)
}
