package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"sort"
	"time"

	"github.com/keithwegner/pq-fabric/bundle/retransmit"
	"github.com/keithwegner/pq-fabric/consensus/protocol"
	"github.com/keithwegner/pq-fabric/consensus/validator"
	"github.com/keithwegner/pq-fabric/core/identity"
)

func main() {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	ports := map[string]int{
		"validator-1": 9101,
		"validator-2": 9102,
		"validator-3": 9103,
		"validator-4": 9104,
		"validator-5": 9105,
		"validator-6": 9106,
		"validator-7": 9107,
	}
	urls := make(map[string]string, len(ports))
	for id, port := range ports {
		urls[id] = fmt.Sprintf("http://127.0.0.1:%d", port)
	}

	nodes := make(map[string]*validator.Node)
	for _, id := range identity.DefaultValidatorIDs() {
		cfg := validator.Config{
			ID:                id,
			Region:            identity.DefaultRegionFor(id),
			ListenAddr:        fmt.Sprintf("127.0.0.1:%d", ports[id]),
			PublicURL:         urls[id],
			PeerURLs:          urls,
			Threshold:         5,
			RequestTimeout:    450 * time.Millisecond,
			EnableHealthProbe: true,
		}
		node, err := validator.NewNode(cfg)
		if err != nil {
			log.Fatal(err)
		}
		nodes[id] = node
	}

	fmt.Println("pq-fabric local demo")
	fmt.Println("starting 7 validators across nyc/london/singapore labels...")
	startAll(ctx, nodes)
	waitForHTTP(urls["validator-1"] + "/health")
	waitForHTTP(urls["validator-7"] + "/health")
	time.Sleep(800 * time.Millisecond)

	leader := nodes["validator-1"]
	commit1, err := leader.Propose(ctx, "genesis: establish local 7-validator pq-fabric quorum")
	must(err)
	fmt.Printf("commit 1: height=%d round=%d votes=%d/%d proposer=%s state=%s\n", commit1.Block.Height, commit1.Block.Round, len(commit1.Certificate.Votes), 7, commit1.Block.ProposerID, shortHash(commit1.Block.StateDigest))
	printHeights(nodes)

	fmt.Println("\nsimulating controlled failure: stopping validator-6 and validator-7")
	must(nodes["validator-6"].Stop(context.Background()))
	must(nodes["validator-7"].Stop(context.Background()))
	time.Sleep(900 * time.Millisecond)

	leader = nodes[mustProposer(commit1.Block.Height+1, 0)]
	commit2, err := leader.Propose(ctx, "failure-mode: commit while two singapore validators are offline")
	must(err)
	fmt.Printf("commit 2 under failure: height=%d round=%d votes=%d/%d threshold=%d proposer=%s state=%s\n", commit2.Block.Height, commit2.Block.Round, len(commit2.Certificate.Votes), 7, commit2.Certificate.Threshold, commit2.Block.ProposerID, shortHash(commit2.Block.StateDigest))
	printHeights(nodes)

	fmt.Println("\nremediating: restarting validator-6 and validator-7, then catch-up from peers")
	must(nodes["validator-6"].Start(ctx))
	must(nodes["validator-7"].Start(ctx))
	waitForHTTP(urls["validator-6"] + "/health")
	waitForHTTP(urls["validator-7"] + "/health")
	must(nodes["validator-6"].CatchUpFromPeers(ctx))
	must(nodes["validator-7"].CatchUpFromPeers(ctx))
	time.Sleep(900 * time.Millisecond)
	printHeights(nodes)

	fmt.Println("\nidempotent retransmission demo")
	ledger := retransmit.NewLedger()
	tx := retransmit.Transaction{ID: "payment-0001", Sequence: 1, Payload: "settle credential issuance fee"}
	first := ledger.Apply(tx)
	second := ledger.Apply(tx)
	fmt.Printf("transaction=%s first_apply=%t retransmit_apply=%t ledger_count=%d\n", tx.ID, first.Applied, second.Applied, ledger.Count())

	fmt.Printf("\npeer health observed by %s\n", leader.ID())
	for _, h := range leader.PeerHealth() {
		fmt.Printf("  %-12s healthy=%-5t height=%d last_error=%s\n", h.PeerID, h.Healthy, h.LastHeight, h.LastError)
	}

	fmt.Println("\ndemo complete: 5-of-7 quorum committed during a 2-node failure; remediated nodes caught up deterministically.")
}

func startAll(ctx context.Context, nodes map[string]*validator.Node) {
	ids := make([]string, 0, len(nodes))
	for id := range nodes {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, id := range ids {
		must(nodes[id].Start(ctx))
	}
}

func printHeights(nodes map[string]*validator.Node) {
	ids := make([]string, 0, len(nodes))
	for id := range nodes {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	fmt.Println("validator state:")
	for _, id := range ids {
		s := nodes[id].Snapshot()
		fmt.Printf("  %-12s region=%-9s running=%-5t height=%d round=%d hash=%s state=%s\n", s.NodeID, s.Region, s.Running, s.Height, s.Round, shortHash(s.LastHash), shortHash(s.StateDigest))
	}
}

func mustProposer(height, round uint64) string {
	proposer, err := protocol.ProposerFor(height, round, identity.DefaultValidatorIDs())
	must(err)
	return proposer
}

func waitForHTTP(url string) {
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		resp, err := http.Get(url)
		if err == nil {
			_ = resp.Body.Close()
			if resp.StatusCode < 500 {
				return
			}
		}
		time.Sleep(80 * time.Millisecond)
	}
	log.Fatalf("timed out waiting for %s", url)
}

func shortHash(h string) string {
	if len(h) <= 12 {
		return h
	}
	return h[:12]
}

func must(err error) {
	if err != nil {
		log.Fatal(err)
	}
}
