package crypto

import (
	"crypto/ecdh"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"errors"
	"fmt"
)

const (
	// DevSignatureAlgorithm is a development-only signature adapter. It exists so
	// protocol code, quorum formation, and tests can run without vendoring a PQ
	// library. Replace this adapter with ML-DSA-87 for real PQ builds.
	DevSignatureAlgorithm = "DEV-ED25519-SLOT-FOR-ML-DSA-87"

	// DevKEMAlgorithm is a development-only key agreement adapter. Replace with
	// an implementation-backed ML-KEM-768 adapter for PQ engineering builds.
	DevKEMAlgorithm = "DEV-X25519-SLOT-FOR-ML-KEM-768"
)

// DevSigner is deterministic by node id so local demos can recreate the same
// validator identities without a key-management service.
type DevSigner struct {
	nodeID  string
	private ed25519.PrivateKey
	public  ed25519.PublicKey
}

func NewDeterministicDevSigner(nodeID string) *DevSigner {
	seed := sha256.Sum256([]byte("pq-fabric/dev-signer/" + nodeID))
	private := ed25519.NewKeyFromSeed(seed[:])
	public := make([]byte, ed25519.PublicKeySize)
	copy(public, private.Public().(ed25519.PublicKey))
	return &DevSigner{nodeID: nodeID, private: private, public: public}
}

func (s *DevSigner) NodeID() string    { return s.nodeID }
func (s *DevSigner) Algorithm() string { return DevSignatureAlgorithm }
func (s *DevSigner) PublicKey() []byte { return append([]byte(nil), s.public...) }
func (s *DevSigner) Sign(m []byte) ([]byte, error) {
	return ed25519.Sign(s.private, m), nil
}

type DevSignatureVerifier struct{}

func (DevSignatureVerifier) Algorithm() string { return DevSignatureAlgorithm }
func (DevSignatureVerifier) Verify(publicKey, message, signature []byte) bool {
	if len(publicKey) != ed25519.PublicKeySize || len(signature) != ed25519.SignatureSize {
		return false
	}
	return ed25519.Verify(ed25519.PublicKey(publicKey), message, signature)
}

type DevKEMPrivate struct {
	private *ecdh.PrivateKey
}

type DevKEMPublic struct {
	public *ecdh.PublicKey
}

func NewRandomDevKEMPrivate() (*DevKEMPrivate, error) {
	private, err := ecdh.X25519().GenerateKey(rand.Reader)
	if err != nil {
		return nil, err
	}
	return &DevKEMPrivate{private: private}, nil
}

func NewDeterministicDevKEMPrivate(label string) (*DevKEMPrivate, error) {
	seed := sha256.Sum256([]byte("pq-fabric/dev-kem/" + label))
	private, err := ecdh.X25519().NewPrivateKey(seed[:])
	if err != nil {
		return nil, err
	}
	return &DevKEMPrivate{private: private}, nil
}

func (k *DevKEMPrivate) Algorithm() string { return DevKEMAlgorithm }
func (k *DevKEMPrivate) PublicKey() []byte { return k.private.PublicKey().Bytes() }
func (k *DevKEMPrivate) Public() *DevKEMPublic {
	return &DevKEMPublic{public: k.private.PublicKey()}
}

func (k *DevKEMPrivate) Decapsulate(ciphertext []byte) ([]byte, error) {
	if len(ciphertext) == 0 {
		return nil, errors.New("empty development KEM ciphertext")
	}
	peerPublic, err := ecdh.X25519().NewPublicKey(ciphertext)
	if err != nil {
		return nil, fmt.Errorf("invalid development KEM ciphertext: %w", err)
	}
	return k.private.ECDH(peerPublic)
}

func NewDevKEMPublic(publicBytes []byte) (*DevKEMPublic, error) {
	if len(publicBytes) == 0 {
		return nil, errors.New("empty development KEM public key")
	}
	public, err := ecdh.X25519().NewPublicKey(publicBytes)
	if err != nil {
		return nil, fmt.Errorf("invalid development KEM public key: %w", err)
	}
	return &DevKEMPublic{public: public}, nil
}

func (p *DevKEMPublic) Algorithm() string { return DevKEMAlgorithm }
func (p *DevKEMPublic) Bytes() []byte     { return p.public.Bytes() }

func (p *DevKEMPublic) Encapsulate() ([]byte, []byte, error) {
	ephemeral, err := ecdh.X25519().GenerateKey(rand.Reader)
	if err != nil {
		return nil, nil, err
	}
	shared, err := ephemeral.ECDH(p.public)
	if err != nil {
		return nil, nil, err
	}
	return ephemeral.PublicKey().Bytes(), shared, nil
}
