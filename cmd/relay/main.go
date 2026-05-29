package main

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	cryptosuite "github.com/keithwegner/pq-fabric/core/crypto/suite"
	relaypkg "github.com/keithwegner/pq-fabric/routing/relay"
)

type relayStatus struct {
	RelayID     string `json:"relay_id"`
	Region      string `json:"region"`
	RoutingMode string `json:"routing_mode"`
	CryptoSuite string `json:"crypto_suite"`
	PublicKeyID string `json:"public_key_id"`
	StartedAt   int64  `json:"started_at_unix_milli"`
	Boundary    string `json:"boundary"`
}

func main() {
	relayID := getenv("RELAY_ID", "relay-1")
	region := getenv("REGION", defaultRegionForRelay(relayID))
	port := getenv("PORT", "8080")
	listenAddr := getenv("LISTEN_ADDR", ":"+port)
	routingMode := getenv("ROUTING_MODE", "private-testbed")

	selected, err := cryptosuite.FromEnv()
	if err != nil {
		log.Fatal(err)
	}
	relay, err := relaypkg.NewRelayForSuite(relayID, region, selected)
	if err != nil {
		log.Fatal(err)
	}
	publicKeyDigest := sha256.Sum256(relay.PublicKey())
	status := relayStatus{
		RelayID:     relay.ID,
		Region:      relay.Region,
		RoutingMode: routingMode,
		CryptoSuite: string(selected.Name),
		PublicKeyID: fmt.Sprintf("%x", publicKeyDigest[:8]),
		StartedAt:   time.Now().UnixMilli(),
		Boundary:    "private local testbed relay; no public discovery or exit routing",
	}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /health", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{"status": "ok", "relay_id": status.RelayID})
	})
	mux.HandleFunc("GET /state", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(status)
	})

	server := &http.Server{Addr: listenAddr, Handler: mux}
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = server.Shutdown(shutdownCtx)
	}()

	log.Printf("%s started region=%s listen=%s mode=%s suite=%s", status.RelayID, status.Region, listenAddr, routingMode, status.CryptoSuite)
	if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Fatal(err)
	}
	log.Printf("%s stopped", status.RelayID)
}

func getenv(key, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(key)); value != "" {
		return value
	}
	return fallback
}

func defaultRegionForRelay(relayID string) string {
	switch relayID {
	case "relay-1", "relay-2":
		return "nyc"
	case "relay-3", "relay-4":
		return "london"
	default:
		return "singapore"
	}
}
