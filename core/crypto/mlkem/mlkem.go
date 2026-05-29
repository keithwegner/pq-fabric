// Package mlkem adapts CIRCL's ML-KEM-768 implementation to the pq-fabric
// protocol-facing KEM interfaces.
package mlkem

import (
	"crypto/sha256"
	"encoding/binary"
	"fmt"

	"github.com/cloudflare/circl/kem"
	circlmlkem "github.com/cloudflare/circl/kem/mlkem/mlkem768"
	pqcrypto "github.com/keithwegner/pq-fabric/core/crypto"
)

const Algorithm = "ML-KEM-768"

var (
	_ pqcrypto.KEMPrivateKey = (*PrivateKey)(nil)
	_ pqcrypto.KEMPublicKey  = (*PublicKey)(nil)
)

type PrivateKey struct {
	private kem.PrivateKey
	public  []byte
}

type PublicKey struct {
	public kem.PublicKey
	bytes  []byte
}

func NewRandomPrivate() (*PrivateKey, error) {
	public, private, err := circlmlkem.Scheme().GenerateKeyPair()
	if err != nil {
		return nil, err
	}
	return newPrivate(public, private)
}

func NewDeterministicPrivate(label string) (*PrivateKey, error) {
	scheme := circlmlkem.Scheme()
	seed := deterministicSeed("pq-fabric/mlkem768/private/"+label, scheme.SeedSize())
	public, private := scheme.DeriveKeyPair(seed)
	return newPrivate(public, private)
}

func NewPrivate(privateBytes []byte) (*PrivateKey, error) {
	scheme := circlmlkem.Scheme()
	private, err := scheme.UnmarshalBinaryPrivateKey(privateBytes)
	if err != nil {
		return nil, fmt.Errorf("invalid ML-KEM-768 private key: %w", err)
	}
	return newPrivate(private.Public(), private)
}

func NewPublic(publicBytes []byte) (*PublicKey, error) {
	scheme := circlmlkem.Scheme()
	if len(publicBytes) != scheme.PublicKeySize() {
		return nil, fmt.Errorf("ML-KEM-768 public key length %d does not match %d", len(publicBytes), scheme.PublicKeySize())
	}
	public, err := scheme.UnmarshalBinaryPublicKey(publicBytes)
	if err != nil {
		return nil, fmt.Errorf("invalid ML-KEM-768 public key: %w", err)
	}
	return &PublicKey{public: public, bytes: append([]byte(nil), publicBytes...)}, nil
}

func (k *PrivateKey) Algorithm() string { return Algorithm }
func (k *PrivateKey) PublicKey() []byte { return append([]byte(nil), k.public...) }

func (k *PrivateKey) Decapsulate(ciphertext []byte) ([]byte, error) {
	scheme := circlmlkem.Scheme()
	if len(ciphertext) != scheme.CiphertextSize() {
		return nil, fmt.Errorf("ML-KEM-768 ciphertext length %d does not match %d", len(ciphertext), scheme.CiphertextSize())
	}
	shared, err := scheme.Decapsulate(k.private, ciphertext)
	if err != nil {
		return nil, err
	}
	return append([]byte(nil), shared...), nil
}

func (p *PublicKey) Algorithm() string { return Algorithm }
func (p *PublicKey) Bytes() []byte     { return append([]byte(nil), p.bytes...) }

func (p *PublicKey) Encapsulate() ([]byte, []byte, error) {
	ciphertext, shared, err := circlmlkem.Scheme().Encapsulate(p.public)
	if err != nil {
		return nil, nil, err
	}
	return append([]byte(nil), ciphertext...), append([]byte(nil), shared...), nil
}

func (p *PublicKey) EncapsulateDeterministically(seed []byte) ([]byte, []byte, error) {
	ciphertext, shared, err := circlmlkem.Scheme().EncapsulateDeterministically(p.public, seed)
	if err != nil {
		return nil, nil, err
	}
	return append([]byte(nil), ciphertext...), append([]byte(nil), shared...), nil
}

func newPrivate(public kem.PublicKey, private kem.PrivateKey) (*PrivateKey, error) {
	publicBytes, err := public.MarshalBinary()
	if err != nil {
		return nil, err
	}
	return &PrivateKey{private: private, public: append([]byte(nil), publicBytes...)}, nil
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
