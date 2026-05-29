package protocol

import (
	"errors"
	"fmt"

	"github.com/keithwegner/pq-fabric/core/messages"
)

const (
	CustodyNone      = "none"
	CustodyRequested = "requested"
	CustodyConfirmed = "confirmed"
	CustodyExpired   = "expired"
)

var (
	ErrMalformed = errors.New("malformed bundle")
	ErrExpired   = errors.New("bundle expired")
	ErrDuplicate = errors.New("duplicate bundle id")
)

// Envelope is the deterministic local bundle envelope used by the prototype.
// It is a transport/application record only; it does not claim production
// CCSDS Bundle Protocol interoperability.
type Envelope struct {
	BundleID               string `json:"bundle_id"`
	SourceNodeID           string `json:"source_node_id"`
	DestinationNodeID      string `json:"destination_node_id"`
	ChannelID              string `json:"channel_id"`
	ChannelType            string `json:"channel_type"`
	SequenceNumber         uint64 `json:"sequence_number"`
	TransactionID          string `json:"transaction_id"`
	CreationTick           uint64 `json:"creation_tick"`
	ExpirationTick         uint64 `json:"expiration_tick,omitempty"`
	Priority               int    `json:"priority"`
	PayloadDigest          string `json:"payload_digest"`
	PayloadBytes           []byte `json:"payload_bytes"`
	PreviousBundleID       string `json:"previous_bundle_id,omitempty"`
	CustodyRequested       bool   `json:"custody_requested"`
	CustodyStatus          string `json:"custody_status"`
	QuorumCertificateRef   string `json:"quorum_certificate_ref,omitempty"`
	SignatureRef           string `json:"signature_ref,omitempty"`
	Compression            string `json:"compression,omitempty"`
	CompressionContentType string `json:"compression_content_type,omitempty"`
}

type canonicalEnvelope struct {
	SourceNodeID           string `json:"source_node_id"`
	DestinationNodeID      string `json:"destination_node_id"`
	ChannelID              string `json:"channel_id"`
	ChannelType            string `json:"channel_type"`
	SequenceNumber         uint64 `json:"sequence_number"`
	TransactionID          string `json:"transaction_id"`
	CreationTick           uint64 `json:"creation_tick"`
	ExpirationTick         uint64 `json:"expiration_tick,omitempty"`
	Priority               int    `json:"priority"`
	PayloadDigest          string `json:"payload_digest"`
	PayloadBytes           []byte `json:"payload_bytes"`
	PreviousBundleID       string `json:"previous_bundle_id,omitempty"`
	CustodyRequested       bool   `json:"custody_requested"`
	CustodyStatus          string `json:"custody_status"`
	QuorumCertificateRef   string `json:"quorum_certificate_ref,omitempty"`
	SignatureRef           string `json:"signature_ref,omitempty"`
	Compression            string `json:"compression,omitempty"`
	CompressionContentType string `json:"compression_content_type,omitempty"`
}

type NewEnvelopeInput struct {
	SourceNodeID           string
	DestinationNodeID      string
	ChannelID              string
	ChannelType            string
	SequenceNumber         uint64
	TransactionID          string
	CreationTick           uint64
	ExpirationTick         uint64
	Priority               int
	PayloadBytes           []byte
	PreviousBundleID       string
	CustodyRequested       bool
	Compression            string
	CompressionContentType string
}

func NewEnvelope(in NewEnvelopeInput) (Envelope, error) {
	status := CustodyNone
	if in.CustodyRequested {
		status = CustodyRequested
	}
	env := Envelope{
		SourceNodeID:           in.SourceNodeID,
		DestinationNodeID:      in.DestinationNodeID,
		ChannelID:              in.ChannelID,
		ChannelType:            in.ChannelType,
		SequenceNumber:         in.SequenceNumber,
		TransactionID:          in.TransactionID,
		CreationTick:           in.CreationTick,
		ExpirationTick:         in.ExpirationTick,
		Priority:               in.Priority,
		PayloadDigest:          messages.HashBytes(in.PayloadBytes),
		PayloadBytes:           append([]byte(nil), in.PayloadBytes...),
		PreviousBundleID:       in.PreviousBundleID,
		CustodyRequested:       in.CustodyRequested,
		CustodyStatus:          status,
		Compression:            in.Compression,
		CompressionContentType: in.CompressionContentType,
	}
	digest, err := env.Digest()
	if err != nil {
		return Envelope{}, err
	}
	env.BundleID = "bundle-" + digest[:24]
	return env, nil
}

func (e Envelope) CanonicalBytes() ([]byte, error) {
	if e.PayloadDigest == "" {
		e.PayloadDigest = messages.HashBytes(e.PayloadBytes)
	}
	return messages.CanonicalJSON(canonicalEnvelope{
		SourceNodeID:           e.SourceNodeID,
		DestinationNodeID:      e.DestinationNodeID,
		ChannelID:              e.ChannelID,
		ChannelType:            e.ChannelType,
		SequenceNumber:         e.SequenceNumber,
		TransactionID:          e.TransactionID,
		CreationTick:           e.CreationTick,
		ExpirationTick:         e.ExpirationTick,
		Priority:               e.Priority,
		PayloadDigest:          e.PayloadDigest,
		PayloadBytes:           e.PayloadBytes,
		PreviousBundleID:       e.PreviousBundleID,
		CustodyRequested:       e.CustodyRequested,
		CustodyStatus:          e.CustodyStatus,
		QuorumCertificateRef:   e.QuorumCertificateRef,
		SignatureRef:           e.SignatureRef,
		Compression:            e.Compression,
		CompressionContentType: e.CompressionContentType,
	})
}

func (e Envelope) Digest() (string, error) {
	canonical, err := e.CanonicalBytes()
	if err != nil {
		return "", err
	}
	return messages.HashBytes(canonical), nil
}

func (e Envelope) Validate(nowTick uint64) error {
	if e.SourceNodeID == "" {
		return fmt.Errorf("%w: source node id is required", ErrMalformed)
	}
	if e.DestinationNodeID == "" {
		return fmt.Errorf("%w: destination node id is required", ErrMalformed)
	}
	if e.ChannelID == "" || e.ChannelType == "" {
		return fmt.Errorf("%w: channel id and type are required", ErrMalformed)
	}
	if e.SequenceNumber == 0 {
		return fmt.Errorf("%w: sequence number must be positive", ErrMalformed)
	}
	if e.TransactionID == "" {
		return fmt.Errorf("%w: transaction id is required", ErrMalformed)
	}
	if e.PayloadDigest == "" {
		return fmt.Errorf("%w: payload digest is required", ErrMalformed)
	}
	if got := messages.HashBytes(e.PayloadBytes); got != e.PayloadDigest {
		return fmt.Errorf("%w: payload digest mismatch", ErrMalformed)
	}
	if e.ExpirationTick > 0 && nowTick > e.ExpirationTick {
		return fmt.Errorf("%w: expiration_tick=%d now_tick=%d", ErrExpired, e.ExpirationTick, nowTick)
	}
	return nil
}

type Index struct {
	byID map[string]string
}

func NewIndex() *Index {
	return &Index{byID: make(map[string]string)}
}

func (i *Index) Add(env Envelope) error {
	if i.byID == nil {
		i.byID = make(map[string]string)
	}
	digest, err := env.Digest()
	if err != nil {
		return err
	}
	if existing, ok := i.byID[env.BundleID]; ok {
		if existing != digest {
			return fmt.Errorf("%w: %s", ErrDuplicate, env.BundleID)
		}
		return ErrDuplicate
	}
	i.byID[env.BundleID] = digest
	return nil
}
