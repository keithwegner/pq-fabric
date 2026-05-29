// Package suite selects the crypto adapter suite used by local validators and
// routing tests. The default suite remains development-only for deterministic
// local demos.
package suite

import (
	"fmt"
	"os"
	"strings"

	pqcrypto "github.com/keithwegner/pq-fabric/core/crypto"
	"github.com/keithwegner/pq-fabric/core/crypto/dev"
	"github.com/keithwegner/pq-fabric/core/crypto/mldsa"
	"github.com/keithwegner/pq-fabric/core/crypto/mlkem"
)

const EnvVar = "PQ_FABRIC_CRYPTO_SUITE"

type Name string

const (
	Dev Name = "dev"
	PQ  Name = "pq"
)

type CryptoSuite struct {
	Name               Name
	SignatureAlgorithm string
	KEMAlgorithm       string
	NewSigner          func(nodeID string) (pqcrypto.Signer, error)
	NewVerifier        func() pqcrypto.SignatureVerifier
	NewKEMPrivate      func(label string) (pqcrypto.KEMPrivateKey, error)
	NewKEMPublic       func(publicKey []byte) (pqcrypto.KEMPublicKey, error)
}

func FromEnv() (CryptoSuite, error) {
	return Lookup(os.Getenv(EnvVar))
}

func Lookup(raw string) (CryptoSuite, error) {
	switch Name(strings.ToLower(strings.TrimSpace(raw))) {
	case "", Dev:
		return devSuite(), nil
	case PQ:
		return pqSuite(), nil
	default:
		return CryptoSuite{}, fmt.Errorf("unsupported crypto suite %q; supported suites: %s, %s", raw, Dev, PQ)
	}
}

func MustLookup(raw string) CryptoSuite {
	selected, err := Lookup(raw)
	if err != nil {
		panic(err)
	}
	return selected
}

func devSuite() CryptoSuite {
	return CryptoSuite{
		Name:               Dev,
		SignatureAlgorithm: dev.SignatureAlgorithm,
		KEMAlgorithm:       dev.KEMAlgorithm,
		NewSigner: func(nodeID string) (pqcrypto.Signer, error) {
			return dev.NewDeterministicSigner(nodeID), nil
		},
		NewVerifier: func() pqcrypto.SignatureVerifier {
			return dev.SignatureVerifier{}
		},
		NewKEMPrivate: func(label string) (pqcrypto.KEMPrivateKey, error) {
			return dev.NewDeterministicKEMPrivate(label)
		},
		NewKEMPublic: func(publicKey []byte) (pqcrypto.KEMPublicKey, error) {
			return dev.NewKEMPublic(publicKey)
		},
	}
}

func pqSuite() CryptoSuite {
	return CryptoSuite{
		Name:               PQ,
		SignatureAlgorithm: mldsa.Algorithm,
		KEMAlgorithm:       mlkem.Algorithm,
		NewSigner: func(nodeID string) (pqcrypto.Signer, error) {
			return mldsa.NewDeterministicSigner(nodeID)
		},
		NewVerifier: func() pqcrypto.SignatureVerifier {
			return mldsa.NewVerifier()
		},
		NewKEMPrivate: func(label string) (pqcrypto.KEMPrivateKey, error) {
			return mlkem.NewDeterministicPrivate(label)
		},
		NewKEMPublic: func(publicKey []byte) (pqcrypto.KEMPublicKey, error) {
			return mlkem.NewPublic(publicKey)
		},
	}
}
