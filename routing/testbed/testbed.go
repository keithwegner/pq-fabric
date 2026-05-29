package testbed

import (
	"bufio"
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	cryptosuite "github.com/keithwegner/pq-fabric/core/crypto/suite"
	"github.com/keithwegner/pq-fabric/routing/circuit"
	"github.com/keithwegner/pq-fabric/routing/relay"
	"github.com/keithwegner/pq-fabric/routing/socks5"
	"github.com/keithwegner/pq-fabric/routing/stream"
)

const (
	DestinationEcho = "local-echo:7000"
	DestinationHTTP = "local-http:8080"
)

type Options struct {
	EvidenceJSONPath string
	EvidenceTextPath string
	WriteArtifacts   bool
}

type Evidence struct {
	GeneratedAtUnixMilli             int64              `json:"generated_at_unix_milli"`
	CircuitID                        string             `json:"circuit_id"`
	EntryRelay                       string             `json:"entry_relay"`
	MiddleRelay                      string             `json:"middle_relay"`
	ExitRelay                        string             `json:"exit_relay"`
	CryptoSuite                      string             `json:"crypto_suite"`
	HandshakeSuccess                 bool               `json:"handshake_success"`
	StreamsOpened                    int                `json:"streams_opened"`
	StreamsCompleted                 int                `json:"streams_completed"`
	RejectedDestinationCount         int                `json:"rejected_destination_count"`
	MalformedHandshakeRejectionCount int                `json:"malformed_handshake_rejection_count"`
	FinalSuccess                     bool               `json:"final_success"`
	SOCKS5RoundTrip                  bool               `json:"socks5_round_trip"`
	EchoResponse                     string             `json:"echo_response,omitempty"`
	HTTPResponseStatus               string             `json:"http_response_status,omitempty"`
	RelayVisibility                  []relay.Visibility `json:"relay_visibility"`
	Limitations                      string             `json:"limitations"`
	EvidenceJSONPath                 string             `json:"evidence_json_path,omitempty"`
	EvidenceTextPath                 string             `json:"evidence_text_path,omitempty"`
}

type ServiceRequest struct {
	ExitRelayID string
	Destination string
	Payload     []byte
}

type Service func(ServiceRequest) ([]byte, error)

type Topology struct {
	Suite  cryptosuite.CryptoSuite
	Relays map[string]*relay.Relay
}

type Runtime struct {
	CircuitID string
	Path      []string
	Relays    map[string]*relay.Relay
	Layers    []circuit.LayerKey
	Streams   *stream.Manager
	Services  map[string]Service
	Opened    int
	Completed int
}

type exitPayload struct {
	Destination string `json:"destination"`
	Body        []byte `json:"body"`
}

func RunScenario(ctx context.Context, opts Options) (Evidence, error) {
	if opts.EvidenceJSONPath == "" {
		opts.EvidenceJSONPath = filepath.Join("tmp", "routing-evidence.json")
	}
	if opts.EvidenceTextPath == "" {
		opts.EvidenceTextPath = filepath.Join("tmp", "routing-evidence.txt")
	}
	topology, err := NewSevenRelayTopologyFromEnv()
	if err != nil {
		return Evidence{}, err
	}
	path := []string{"relay-1", "relay-4", "relay-7"}
	runtime, err := topology.BuildCircuit(path)
	if err != nil {
		return Evidence{}, err
	}
	malformedRejections := 0
	malformed := circuit.ExtensionMessage{CircuitID: runtime.CircuitID, RelayID: "relay-1", Role: circuit.Entry, Ciphertext: []byte("malformed"), Proof: []byte("bad")}
	if _, err := topology.Relays["relay-1"].AcceptExtension(malformed, "client", "relay-4"); err != nil {
		malformedRejections++
	}

	echoResponse, err := runtime.Send(ctx, DestinationEcho, []byte("hello over onion"))
	if err != nil {
		return Evidence{}, err
	}
	httpResponse, err := runtime.Send(ctx, DestinationHTTP, []byte("GET /region HTTP/1.1\r\nHost: local-http\r\n\r\n"))
	if err != nil {
		return Evidence{}, err
	}
	status := parseHTTPStatus(httpResponse)
	var rejected int
	if _, err := runtime.Send(ctx, "example.com:80", []byte("GET / HTTP/1.1\r\n\r\n")); err != nil {
		rejected++
	}
	if _, err := runtime.Send(ctx, DestinationEcho, []byte("second stream")); err != nil {
		return Evidence{}, err
	}
	socksOK, err := runtime.SOCKS5RoundTrip(ctx, DestinationEcho, []byte("via socks5"))
	if err != nil {
		return Evidence{}, err
	}
	visibility := runtime.Visibility()
	evidence := Evidence{
		GeneratedAtUnixMilli:             time.Now().UnixMilli(),
		CircuitID:                        runtime.CircuitID,
		EntryRelay:                       path[0],
		MiddleRelay:                      path[1],
		ExitRelay:                        path[2],
		CryptoSuite:                      string(topology.Suite.Name),
		HandshakeSuccess:                 true,
		StreamsOpened:                    runtime.Opened,
		StreamsCompleted:                 runtime.Completed,
		RejectedDestinationCount:         rejected,
		MalformedHandshakeRejectionCount: malformedRejections,
		FinalSuccess:                     socksOK && rejected == 1 && malformedRejections == 1,
		SOCKS5RoundTrip:                  socksOK,
		EchoResponse:                     string(echoResponse),
		HTTPResponseStatus:               status,
		RelayVisibility:                  visibility,
		Limitations:                      "private local testbed only; no public relay discovery, public exits, production anonymity, censorship resistance, FIPS certification, ACVTS validation, or production post-quantum security claims",
	}
	if opts.WriteArtifacts {
		if err := writeArtifacts(opts, evidence); err != nil {
			return Evidence{}, err
		}
		evidence.EvidenceJSONPath = opts.EvidenceJSONPath
		evidence.EvidenceTextPath = opts.EvidenceTextPath
	}
	return evidence, nil
}

func NewSevenRelayTopologyFromEnv() (*Topology, error) {
	selected, err := cryptosuite.FromEnv()
	if err != nil {
		return nil, err
	}
	return NewSevenRelayTopology(selected)
}

func NewSevenRelayTopology(selected cryptosuite.CryptoSuite) (*Topology, error) {
	relays := make(map[string]*relay.Relay, 7)
	for i := 1; i <= 7; i++ {
		id := fmt.Sprintf("relay-%d", i)
		r, err := relay.NewRelayForSuite(id, regionForRelay(id), selected)
		if err != nil {
			return nil, err
		}
		r.Start()
		if id == "relay-7" {
			r.SetExitPolicy(relay.NewExitPolicy([]string{DestinationEcho, DestinationHTTP}))
		}
		relays[id] = r
	}
	return &Topology{Suite: selected, Relays: relays}, nil
}

func (t *Topology) BuildCircuit(path []string) (*Runtime, error) {
	if len(path) != 3 {
		return nil, fmt.Errorf("testbed circuit requires exactly 3 relays, got %d", len(path))
	}
	circuitID := circuit.NewCircuitID(path)
	roles := []circuit.HopRole{circuit.Entry, circuit.Middle, circuit.Exit}
	previous := []string{"client", path[0], path[1]}
	next := []string{path[1], path[2], ""}
	layers := make([]circuit.LayerKey, 0, 3)
	for i, relayID := range path {
		r, ok := t.Relays[relayID]
		if !ok {
			return nil, fmt.Errorf("unknown relay %s", relayID)
		}
		extension, layerKey, err := circuit.CreateExtension(circuitID, relayID, roles[i], r.PublicKey(), t.Suite.NewKEMPublic)
		if err != nil {
			return nil, err
		}
		if _, err := r.AcceptExtension(extension, previous[i], next[i]); err != nil {
			return nil, err
		}
		layers = append(layers, circuit.LayerKey{RelayID: relayID, Key: layerKey})
	}
	return &Runtime{
		CircuitID: circuitID,
		Path:      append([]string(nil), path...),
		Relays:    t.Relays,
		Layers:    []circuit.LayerKey{layers[2], layers[1], layers[0]},
		Streams:   stream.NewManager(),
		Services:  defaultServices(),
	}, nil
}

func (r *Runtime) Send(ctx context.Context, destination string, payload []byte) ([]byte, error) {
	streamID, err := r.Streams.Open()
	if err != nil {
		return nil, err
	}
	r.Opened++
	defer func() {
		_ = r.Streams.Close(streamID)
	}()
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	default:
	}
	payloadJSON, err := json.Marshal(exitPayload{Destination: destination, Body: payload})
	if err != nil {
		return nil, err
	}
	encoded, err := circuit.WrapLayers(r.Layers, r.CircuitID, streamID, "data", uint64(r.Opened), payloadJSON)
	if err != nil {
		return nil, err
	}
	for _, relayID := range r.Path {
		encoded, _, err = r.Relays[relayID].ProcessCell(encoded)
		if err != nil {
			return nil, err
		}
	}
	var request exitPayload
	if err := json.Unmarshal(encoded, &request); err != nil {
		return nil, err
	}
	exitRelay := r.Relays[r.Path[2]]
	if err := exitRelay.RecordExitDestination(r.CircuitID, request.Destination); err != nil {
		return nil, err
	}
	service, ok := r.Services[request.Destination]
	if !ok {
		return nil, fmt.Errorf("no local test service registered for %s", request.Destination)
	}
	response, err := service(ServiceRequest{ExitRelayID: exitRelay.ID, Destination: request.Destination, Payload: request.Body})
	if err != nil {
		return nil, err
	}
	wrappedResponse, err := circuit.WrapLayers(r.Layers, r.CircuitID, streamID, "response", uint64(r.Opened), response)
	if err != nil {
		return nil, err
	}
	plaintext, err := circuit.UnwrapLayers(r.Layers, wrappedResponse)
	if err != nil {
		return nil, err
	}
	r.Completed++
	return plaintext, nil
}

func (r *Runtime) DialThroughCircuit(ctx context.Context, destination string, payload []byte) ([]byte, error) {
	return r.Send(ctx, destination, payload)
}

func (r *Runtime) SOCKS5RoundTrip(ctx context.Context, destination string, payload []byte) (bool, error) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return false, err
	}
	defer listener.Close()
	proxy := socks5.Proxy{
		Dialer: r,
		Allow: func(destination string) bool {
			return r.Relays[r.Path[2]].AllowsDestination(destination)
		},
	}
	errCh := make(chan error, 1)
	go func() {
		conn, err := listener.Accept()
		if err != nil {
			errCh <- err
			return
		}
		errCh <- proxy.ServeConn(ctx, conn)
	}()
	conn, err := net.Dial("tcp", listener.Addr().String())
	if err != nil {
		return false, err
	}
	tcpConn := conn.(*net.TCPConn)
	if err := writeSOCKS5ClientRequest(tcpConn, destination); err != nil {
		_ = tcpConn.Close()
		return false, err
	}
	if _, err := tcpConn.Write(payload); err != nil {
		_ = tcpConn.Close()
		return false, err
	}
	_ = tcpConn.CloseWrite()
	response, err := io.ReadAll(tcpConn)
	_ = tcpConn.Close()
	if err != nil {
		return false, err
	}
	if err := <-errCh; err != nil {
		return false, err
	}
	return strings.Contains(string(response), "via socks5"), nil
}

func (r *Runtime) Visibility() []relay.Visibility {
	views := make([]relay.Visibility, 0, len(r.Path))
	for _, relayID := range r.Path {
		views = append(views, r.Relays[relayID].Visibility(r.CircuitID))
	}
	return views
}

func (r *Runtime) Teardown() {
	for _, relayID := range r.Path {
		r.Relays[relayID].Teardown(r.CircuitID)
	}
	r.Streams.CloseAll()
}

func defaultServices() map[string]Service {
	return map[string]Service{
		DestinationEcho: func(req ServiceRequest) ([]byte, error) {
			return []byte(fmt.Sprintf("echo from %s: %s", req.ExitRelayID, string(req.Payload))), nil
		},
		DestinationHTTP: func(req ServiceRequest) ([]byte, error) {
			body := fmt.Sprintf("region-test exit=%s destination=%s\n", req.ExitRelayID, req.Destination)
			return []byte(fmt.Sprintf("HTTP/1.1 200 OK\r\nX-Exit-Relay: %s\r\nContent-Length: %d\r\n\r\n%s", req.ExitRelayID, len(body), body)), nil
		},
	}
}

func writeSOCKS5ClientRequest(conn *net.TCPConn, destination string) error {
	if _, err := conn.Write([]byte{0x05, 0x01, 0x00}); err != nil {
		return err
	}
	greeting := make([]byte, 2)
	if _, err := io.ReadFull(conn, greeting); err != nil {
		return err
	}
	if greeting[0] != 0x05 || greeting[1] != 0x00 {
		return errors.New("SOCKS5 no-auth negotiation failed")
	}
	host, portRaw, err := net.SplitHostPort(destination)
	if err != nil {
		return err
	}
	port := uint64(0)
	if _, err := fmt.Sscanf(portRaw, "%d", &port); err != nil {
		return err
	}
	if len(host) > 255 {
		return errors.New("SOCKS5 host too long")
	}
	request := []byte{0x05, 0x01, 0x00, 0x03, byte(len(host))}
	request = append(request, []byte(host)...)
	var portBytes [2]byte
	binary.BigEndian.PutUint16(portBytes[:], uint16(port))
	request = append(request, portBytes[:]...)
	if _, err := conn.Write(request); err != nil {
		return err
	}
	reply := make([]byte, 10)
	if _, err := io.ReadFull(conn, reply); err != nil {
		return err
	}
	if reply[1] != 0x00 {
		return fmt.Errorf("SOCKS5 connect rejected with status 0x%02x", reply[1])
	}
	return nil
}

func parseHTTPStatus(response []byte) string {
	reader := bufio.NewReader(strings.NewReader(string(response)))
	line, err := reader.ReadString('\n')
	if err != nil && len(line) == 0 {
		return ""
	}
	return strings.TrimSpace(line)
}

func regionForRelay(id string) string {
	switch id {
	case "relay-1", "relay-2":
		return "nyc"
	case "relay-3", "relay-4":
		return "london"
	case "relay-5", "relay-6", "relay-7":
		return "singapore"
	default:
		return "unknown"
	}
}

func writeArtifacts(opts Options, evidence Evidence) error {
	if err := os.MkdirAll(filepath.Dir(opts.EvidenceJSONPath), 0o755); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(opts.EvidenceTextPath), 0o755); err != nil {
		return err
	}
	jsonData, err := json.MarshalIndent(evidence, "", "  ")
	if err != nil {
		return err
	}
	jsonData = append(jsonData, '\n')
	if err := os.WriteFile(opts.EvidenceJSONPath, jsonData, 0o644); err != nil {
		return err
	}
	return os.WriteFile(opts.EvidenceTextPath, []byte(renderText(evidence)), 0o644)
}

func renderText(e Evidence) string {
	var b strings.Builder
	fmt.Fprintf(&b, "pq-fabric private routing testbed evidence\n")
	fmt.Fprintf(&b, "circuit_id=%s suite=%s entry=%s middle=%s exit=%s\n", e.CircuitID, e.CryptoSuite, e.EntryRelay, e.MiddleRelay, e.ExitRelay)
	fmt.Fprintf(&b, "handshake_success=%t streams_opened=%d streams_completed=%d socks5_round_trip=%t\n", e.HandshakeSuccess, e.StreamsOpened, e.StreamsCompleted, e.SOCKS5RoundTrip)
	fmt.Fprintf(&b, "rejected_destinations=%d malformed_handshake_rejections=%d final_success=%t\n", e.RejectedDestinationCount, e.MalformedHandshakeRejectionCount, e.FinalSuccess)
	fmt.Fprintf(&b, "echo_response=%s\n", e.EchoResponse)
	fmt.Fprintf(&b, "http_response_status=%s\n", e.HTTPResponseStatus)
	fmt.Fprintf(&b, "relay_visibility:\n")
	views := append([]relay.Visibility(nil), e.RelayVisibility...)
	sort.SliceStable(views, func(i, j int) bool { return views[i].RelayID < views[j].RelayID })
	for _, view := range views {
		fmt.Fprintf(&b, "  relay=%s role=%s previous=%s next=%s knows_client=%t knows_destination=%t full_path=%t destination=%s\n", view.RelayID, view.Role, view.PreviousHop, view.NextHop, view.KnowsClientConnection, view.KnowsFinalDestination, view.KnowsFullPath, view.FinalDestination)
	}
	fmt.Fprintf(&b, "limitations=%s\n", e.Limitations)
	return b.String()
}
