// Package mldsa adapts CIRCL's ML-DSA-87 implementation to the pq-fabric
// protocol-facing signature interfaces.
package mldsa

import (
	"crypto/sha256"
	"encoding/binary"
	"fmt"

	circlmldsa "github.com/cloudflare/circl/sign/mldsa/mldsa87"
	pqcrypto "github.com/keithwegner/pq-fabric/core/crypto"
)

const Algorithm = "ML-DSA-87"

var (
	_ pqcrypto.Signer            = (*Signer)(nil)
	_ pqcrypto.SignatureVerifier = Verifier{}
)

type Signer struct {
	nodeID  string
	private *circlmldsa.PrivateKey
	public  []byte
}

type Verifier struct{}

func NewRandomSigner(nodeID string) (*Signer, error) {
	public, private, err := circlmldsa.GenerateKey(nil)
	if err != nil {
		return nil, err
	}
	return newSigner(nodeID, public, private), nil
}

func NewDeterministicSigner(nodeID string) (*Signer, error) {
	seed := deterministicSeed("pq-fabric/mldsa87/signer/"+nodeID, circlmldsa.SeedSize)
	var seedArray [circlmldsa.SeedSize]byte
	copy(seedArray[:], seed)
	public, private := circlmldsa.NewKeyFromSeed(&seedArray)
	return newSigner(nodeID, public, private), nil
}

func NewVerifier() Verifier {
	return Verifier{}
}

func (s *Signer) NodeID() string    { return s.nodeID }
func (s *Signer) Algorithm() string { return Algorithm }
func (s *Signer) PublicKey() []byte { return append([]byte(nil), s.public...) }

func (s *Signer) Sign(message []byte) ([]byte, error) {
	signature := make([]byte, circlmldsa.SignatureSize)
	if err := circlmldsa.SignTo(s.private, message, nil, false, signature); err != nil {
		return nil, err
	}
	return signature, nil
}

func (Verifier) Algorithm() string { return Algorithm }

func (Verifier) Verify(publicKey, message, signature []byte) bool {
	if len(signature) != circlmldsa.SignatureSize {
		return false
	}
	public, err := parsePublicKey(publicKey)
	if err != nil {
		return false
	}
	return circlmldsa.Verify(public, message, nil, signature)
}

func parsePublicKey(publicKey []byte) (*circlmldsa.PublicKey, error) {
	if len(publicKey) != circlmldsa.PublicKeySize {
		return nil, fmt.Errorf("ML-DSA-87 public key length %d does not match %d", len(publicKey), circlmldsa.PublicKeySize)
	}
	var public circlmldsa.PublicKey
	if err := public.UnmarshalBinary(publicKey); err != nil {
		return nil, err
	}
	return &public, nil
}

func newSigner(nodeID string, public *circlmldsa.PublicKey, private *circlmldsa.PrivateKey) *Signer {
	return &Signer{
		nodeID:  nodeID,
		private: private,
		public:  public.Bytes(),
	}
}

func deterministicSeed(label string, size int) []byte {
	out := make([]byte, 0, size)
	var counter uint32
	for len(out) < size {
		var counterBytes [4]byte
		binary.BigEndian.PutUint32(counterBytes[:], counter)
		hash := sha256.New()
		_, _ = hash.Write([]byte(label))
		_, _ = hash.Write(counterBytes[:])
		out = append(out, hash.Sum(nil)...)
		counter++
	}
	return out[:size]
}
