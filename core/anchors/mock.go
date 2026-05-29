package anchors

import (
	"context"
	"fmt"
	"sync"
)

type MockBackend struct {
	mu          sync.Mutex
	admin       string
	roles       map[string]map[string]bool
	identities  map[string]IdentityRecord
	credentials map[string]CredentialRecord
	proposals   map[string]GovernanceProposalRecord
	qcs         map[string]QuorumCertificateRecord
	tick        uint64
}

func NewMockBackend(admin string) *MockBackend {
	b := &MockBackend{
		admin:       admin,
		roles:       make(map[string]map[string]bool),
		identities:  make(map[string]IdentityRecord),
		credentials: make(map[string]CredentialRecord),
		proposals:   make(map[string]GovernanceProposalRecord),
		qcs:         make(map[string]QuorumCertificateRecord),
	}
	for _, role := range []string{RoleIdentityAdmin, RoleCredentialIssuer, RoleGovernanceAdmin, RoleQCAnchorer} {
		b.roles[role] = map[string]bool{admin: true}
	}
	return b
}

func (b *MockBackend) Authorize(role, actor string, allowed bool) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.roles[role] == nil {
		b.roles[role] = make(map[string]bool)
	}
	b.roles[role][actor] = allowed
}

func (b *MockBackend) RegisterIdentity(ctx context.Context, actor string, record IdentityRecord) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := ValidateIdentityRecord(record); err != nil {
		return err
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if !b.authorizedLocked(RoleIdentityAdmin, actor) {
		return fmt.Errorf("%w: %s lacks %s", ErrUnauthorized, actor, RoleIdentityAdmin)
	}
	if _, exists := b.identities[record.ValidatorID]; exists {
		return fmt.Errorf("%w: identity %s", ErrDuplicate, record.ValidatorID)
	}
	record.UpdatedTick = b.nextTickLocked()
	b.identities[record.ValidatorID] = record
	return nil
}

func (b *MockBackend) UpdateIdentity(ctx context.Context, actor string, record IdentityRecord) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := ValidateIdentityRecord(record); err != nil {
		return err
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if !b.authorizedLocked(RoleIdentityAdmin, actor) {
		return fmt.Errorf("%w: %s lacks %s", ErrUnauthorized, actor, RoleIdentityAdmin)
	}
	if _, exists := b.identities[record.ValidatorID]; !exists {
		return fmt.Errorf("%w: identity %s", ErrNotFound, record.ValidatorID)
	}
	record.UpdatedTick = b.nextTickLocked()
	b.identities[record.ValidatorID] = record
	return nil
}

func (b *MockBackend) GetIdentity(ctx context.Context, validatorID string) (IdentityRecord, bool, error) {
	if err := ctx.Err(); err != nil {
		return IdentityRecord{}, false, err
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	record, ok := b.identities[validatorID]
	return record, ok, nil
}

func (b *MockBackend) AnchorCredential(ctx context.Context, actor string, record CredentialRecord) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := ValidateCredentialRecord(record); err != nil {
		return err
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if !b.authorizedLocked(RoleCredentialIssuer, actor) {
		return fmt.Errorf("%w: %s lacks %s", ErrUnauthorized, actor, RoleCredentialIssuer)
	}
	if _, exists := b.identities[record.SubjectValidatorID]; !exists {
		return fmt.Errorf("%w: credential subject %s", ErrNotFound, record.SubjectValidatorID)
	}
	if _, exists := b.identities[record.IssuerValidatorID]; !exists {
		return fmt.Errorf("%w: credential issuer %s", ErrNotFound, record.IssuerValidatorID)
	}
	if _, exists := b.credentials[record.CredentialHash]; exists {
		return fmt.Errorf("%w: credential %s", ErrDuplicate, record.CredentialHash)
	}
	record.AnchoredTick = b.nextTickLocked()
	b.credentials[record.CredentialHash] = record
	return nil
}

func (b *MockBackend) GetCredential(ctx context.Context, credentialHash string) (CredentialRecord, bool, error) {
	if err := ctx.Err(); err != nil {
		return CredentialRecord{}, false, err
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	record, ok := b.credentials[credentialHash]
	return record, ok, nil
}

func (b *MockBackend) AnchorGovernanceProposal(ctx context.Context, actor string, record GovernanceProposalRecord) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if record.State == "" {
		record.State = ProposalStateAnchored
	}
	if err := ValidateGovernanceProposalRecord(record); err != nil {
		return err
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if !b.authorizedLocked(RoleGovernanceAdmin, actor) {
		return fmt.Errorf("%w: %s lacks %s", ErrUnauthorized, actor, RoleGovernanceAdmin)
	}
	if _, exists := b.proposals[record.ProposalHash]; exists {
		return fmt.Errorf("%w: proposal %s", ErrDuplicate, record.ProposalHash)
	}
	tick := b.nextTickLocked()
	record.CreatedTick = tick
	record.UpdatedTick = tick
	b.proposals[record.ProposalHash] = record
	return nil
}

func (b *MockBackend) UpdateGovernanceProposalState(ctx context.Context, actor, proposalHash, state string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if proposalHash == "" || state == "" {
		return fmt.Errorf("%w: proposal hash and state required", ErrInvalid)
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if !b.authorizedLocked(RoleGovernanceAdmin, actor) {
		return fmt.Errorf("%w: %s lacks %s", ErrUnauthorized, actor, RoleGovernanceAdmin)
	}
	record, exists := b.proposals[proposalHash]
	if !exists {
		return fmt.Errorf("%w: proposal %s", ErrNotFound, proposalHash)
	}
	record.State = state
	record.UpdatedTick = b.nextTickLocked()
	b.proposals[proposalHash] = record
	return nil
}

func (b *MockBackend) GetGovernanceProposal(ctx context.Context, proposalHash string) (GovernanceProposalRecord, bool, error) {
	if err := ctx.Err(); err != nil {
		return GovernanceProposalRecord{}, false, err
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	record, ok := b.proposals[proposalHash]
	return record, ok, nil
}

func (b *MockBackend) AnchorQuorumCertificate(ctx context.Context, actor string, record QuorumCertificateRecord) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := ValidateQuorumCertificateRecord(record); err != nil {
		return err
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if !b.authorizedLocked(RoleQCAnchorer, actor) {
		return fmt.Errorf("%w: %s lacks %s", ErrUnauthorized, actor, RoleQCAnchorer)
	}
	if _, exists := b.qcs[record.QCHash]; exists {
		return fmt.Errorf("%w: qc %s", ErrDuplicate, record.QCHash)
	}
	record.AnchoredTick = b.nextTickLocked()
	b.qcs[record.QCHash] = record
	return nil
}

func (b *MockBackend) GetQuorumCertificateAnchor(ctx context.Context, qcHash string) (QuorumCertificateRecord, bool, error) {
	if err := ctx.Err(); err != nil {
		return QuorumCertificateRecord{}, false, err
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	record, ok := b.qcs[qcHash]
	return record, ok, nil
}

func (b *MockBackend) Status(ctx context.Context) (Status, error) {
	if err := ctx.Err(); err != nil {
		return Status{}, err
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	return Status{
		Backend:                 "mock",
		IdentityCount:           len(b.identities),
		CredentialCount:         len(b.credentials),
		GovernanceProposalCount: len(b.proposals),
		QuorumCertificateCount:  len(b.qcs),
		Configured:              true,
	}, nil
}

func (b *MockBackend) IdentityRecords() map[string]IdentityRecord {
	b.mu.Lock()
	defer b.mu.Unlock()
	out := make(map[string]IdentityRecord, len(b.identities))
	for key, record := range b.identities {
		out[key] = record
	}
	return out
}

func (b *MockBackend) authorizedLocked(role, actor string) bool {
	return actor != "" && b.roles[role][actor]
}

func (b *MockBackend) nextTickLocked() uint64 {
	b.tick++
	return b.tick
}
