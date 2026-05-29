package identity

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"fmt"

	cryptosuite "github.com/keithwegner/pq-fabric/core/crypto/suite"
)

// ValidatorIdentity is the local genesis identity material for a validator.
type ValidatorIdentity struct {
	ID                 string `json:"id"`
	Region             string `json:"region"`
	PublicURL          string `json:"public_url"`
	Algorithm          string `json:"algorithm"`
	PublicKey          []byte `json:"public_key"`
	SignatureAlgorithm string `json:"signature_algorithm"`
	SignaturePublicKey []byte `json:"signature_public_key"`
	KEMAlgorithm       string `json:"kem_algorithm"`
	KEMPublicKey       []byte `json:"kem_public_key"`
	KeyID              string `json:"key_id"`
}

func (v ValidatorIdentity) PublicKeyB64() string {
	return base64.StdEncoding.EncodeToString(v.SignaturePublicKeyBytes())
}

func (v ValidatorIdentity) SignaturePublicKeyB64() string {
	return base64.StdEncoding.EncodeToString(v.SignaturePublicKeyBytes())
}

func (v ValidatorIdentity) KEMPublicKeyB64() string {
	return base64.StdEncoding.EncodeToString(v.KEMPublicKey)
}

func (v ValidatorIdentity) SignatureAlgorithmName() string {
	if v.SignatureAlgorithm != "" {
		return v.SignatureAlgorithm
	}
	return v.Algorithm
}

func (v ValidatorIdentity) SignaturePublicKeyBytes() []byte {
	if len(v.SignaturePublicKey) > 0 {
		return append([]byte(nil), v.SignaturePublicKey...)
	}
	return append([]byte(nil), v.PublicKey...)
}

func DefaultValidatorIDs() []string {
	return []string{
		"validator-1",
		"validator-2",
		"validator-3",
		"validator-4",
		"validator-5",
		"validator-6",
		"validator-7",
	}
}

func DefaultRegionFor(id string) string {
	switch id {
	case "validator-1", "validator-2":
		return "nyc"
	case "validator-3", "validator-4":
		return "london"
	case "validator-5", "validator-6", "validator-7":
		return "singapore"
	default:
		return "unknown"
	}
}

func DefaultValidatorIdentities(publicURLs map[string]string) map[string]ValidatorIdentity {
	identities, err := ValidatorIdentitiesForSuite(publicURLs, cryptosuite.MustLookup(string(cryptosuite.Dev)))
	if err != nil {
		panic(err)
	}
	return identities
}

func ValidatorIdentitiesForSuite(publicURLs map[string]string, selected cryptosuite.CryptoSuite) (map[string]ValidatorIdentity, error) {
	out := make(map[string]ValidatorIdentity, len(DefaultValidatorIDs()))
	for _, id := range DefaultValidatorIDs() {
		identity, err := ValidatorIdentityForSuite(id, DefaultRegionFor(id), publicURLs[id], selected)
		if err != nil {
			return nil, err
		}
		out[id] = identity
	}
	return out, nil
}

func ValidatorIdentityForSuite(id, region, publicURL string, selected cryptosuite.CryptoSuite) (ValidatorIdentity, error) {
	signer, err := selected.NewSigner(id)
	if err != nil {
		return ValidatorIdentity{}, fmt.Errorf("create signer for %s: %w", id, err)
	}
	kemPrivate, err := selected.NewKEMPrivate(id)
	if err != nil {
		return ValidatorIdentity{}, fmt.Errorf("create KEM key for %s: %w", id, err)
	}
	signaturePublicKey := signer.PublicKey()
	kemPublicKey := kemPrivate.PublicKey()
	keyID := ValidatorKeyID(id, selected.SignatureAlgorithm, signaturePublicKey, selected.KEMAlgorithm, kemPublicKey)
	return ValidatorIdentity{
		ID:                 id,
		Region:             region,
		PublicURL:          publicURL,
		Algorithm:          signer.Algorithm(),
		PublicKey:          signaturePublicKey,
		SignatureAlgorithm: selected.SignatureAlgorithm,
		SignaturePublicKey: signaturePublicKey,
		KEMAlgorithm:       selected.KEMAlgorithm,
		KEMPublicKey:       kemPublicKey,
		KeyID:              keyID,
	}, nil
}

func RequireIdentity(identities map[string]ValidatorIdentity, id string) (ValidatorIdentity, error) {
	identity, ok := identities[id]
	if !ok {
		return ValidatorIdentity{}, fmt.Errorf("unknown validator identity: %s", id)
	}
	return identity, nil
}

func ValidatorKeyID(validatorID, signatureAlgorithm string, signaturePublicKey []byte, kemAlgorithm string, kemPublicKey []byte) string {
	hash := sha256.New()
	_, _ = hash.Write([]byte("pq-fabric/validator-key-id/v1"))
	_, _ = hash.Write([]byte{0})
	_, _ = hash.Write([]byte(validatorID))
	_, _ = hash.Write([]byte{0})
	_, _ = hash.Write([]byte(signatureAlgorithm))
	_, _ = hash.Write([]byte{0})
	_, _ = hash.Write(signaturePublicKey)
	_, _ = hash.Write([]byte{0})
	_, _ = hash.Write([]byte(kemAlgorithm))
	_, _ = hash.Write([]byte{0})
	_, _ = hash.Write(kemPublicKey)
	return hex.EncodeToString(hash.Sum(nil))
}

func PublicKeyFingerprint(algorithm string, publicKey []byte) string {
	hash := sha256.New()
	_, _ = hash.Write([]byte("pq-fabric/public-key-fingerprint/v1"))
	_, _ = hash.Write([]byte{0})
	_, _ = hash.Write([]byte(algorithm))
	_, _ = hash.Write([]byte{0})
	_, _ = hash.Write(publicKey)
	return hex.EncodeToString(hash.Sum(nil))
}
