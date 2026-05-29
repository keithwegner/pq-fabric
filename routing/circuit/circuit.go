package circuit

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	pqcrypto "github.com/keithwegner/pq-fabric/core/crypto"
	cryptosuite "github.com/keithwegner/pq-fabric/core/crypto/suite"
	"github.com/keithwegner/pq-fabric/core/messages"
)

type HopRole string

const (
	Entry  HopRole = "entry"
	Middle HopRole = "middle"
	Exit   HopRole = "exit"
)

type Hop struct {
	RelayID    string  `json:"relay_id"`
	Role       HopRole `json:"role"`
	Ciphertext []byte  `json:"ciphertext"`
	SecretHash string  `json:"secret_hash"`
}

type Circuit struct {
	ID   string `json:"id"`
	Hops []Hop  `json:"hops"`
}

type ExtensionMessage struct {
	CircuitID  string  `json:"circuit_id"`
	RelayID    string  `json:"relay_id"`
	Role       HopRole `json:"role"`
	Nonce      string  `json:"nonce"`
	Ciphertext []byte  `json:"ciphertext"`
	Proof      []byte  `json:"proof"`
}

type LayerKey struct {
	RelayID string `json:"relay_id"`
	Key     []byte `json:"-"`
}

type Cell struct {
	CircuitID   string `json:"circuit_id"`
	StreamID    uint64 `json:"stream_id"`
	PayloadType string `json:"payload_type"`
	Sequence    uint64 `json:"sequence"`
	RelayID     string `json:"relay_id"`
	Nonce       []byte `json:"nonce"`
	Ciphertext  []byte `json:"ciphertext"`
	Tag         []byte `json:"tag"`
}

// BuildPrivateTestbedCircuit models telescoping circuit construction through
// the configured crypto suite. It does not open a public proxy or exit to the
// Internet; it is a protocol-level artifact for the private local testbed.
func BuildPrivateTestbedCircuit(relayIDs []string, relayPublicKeys map[string][]byte) (Circuit, error) {
	selected, err := cryptosuite.FromEnv()
	if err != nil {
		return Circuit{}, err
	}
	return BuildPrivateTestbedCircuitWithKEM(relayIDs, relayPublicKeys, selected.NewKEMPublic)
}

func BuildPrivateTestbedCircuitWithKEM(relayIDs []string, relayPublicKeys map[string][]byte, newPublic func([]byte) (pqcrypto.KEMPublicKey, error)) (Circuit, error) {
	if len(relayIDs) != 3 {
		return Circuit{}, fmt.Errorf("private testbed circuit requires exactly 3 hops, got %d", len(relayIDs))
	}
	circuitID := NewCircuitID(relayIDs)
	roles := []HopRole{Entry, Middle, Exit}
	hops := make([]Hop, 0, 3)
	for i, relayID := range relayIDs {
		publicBytes, ok := relayPublicKeys[relayID]
		if !ok {
			return Circuit{}, fmt.Errorf("missing relay public key for %s", relayID)
		}
		extension, layerKey, err := CreateExtension(circuitID, relayID, roles[i], publicBytes, newPublic)
		if err != nil {
			return Circuit{}, err
		}
		secretHash := sha256.Sum256(layerKey)
		hops = append(hops, Hop{RelayID: relayID, Role: roles[i], Ciphertext: extension.Ciphertext, SecretHash: hex.EncodeToString(secretHash[:])})
	}
	return Circuit{ID: circuitID, Hops: hops}, nil
}

func NewCircuitID(relayIDs []string) string {
	hash := sha256.New()
	_, _ = hash.Write([]byte("pq-fabric/routing/circuit-id/v1"))
	for _, id := range relayIDs {
		_, _ = hash.Write([]byte{0})
		_, _ = hash.Write([]byte(id))
	}
	return hex.EncodeToString(hash.Sum(nil))[:24]
}

func CreateExtension(circuitID, relayID string, role HopRole, relayPublicKey []byte, newPublic func([]byte) (pqcrypto.KEMPublicKey, error)) (ExtensionMessage, []byte, error) {
	if strings.TrimSpace(circuitID) == "" {
		return ExtensionMessage{}, nil, errors.New("circuit id is required")
	}
	if strings.TrimSpace(relayID) == "" {
		return ExtensionMessage{}, nil, errors.New("relay id is required")
	}
	if !validRole(role) {
		return ExtensionMessage{}, nil, fmt.Errorf("unsupported hop role: %s", role)
	}
	if newPublic == nil {
		return ExtensionMessage{}, nil, errors.New("KEM public key parser is required")
	}
	publicKey, err := newPublic(relayPublicKey)
	if err != nil {
		return ExtensionMessage{}, nil, err
	}
	ciphertext, sharedSecret, err := publicKey.Encapsulate()
	if err != nil {
		return ExtensionMessage{}, nil, err
	}
	extension := ExtensionMessage{
		CircuitID:  circuitID,
		RelayID:    relayID,
		Role:       role,
		Nonce:      extensionNonce(circuitID, relayID, role, ciphertext),
		Ciphertext: ciphertext,
	}
	extension.Proof, err = extensionProof(sharedSecret, extension)
	if err != nil {
		return ExtensionMessage{}, nil, err
	}
	return extension, DeriveLayerKey(sharedSecret, circuitID, relayID, role), nil
}

func VerifyExtension(extension ExtensionMessage, privateKey pqcrypto.KEMPrivateKey) ([]byte, error) {
	if privateKey == nil {
		return nil, errors.New("relay KEM private key is required")
	}
	if strings.TrimSpace(extension.CircuitID) == "" {
		return nil, errors.New("extension circuit id is required")
	}
	if strings.TrimSpace(extension.RelayID) == "" {
		return nil, errors.New("extension relay id is required")
	}
	if !validRole(extension.Role) {
		return nil, fmt.Errorf("unsupported extension role: %s", extension.Role)
	}
	if len(extension.Ciphertext) == 0 {
		return nil, errors.New("extension ciphertext is required")
	}
	if len(extension.Proof) == 0 {
		return nil, errors.New("extension proof is required")
	}
	sharedSecret, err := privateKey.Decapsulate(extension.Ciphertext)
	if err != nil {
		return nil, err
	}
	expected, err := extensionProof(sharedSecret, extension)
	if err != nil {
		return nil, err
	}
	if !hmac.Equal(expected, extension.Proof) {
		return nil, errors.New("extension proof verification failed")
	}
	return DeriveLayerKey(sharedSecret, extension.CircuitID, extension.RelayID, extension.Role), nil
}

func (e ExtensionMessage) ID() string {
	hash, err := messages.HashCanonical(extensionProofPayload(e))
	if err != nil {
		return ""
	}
	return hash
}

func DeriveLayerKey(sharedSecret []byte, circuitID, relayID string, role HopRole) []byte {
	hash := sha256.New()
	_, _ = hash.Write([]byte("pq-fabric/routing/layer-key/v1"))
	_, _ = hash.Write([]byte{0})
	_, _ = hash.Write(sharedSecret)
	_, _ = hash.Write([]byte{0})
	_, _ = hash.Write([]byte(circuitID))
	_, _ = hash.Write([]byte{0})
	_, _ = hash.Write([]byte(relayID))
	_, _ = hash.Write([]byte{0})
	_, _ = hash.Write([]byte(role))
	return hash.Sum(nil)
}

func WrapLayers(layers []LayerKey, circuitID string, streamID uint64, payloadType string, sequence uint64, plaintext []byte) ([]byte, error) {
	if len(layers) == 0 {
		return nil, errors.New("at least one layer is required")
	}
	out := append([]byte(nil), plaintext...)
	for _, layer := range layers {
		encoded, err := WrapLayer(layer, circuitID, streamID, payloadType, sequence, out)
		if err != nil {
			return nil, err
		}
		out = encoded
	}
	return out, nil
}

func UnwrapLayers(layers []LayerKey, encoded []byte) ([]byte, error) {
	if len(layers) == 0 {
		return nil, errors.New("at least one layer is required")
	}
	out := append([]byte(nil), encoded...)
	for i := len(layers) - 1; i >= 0; i-- {
		_, plaintext, err := UnwrapLayer(layers[i].Key, out)
		if err != nil {
			return nil, err
		}
		out = plaintext
	}
	return out, nil
}

func WrapLayer(layer LayerKey, circuitID string, streamID uint64, payloadType string, sequence uint64, plaintext []byte) ([]byte, error) {
	if len(layer.Key) == 0 {
		return nil, errors.New("layer key is required")
	}
	aead, err := newAEAD(layer.Key)
	if err != nil {
		return nil, err
	}
	cell := Cell{
		CircuitID:   circuitID,
		StreamID:    streamID,
		PayloadType: payloadType,
		Sequence:    sequence,
		RelayID:     layer.RelayID,
		Nonce:       layerNonce(layer, circuitID, streamID, payloadType, sequence),
	}
	sealed := aead.Seal(nil, cell.Nonce, plaintext, cellAAD(cell))
	tagSize := aead.Overhead()
	cell.Ciphertext = append([]byte(nil), sealed[:len(sealed)-tagSize]...)
	cell.Tag = append([]byte(nil), sealed[len(sealed)-tagSize:]...)
	return json.Marshal(cell)
}

func UnwrapLayer(key []byte, encoded []byte) (Cell, []byte, error) {
	if len(key) == 0 {
		return Cell{}, nil, errors.New("layer key is required")
	}
	var cell Cell
	if err := json.Unmarshal(encoded, &cell); err != nil {
		return Cell{}, nil, fmt.Errorf("parse onion cell: %w", err)
	}
	if strings.TrimSpace(cell.CircuitID) == "" {
		return Cell{}, nil, errors.New("cell circuit id is required")
	}
	if len(cell.Nonce) == 0 || len(cell.Ciphertext) == 0 || len(cell.Tag) == 0 {
		return Cell{}, nil, errors.New("cell encryption material is incomplete")
	}
	aead, err := newAEAD(key)
	if err != nil {
		return Cell{}, nil, err
	}
	sealed := append(append([]byte(nil), cell.Ciphertext...), cell.Tag...)
	plaintext, err := aead.Open(nil, cell.Nonce, sealed, cellAAD(cell))
	if err != nil {
		return Cell{}, nil, err
	}
	return cell, plaintext, nil
}

func DecodeCellMetadata(encoded []byte) (Cell, error) {
	var cell Cell
	if err := json.Unmarshal(encoded, &cell); err != nil {
		return Cell{}, err
	}
	return cell, nil
}

func newAEAD(keyMaterial []byte) (cipher.AEAD, error) {
	key := sha256.Sum256(append([]byte("pq-fabric/routing/aead/v1\x00"), keyMaterial...))
	block, err := aes.NewCipher(key[:])
	if err != nil {
		return nil, err
	}
	return cipher.NewGCM(block)
}

func cellAAD(cell Cell) []byte {
	return []byte(fmt.Sprintf("%s|%d|%s|%d|%s", cell.CircuitID, cell.StreamID, cell.PayloadType, cell.Sequence, cell.RelayID))
}

func layerNonce(layer LayerKey, circuitID string, streamID uint64, payloadType string, sequence uint64) []byte {
	hash := sha256.New()
	_, _ = hash.Write([]byte("pq-fabric/routing/nonce/v1"))
	_, _ = hash.Write([]byte{0})
	_, _ = hash.Write(layer.Key)
	_, _ = hash.Write([]byte{0})
	_, _ = hash.Write([]byte(layer.RelayID))
	_, _ = hash.Write([]byte{0})
	_, _ = hash.Write([]byte(circuitID))
	_, _ = hash.Write([]byte(fmt.Sprintf("|%d|%s|%d", streamID, payloadType, sequence)))
	return hash.Sum(nil)[:12]
}

func extensionNonce(circuitID, relayID string, role HopRole, ciphertext []byte) string {
	hash := sha256.New()
	_, _ = hash.Write([]byte("pq-fabric/routing/extension-nonce/v1"))
	_, _ = hash.Write([]byte{0})
	_, _ = hash.Write([]byte(circuitID))
	_, _ = hash.Write([]byte{0})
	_, _ = hash.Write([]byte(relayID))
	_, _ = hash.Write([]byte{0})
	_, _ = hash.Write([]byte(role))
	_, _ = hash.Write([]byte{0})
	_, _ = hash.Write(ciphertext)
	return hex.EncodeToString(hash.Sum(nil))[:24]
}

func extensionProof(sharedSecret []byte, extension ExtensionMessage) ([]byte, error) {
	payload, err := messages.CanonicalJSON(extensionProofPayload(extension))
	if err != nil {
		return nil, err
	}
	mac := hmac.New(sha256.New, sharedSecret)
	_, _ = mac.Write([]byte("pq-fabric/routing/extension-proof/v1"))
	_, _ = mac.Write([]byte{0})
	_, _ = mac.Write(payload)
	return mac.Sum(nil), nil
}

func extensionProofPayload(extension ExtensionMessage) struct {
	CircuitID  string  `json:"circuit_id"`
	RelayID    string  `json:"relay_id"`
	Role       HopRole `json:"role"`
	Nonce      string  `json:"nonce"`
	Ciphertext []byte  `json:"ciphertext"`
} {
	return struct {
		CircuitID  string  `json:"circuit_id"`
		RelayID    string  `json:"relay_id"`
		Role       HopRole `json:"role"`
		Nonce      string  `json:"nonce"`
		Ciphertext []byte  `json:"ciphertext"`
	}{
		CircuitID:  extension.CircuitID,
		RelayID:    extension.RelayID,
		Role:       extension.Role,
		Nonce:      extension.Nonce,
		Ciphertext: extension.Ciphertext,
	}
}

func validRole(role HopRole) bool {
	switch role {
	case Entry, Middle, Exit:
		return true
	default:
		return false
	}
}
