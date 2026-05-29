package protocol

import (
	"encoding/base64"
	"errors"
	"fmt"
	"sort"
	"time"

	consensusstate "github.com/keithwegner/pq-fabric/consensus/state"
	pqcrypto "github.com/keithwegner/pq-fabric/core/crypto"
	"github.com/keithwegner/pq-fabric/core/identity"
	"github.com/keithwegner/pq-fabric/core/messages"
)

const (
	DecisionCommit = "commit"
	GenesisHash    = "GENESIS"

	StageProposal  = "proposal"
	StagePrevote   = "prevote"
	StagePrecommit = "precommit"
	StageCommit    = "commit"
)

type Block struct {
	Height             uint64                       `json:"height"`
	Round              uint64                       `json:"round"`
	PreviousHash       string                       `json:"previous_hash"`
	Payload            string                       `json:"payload"`
	Transactions       []consensusstate.Transaction `json:"transactions,omitempty"`
	StateDigest        string                       `json:"state_digest,omitempty"`
	ProposerID         string                       `json:"proposer_id"`
	MembershipVersion  uint64                       `json:"membership_version,omitempty"`
	ValidatorSetHash   string                       `json:"validator_set_hash,omitempty"`
	CreatedAtUnixMilli int64                        `json:"created_at_unix_milli"`
}

func NewBlock(height uint64, previousHash, payload, proposerID string) Block {
	return NewRoundBlock(height, 0, previousHash, payload, proposerID, "")
}

func NewRoundBlock(height, round uint64, previousHash, payload, proposerID, stateDigest string) Block {
	return Block{
		Height:             height,
		Round:              round,
		PreviousHash:       previousHash,
		Payload:            payload,
		Transactions:       consensusstate.NormalizeTransactions(payload, nil),
		StateDigest:        stateDigest,
		ProposerID:         proposerID,
		CreatedAtUnixMilli: time.Now().UnixMilli(),
	}
}

func (b Block) Hash() (string, error) {
	return messages.HashCanonical(b)
}

type Proposal struct {
	Block     Block  `json:"block"`
	Round     uint64 `json:"round"`
	Algorithm string `json:"algorithm"`
	Signature string `json:"signature"`
}

type PrecommitRequest struct {
	Proposal  Proposal          `json:"proposal"`
	PrevoteQC QuorumCertificate `json:"prevote_qc"`
}

type Vote struct {
	Height    uint64 `json:"height"`
	Round     uint64 `json:"round"`
	Stage     string `json:"stage"`
	BlockHash string `json:"block_hash"`
	VoterID   string `json:"voter_id"`
	Decision  string `json:"decision"`
	Algorithm string `json:"algorithm"`
	Signature string `json:"signature"`
}

type VoteToSign struct {
	Height    uint64 `json:"height"`
	Round     uint64 `json:"round"`
	Stage     string `json:"stage"`
	BlockHash string `json:"block_hash"`
	VoterID   string `json:"voter_id"`
	Decision  string `json:"decision"`
}

type QuorumCertificate struct {
	Height             uint64 `json:"height"`
	Round              uint64 `json:"round"`
	Stage              string `json:"stage"`
	BlockHash          string `json:"block_hash"`
	Decision           string `json:"decision"`
	Threshold          int    `json:"threshold"`
	MembershipVersion  uint64 `json:"membership_version,omitempty"`
	ValidatorSetHash   string `json:"validator_set_hash,omitempty"`
	FormedAtUnixMilli  int64  `json:"formed_at_unix_milli"`
	ValidatorVoteCount int    `json:"validator_vote_count"`
	Votes              []Vote `json:"votes"`
}

type LockState struct {
	Height             uint64 `json:"height,omitempty"`
	Round              uint64 `json:"round,omitempty"`
	BlockHash          string `json:"block_hash,omitempty"`
	SourceQCBlockHash  string `json:"source_qc_block_hash,omitempty"`
	SourceQCRound      uint64 `json:"source_qc_round,omitempty"`
	SourceQCStage      string `json:"source_qc_stage,omitempty"`
	SourceVoteCount    int    `json:"source_vote_count,omitempty"`
	UpdatedAtUnixMilli int64  `json:"updated_at_unix_milli,omitempty"`
}

type CommitRequest struct {
	Block       Block             `json:"block"`
	Certificate QuorumCertificate `json:"certificate"`
}

type StateSnapshot struct {
	NodeID            string    `json:"node_id"`
	Region            string    `json:"region"`
	Height            uint64    `json:"height"`
	Round             uint64    `json:"round"`
	LastHash          string    `json:"last_hash"`
	StateDigest       string    `json:"state_digest"`
	Lock              LockState `json:"lock,omitempty"`
	CommitCount       int       `json:"commit_count"`
	Running           bool      `json:"running"`
	LastEvent         string    `json:"last_event,omitempty"`
	CryptoSigner      string    `json:"crypto_signer"`
	SignerProvider    string    `json:"signer_provider,omitempty"`
	MembershipVersion uint64    `json:"membership_version,omitempty"`
	ValidatorSetHash  string    `json:"validator_set_hash,omitempty"`
}

func ProposerFor(height, round uint64, validatorIDs []string) (string, error) {
	if len(validatorIDs) == 0 {
		return "", errors.New("validator set is empty")
	}
	if height == 0 {
		return "", errors.New("height must be positive")
	}
	offset := (height - 1 + round) % uint64(len(validatorIDs))
	return validatorIDs[offset], nil
}

func ProposerForRound(validatorIDs []string, height, round uint64) (string, error) {
	return ProposerFor(height, round, validatorIDs)
}

func SignProposal(block Block, signer pqcrypto.Signer) (Proposal, error) {
	blockHash, err := block.Hash()
	if err != nil {
		return Proposal{}, err
	}
	sig, err := signer.Sign([]byte(blockHash))
	if err != nil {
		return Proposal{}, err
	}
	return Proposal{Block: block, Round: block.Round, Algorithm: signer.Algorithm(), Signature: base64.StdEncoding.EncodeToString(sig)}, nil
}

func VerifyProposal(proposal Proposal, identities map[string]identity.ValidatorIdentity, verifier pqcrypto.SignatureVerifier) error {
	if proposal.Block.Height == 0 {
		return errors.New("proposal height must be positive")
	}
	if proposal.Block.PreviousHash == "" {
		return errors.New("proposal previous hash is required")
	}
	if proposal.Block.ProposerID == "" {
		return errors.New("proposal proposer id is required")
	}
	if proposal.Round != proposal.Block.Round {
		return fmt.Errorf("proposal round %d does not match block round %d", proposal.Round, proposal.Block.Round)
	}
	proposer, err := identity.RequireIdentity(identities, proposal.Block.ProposerID)
	if err != nil {
		return err
	}
	if proposer.SignatureAlgorithmName() != proposal.Algorithm {
		return fmt.Errorf("proposal algorithm mismatch: identity=%s proposal=%s", proposer.SignatureAlgorithmName(), proposal.Algorithm)
	}
	if verifier.Algorithm() != proposal.Algorithm {
		return fmt.Errorf("proposal verifier algorithm mismatch: verifier=%s proposal=%s", verifier.Algorithm(), proposal.Algorithm)
	}
	blockHash, err := proposal.Block.Hash()
	if err != nil {
		return err
	}
	signature, err := base64.StdEncoding.DecodeString(proposal.Signature)
	if err != nil {
		return fmt.Errorf("invalid proposal signature encoding: %w", err)
	}
	if !verifier.Verify(proposer.SignaturePublicKeyBytes(), []byte(blockHash), signature) {
		return errors.New("invalid proposal signature")
	}
	return nil
}

func SignVote(height uint64, blockHash, voterID string, signer pqcrypto.Signer) (Vote, error) {
	return SignStageVote(height, 0, StagePrecommit, blockHash, voterID, signer)
}

func SignStageVote(height, round uint64, stage, blockHash, voterID string, signer pqcrypto.Signer) (Vote, error) {
	stage = NormalizeStage(stage)
	payload := VoteToSign{Height: height, Round: round, Stage: stage, BlockHash: blockHash, VoterID: voterID, Decision: DecisionCommit}
	canonical, err := messages.CanonicalJSON(payload)
	if err != nil {
		return Vote{}, err
	}
	sig, err := signer.Sign(canonical)
	if err != nil {
		return Vote{}, err
	}
	return Vote{
		Height:    height,
		Round:     round,
		Stage:     stage,
		BlockHash: blockHash,
		VoterID:   voterID,
		Decision:  DecisionCommit,
		Algorithm: signer.Algorithm(),
		Signature: base64.StdEncoding.EncodeToString(sig),
	}, nil
}

func VerifyVote(vote Vote, identities map[string]identity.ValidatorIdentity, verifier pqcrypto.SignatureVerifier) error {
	vote.Stage = NormalizeStage(vote.Stage)
	if vote.Decision != DecisionCommit {
		return fmt.Errorf("unsupported vote decision: %s", vote.Decision)
	}
	if !IsVoteStage(vote.Stage) {
		return fmt.Errorf("unsupported vote stage: %s", vote.Stage)
	}
	if vote.Height == 0 {
		return errors.New("vote height must be positive")
	}
	if vote.BlockHash == "" {
		return errors.New("vote block hash is required")
	}
	voter, err := identity.RequireIdentity(identities, vote.VoterID)
	if err != nil {
		return err
	}
	if voter.SignatureAlgorithmName() != vote.Algorithm {
		return fmt.Errorf("vote algorithm mismatch for %s: identity=%s vote=%s", vote.VoterID, voter.SignatureAlgorithmName(), vote.Algorithm)
	}
	if verifier.Algorithm() != vote.Algorithm {
		return fmt.Errorf("vote verifier algorithm mismatch: verifier=%s vote=%s", verifier.Algorithm(), vote.Algorithm)
	}
	payload := VoteToSign{Height: vote.Height, Round: vote.Round, Stage: vote.Stage, BlockHash: vote.BlockHash, VoterID: vote.VoterID, Decision: vote.Decision}
	canonical, err := messages.CanonicalJSON(payload)
	if err != nil {
		return err
	}
	signature, err := base64.StdEncoding.DecodeString(vote.Signature)
	if err != nil {
		return fmt.Errorf("invalid vote signature encoding for %s: %w", vote.VoterID, err)
	}
	if !verifier.Verify(voter.SignaturePublicKeyBytes(), canonical, signature) {
		return fmt.Errorf("invalid vote signature from %s", vote.VoterID)
	}
	return nil
}

func FormQuorumCertificate(height uint64, blockHash string, votes []Vote, threshold int) (QuorumCertificate, error) {
	return FormStageQuorumCertificate(height, 0, StagePrecommit, blockHash, votes, threshold)
}

func FormStageQuorumCertificate(height, round uint64, stage, blockHash string, votes []Vote, threshold int) (QuorumCertificate, error) {
	stage = NormalizeStage(stage)
	if threshold <= 0 {
		return QuorumCertificate{}, errors.New("quorum threshold must be positive")
	}
	if !IsVoteStage(stage) {
		return QuorumCertificate{}, fmt.Errorf("unsupported quorum vote stage: %s", stage)
	}
	unique := make(map[string]Vote)
	for _, vote := range votes {
		vote.Stage = NormalizeStage(vote.Stage)
		if vote.Height == height && vote.Round == round && vote.Stage == stage && vote.BlockHash == blockHash && vote.Decision == DecisionCommit {
			if _, exists := unique[vote.VoterID]; !exists {
				unique[vote.VoterID] = vote
			}
		}
	}
	if len(unique) < threshold {
		return QuorumCertificate{}, fmt.Errorf("insufficient votes for quorum: got %d need %d", len(unique), threshold)
	}
	ids := make([]string, 0, len(unique))
	for id := range unique {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	qcVotes := make([]Vote, 0, len(ids))
	for _, id := range ids {
		qcVotes = append(qcVotes, unique[id])
	}
	return QuorumCertificate{
		Height:             height,
		Round:              round,
		Stage:              stage,
		BlockHash:          blockHash,
		Decision:           DecisionCommit,
		Threshold:          threshold,
		FormedAtUnixMilli:  time.Now().UnixMilli(),
		ValidatorVoteCount: len(unique),
		Votes:              qcVotes,
	}, nil
}

func VerifyQuorumCertificate(cert QuorumCertificate, identities map[string]identity.ValidatorIdentity, verifier pqcrypto.SignatureVerifier) error {
	cert.Stage = NormalizeStage(cert.Stage)
	if cert.Decision != DecisionCommit {
		return fmt.Errorf("unsupported certificate decision: %s", cert.Decision)
	}
	if !IsVoteStage(cert.Stage) {
		return fmt.Errorf("unsupported certificate vote stage: %s", cert.Stage)
	}
	if cert.Height == 0 {
		return errors.New("certificate height must be positive")
	}
	if cert.BlockHash == "" {
		return errors.New("certificate block hash is required")
	}
	if cert.Threshold <= 0 {
		return errors.New("certificate threshold must be positive")
	}
	if cert.Threshold > len(identities) {
		return fmt.Errorf("certificate threshold %d exceeds validator set size %d", cert.Threshold, len(identities))
	}
	unique := make(map[string]struct{})
	for _, vote := range cert.Votes {
		vote.Stage = NormalizeStage(vote.Stage)
		if vote.Height != cert.Height || vote.Round != cert.Round || vote.Stage != cert.Stage || vote.BlockHash != cert.BlockHash || vote.Decision != cert.Decision {
			return fmt.Errorf("vote from %s does not match certificate", vote.VoterID)
		}
		if _, exists := unique[vote.VoterID]; exists {
			return fmt.Errorf("duplicate vote from %s", vote.VoterID)
		}
		if err := VerifyVote(vote, identities, verifier); err != nil {
			return err
		}
		unique[vote.VoterID] = struct{}{}
	}
	if cert.ValidatorVoteCount != len(cert.Votes) {
		return fmt.Errorf("certificate validator vote count %d does not match vote list length %d", cert.ValidatorVoteCount, len(cert.Votes))
	}
	if len(unique) < cert.Threshold {
		return fmt.Errorf("certificate has insufficient unique votes: got %d need %d", len(unique), cert.Threshold)
	}
	return nil
}

func NormalizeStage(stage string) string {
	if stage == "" {
		return StagePrecommit
	}
	return stage
}

func IsVoteStage(stage string) bool {
	switch stage {
	case StagePrevote, StagePrecommit:
		return true
	default:
		return false
	}
}
