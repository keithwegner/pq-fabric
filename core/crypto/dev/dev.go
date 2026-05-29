// Package dev exposes the development-only crypto adapters used by the local
// deterministic demo and tests. These adapters are not post-quantum secure.
package dev

import pqcrypto "github.com/keithwegner/pq-fabric/core/crypto"

const (
	SignatureAlgorithm = pqcrypto.DevSignatureAlgorithm
	KEMAlgorithm       = pqcrypto.DevKEMAlgorithm
)

type Signer = pqcrypto.DevSigner
type SignatureVerifier = pqcrypto.DevSignatureVerifier
type KEMPrivateKey = pqcrypto.DevKEMPrivate
type KEMPublicKey = pqcrypto.DevKEMPublic

func NewDeterministicSigner(nodeID string) *Signer {
	return pqcrypto.NewDeterministicDevSigner(nodeID)
}

func NewRandomKEMPrivate() (*KEMPrivateKey, error) {
	return pqcrypto.NewRandomDevKEMPrivate()
}

func NewDeterministicKEMPrivate(label string) (*KEMPrivateKey, error) {
	return pqcrypto.NewDeterministicDevKEMPrivate(label)
}

func NewKEMPublic(publicBytes []byte) (*KEMPublicKey, error) {
	return pqcrypto.NewDevKEMPublic(publicBytes)
}
