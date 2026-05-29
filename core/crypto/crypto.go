package crypto

// Signer is the narrow protocol-facing interface used by validators and relays.
// The default local suite wires this to a development-only Ed25519 adapter.
// The pq suite wires this to ML-DSA-87 for engineering validation.
type Signer interface {
	NodeID() string
	Algorithm() string
	PublicKey() []byte
	Sign(message []byte) ([]byte, error)
}

// SignatureVerifier verifies detached signatures over canonical protocol bytes.
type SignatureVerifier interface {
	Algorithm() string
	Verify(publicKey, message, signature []byte) bool
}

// KEMPrivateKey represents the private side of a KEM/key-agreement primitive.
// The default local suite wires this to development-only X25519. The pq suite
// wires this to ML-KEM-768 for engineering validation.
type KEMPrivateKey interface {
	Algorithm() string
	PublicKey() []byte
	Decapsulate(ciphertext []byte) ([]byte, error)
}

// KEMPublicKey represents the public side of a KEM/key-agreement primitive.
type KEMPublicKey interface {
	Algorithm() string
	Bytes() []byte
	Encapsulate() (ciphertext []byte, sharedSecret []byte, err error)
}
