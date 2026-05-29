package relay

import (
	"errors"
	"fmt"
	"sort"
	"sync"

	pqcrypto "github.com/keithwegner/pq-fabric/core/crypto"
	cryptosuite "github.com/keithwegner/pq-fabric/core/crypto/suite"
	"github.com/keithwegner/pq-fabric/routing/circuit"
)

type Relay struct {
	ID      string
	Region  string
	KEM     pqcrypto.KEMPrivateKey
	Running bool

	mu              sync.Mutex
	sessions        map[string]SessionState
	seenExtensions  map[string]struct{}
	streams         map[string]map[uint64]struct{}
	exitPolicy      ExitPolicy
	lastDestination string
}

type SessionState struct {
	CircuitID     string          `json:"circuit_id"`
	RelayID       string          `json:"relay_id"`
	Role          circuit.HopRole `json:"role"`
	PreviousHop   string          `json:"previous_hop,omitempty"`
	NextHop       string          `json:"next_hop,omitempty"`
	LayerKey      []byte          `json:"-"`
	ExtensionID   string          `json:"extension_id"`
	LastStreamID  uint64          `json:"last_stream_id,omitempty"`
	LastCellType  string          `json:"last_cell_type,omitempty"`
	ForwardedCell int             `json:"forwarded_cell_count"`
}

type Visibility struct {
	RelayID                  string          `json:"relay_id"`
	Region                   string          `json:"region"`
	Role                     circuit.HopRole `json:"role"`
	PreviousHop              string          `json:"previous_hop,omitempty"`
	NextHop                  string          `json:"next_hop,omitempty"`
	KnowsClientConnection    bool            `json:"knows_client_connection"`
	KnowsFinalDestination    bool            `json:"knows_final_destination"`
	KnowsFullPath            bool            `json:"knows_full_path"`
	FinalDestination         string          `json:"final_destination,omitempty"`
	LocalSessionKeyAvailable bool            `json:"local_session_key_available"`
	LastStreamID             uint64          `json:"last_stream_id,omitempty"`
	LastCellType             string          `json:"last_cell_type,omitempty"`
}

type ExitPolicy struct {
	AllowedDestinations map[string]struct{} `json:"allowed_destinations"`
}

func NewDevelopmentRelay(id, region string) (*Relay, error) {
	return NewRelayForSuite(id, region, cryptosuite.MustLookup(string(cryptosuite.Dev)))
}

func NewRelayForSuite(id, region string, selected cryptosuite.CryptoSuite) (*Relay, error) {
	kem, err := selected.NewKEMPrivate(id)
	if err != nil {
		return nil, err
	}
	return &Relay{
		ID:             id,
		Region:         region,
		KEM:            kem,
		sessions:       make(map[string]SessionState),
		seenExtensions: make(map[string]struct{}),
		streams:        make(map[string]map[uint64]struct{}),
		exitPolicy:     NewExitPolicy(nil),
	}, nil
}

func NewExitPolicy(allowed []string) ExitPolicy {
	out := ExitPolicy{AllowedDestinations: make(map[string]struct{}, len(allowed))}
	for _, destination := range allowed {
		if destination != "" {
			out.AllowedDestinations[destination] = struct{}{}
		}
	}
	return out
}

func (p ExitPolicy) Allows(destination string) bool {
	if len(p.AllowedDestinations) == 0 {
		return false
	}
	_, ok := p.AllowedDestinations[destination]
	return ok
}

func (p ExitPolicy) Destinations() []string {
	out := make([]string, 0, len(p.AllowedDestinations))
	for destination := range p.AllowedDestinations {
		out = append(out, destination)
	}
	sort.Strings(out)
	return out
}

func (r *Relay) PublicKey() []byte { return r.KEM.PublicKey() }

func (r *Relay) Start() { r.Running = true }

func (r *Relay) Stop() { r.Running = false }

func (r *Relay) SetExitPolicy(policy ExitPolicy) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.exitPolicy = policy
}

func (r *Relay) AllowsDestination(destination string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.exitPolicy.Allows(destination)
}

func (r *Relay) AcceptExtension(extension circuit.ExtensionMessage, previousHop, nextHop string) (SessionState, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if !r.Running {
		return SessionState{}, fmt.Errorf("%s is not running", r.ID)
	}
	if extension.RelayID != r.ID {
		return SessionState{}, fmt.Errorf("extension relay id %s does not match %s", extension.RelayID, r.ID)
	}
	extensionID := extension.ID()
	if extensionID == "" {
		return SessionState{}, errors.New("extension id could not be derived")
	}
	if _, ok := r.seenExtensions[extensionID]; ok {
		return SessionState{}, errors.New("replayed circuit extension rejected")
	}
	layerKey, err := circuit.VerifyExtension(extension, r.KEM)
	if err != nil {
		return SessionState{}, err
	}
	state := SessionState{
		CircuitID:   extension.CircuitID,
		RelayID:     r.ID,
		Role:        extension.Role,
		PreviousHop: previousHop,
		NextHop:     nextHop,
		LayerKey:    layerKey,
		ExtensionID: extensionID,
	}
	r.sessions[extension.CircuitID] = state
	r.seenExtensions[extensionID] = struct{}{}
	return state, nil
}

func (r *Relay) ProcessCell(encoded []byte) ([]byte, circuit.Cell, error) {
	meta, err := circuit.DecodeCellMetadata(encoded)
	if err != nil {
		return nil, circuit.Cell{}, err
	}
	r.mu.Lock()
	state, ok := r.sessions[meta.CircuitID]
	r.mu.Unlock()
	if !ok {
		return nil, circuit.Cell{}, fmt.Errorf("unknown circuit id %s at relay %s", meta.CircuitID, r.ID)
	}
	if meta.RelayID != r.ID {
		return nil, circuit.Cell{}, fmt.Errorf("cell for relay %s reached relay %s", meta.RelayID, r.ID)
	}
	cell, plaintext, err := circuit.UnwrapLayer(state.LayerKey, encoded)
	if err != nil {
		return nil, circuit.Cell{}, err
	}
	r.mu.Lock()
	state.LastStreamID = cell.StreamID
	state.LastCellType = cell.PayloadType
	state.ForwardedCell++
	r.sessions[meta.CircuitID] = state
	if r.streams[meta.CircuitID] == nil {
		r.streams[meta.CircuitID] = make(map[uint64]struct{})
	}
	r.streams[meta.CircuitID][cell.StreamID] = struct{}{}
	r.mu.Unlock()
	return plaintext, cell, nil
}

func (r *Relay) RecordExitDestination(circuitID, destination string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	state, ok := r.sessions[circuitID]
	if !ok {
		return fmt.Errorf("unknown circuit id %s at relay %s", circuitID, r.ID)
	}
	if state.Role != circuit.Exit {
		return fmt.Errorf("relay %s is not an exit relay for circuit %s", r.ID, circuitID)
	}
	if !r.exitPolicy.Allows(destination) {
		return fmt.Errorf("destination %s is not allowed by exit policy", destination)
	}
	r.lastDestination = destination
	return nil
}

func (r *Relay) Session(circuitID string) (SessionState, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	state, ok := r.sessions[circuitID]
	return state, ok
}

func (r *Relay) Visibility(circuitID string) Visibility {
	r.mu.Lock()
	defer r.mu.Unlock()
	state := r.sessions[circuitID]
	view := Visibility{
		RelayID:                  r.ID,
		Region:                   r.Region,
		Role:                     state.Role,
		PreviousHop:              state.PreviousHop,
		NextHop:                  state.NextHop,
		LocalSessionKeyAvailable: len(state.LayerKey) > 0,
		LastStreamID:             state.LastStreamID,
		LastCellType:             state.LastCellType,
	}
	switch state.Role {
	case circuit.Entry:
		view.KnowsClientConnection = state.PreviousHop != ""
	case circuit.Exit:
		view.KnowsFinalDestination = r.lastDestination != ""
		view.FinalDestination = r.lastDestination
	}
	return view
}

func (r *Relay) Teardown(circuitID string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.sessions, circuitID)
	delete(r.streams, circuitID)
	if len(r.sessions) == 0 {
		r.lastDestination = ""
	}
}
