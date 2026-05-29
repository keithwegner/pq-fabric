package protocol

import (
	"testing"

	pqcrypto "github.com/keithwegner/pq-fabric/core/crypto"
	cryptosuite "github.com/keithwegner/pq-fabric/core/crypto/suite"
	"github.com/keithwegner/pq-fabric/core/identity"
)

func TestQuorumCertificateRequiresFiveUniqueVotes(t *testing.T) {
	ids := identity.DefaultValidatorIDs()
	urls := make(map[string]string)
	for _, id := range ids {
		urls[id] = "http://" + id + ":8080"
	}
	identities := identity.DefaultValidatorIdentities(urls)
	block := NewBlock(1, GenesisHash, "test payload", "validator-1")
	blockHash, err := block.Hash()
	if err != nil {
		t.Fatal(err)
	}
	votes := make([]Vote, 0, 5)
	for i := 0; i < 5; i++ {
		signer := pqcrypto.NewDeterministicDevSigner(ids[i])
		vote, err := SignVote(1, blockHash, ids[i], signer)
		if err != nil {
			t.Fatal(err)
		}
		votes = append(votes, vote)
	}
	cert, err := FormQuorumCertificate(1, blockHash, votes, 5)
	if err != nil {
		t.Fatal(err)
	}
	if len(cert.Votes) != 5 {
		t.Fatalf("expected 5 votes, got %d", len(cert.Votes))
	}
	if err := VerifyQuorumCertificate(cert, identities, pqcrypto.DevSignatureVerifier{}); err != nil {
		t.Fatal(err)
	}
}

func TestQuorumCertificateRejectsDuplicateVotes(t *testing.T) {
	ids := identity.DefaultValidatorIDs()
	urls := make(map[string]string)
	for _, id := range ids {
		urls[id] = "http://" + id + ":8080"
	}
	identities := identity.DefaultValidatorIdentities(urls)
	block := NewBlock(1, GenesisHash, "test payload", "validator-1")
	blockHash, err := block.Hash()
	if err != nil {
		t.Fatal(err)
	}
	signer := pqcrypto.NewDeterministicDevSigner("validator-1")
	vote, err := SignVote(1, blockHash, "validator-1", signer)
	if err != nil {
		t.Fatal(err)
	}
	cert := QuorumCertificate{Height: 1, BlockHash: blockHash, Decision: DecisionCommit, Threshold: 5, Votes: []Vote{vote, vote, vote, vote, vote}}
	if err := VerifyQuorumCertificate(cert, identities, pqcrypto.DevSignatureVerifier{}); err == nil {
		t.Fatal("expected duplicate vote certificate to be rejected")
	}
}

func TestProposalVerificationRejectsTampering(t *testing.T) {
	identities := testIdentities()
	signer := pqcrypto.NewDeterministicDevSigner("validator-1")
	block := NewBlock(1, GenesisHash, "payload", "validator-1")
	proposal, err := SignProposal(block, signer)
	if err != nil {
		t.Fatal(err)
	}
	if err := VerifyProposal(proposal, identities, pqcrypto.DevSignatureVerifier{}); err != nil {
		t.Fatal(err)
	}
	tampered := proposal
	tampered.Block.Payload = "changed"
	if err := VerifyProposal(tampered, identities, pqcrypto.DevSignatureVerifier{}); err == nil {
		t.Fatal("expected tampered proposal to fail")
	}
	badEncoding := proposal
	badEncoding.Signature = "not base64"
	if err := VerifyProposal(badEncoding, identities, pqcrypto.DevSignatureVerifier{}); err == nil {
		t.Fatal("expected malformed proposal signature to fail")
	}
	badAlgorithm := proposal
	badAlgorithm.Algorithm = "other"
	if err := VerifyProposal(badAlgorithm, identities, pqcrypto.DevSignatureVerifier{}); err == nil {
		t.Fatal("expected proposal algorithm mismatch to fail")
	}
	unknownProposer := proposal
	unknownProposer.Block.ProposerID = "validator-99"
	if err := VerifyProposal(unknownProposer, identities, pqcrypto.DevSignatureVerifier{}); err == nil {
		t.Fatal("expected unknown proposer to fail")
	}
}

func TestProtocolVerifiesPQSuiteSignatures(t *testing.T) {
	selected, err := cryptosuite.Lookup("pq")
	if err != nil {
		t.Fatal(err)
	}
	identities, err := identity.ValidatorIdentitiesForSuite(testURLs(), selected)
	if err != nil {
		t.Fatal(err)
	}
	signer, err := selected.NewSigner("validator-1")
	if err != nil {
		t.Fatal(err)
	}
	block := NewBlock(1, GenesisHash, "pq payload", "validator-1")
	proposal, err := SignProposal(block, signer)
	if err != nil {
		t.Fatal(err)
	}
	if err := VerifyProposal(proposal, identities, selected.NewVerifier()); err != nil {
		t.Fatal(err)
	}
	blockHash, err := block.Hash()
	if err != nil {
		t.Fatal(err)
	}
	vote, err := SignVote(1, blockHash, "validator-1", signer)
	if err != nil {
		t.Fatal(err)
	}
	if err := VerifyVote(vote, identities, selected.NewVerifier()); err != nil {
		t.Fatal(err)
	}
}

func TestVoteVerificationRejectsMalformedUnsupportedAndTamperedVotes(t *testing.T) {
	identities := testIdentities()
	signer := pqcrypto.NewDeterministicDevSigner("validator-1")
	vote, err := SignVote(1, "block-hash", "validator-1", signer)
	if err != nil {
		t.Fatal(err)
	}
	if err := VerifyVote(vote, identities, pqcrypto.DevSignatureVerifier{}); err != nil {
		t.Fatal(err)
	}
	unsupported := vote
	unsupported.Decision = "abort"
	if err := VerifyVote(unsupported, identities, pqcrypto.DevSignatureVerifier{}); err == nil {
		t.Fatal("expected unsupported vote decision to fail")
	}
	malformed := vote
	malformed.Signature = "not base64"
	if err := VerifyVote(malformed, identities, pqcrypto.DevSignatureVerifier{}); err == nil {
		t.Fatal("expected malformed vote signature to fail")
	}
	tampered := vote
	tampered.BlockHash = "different"
	if err := VerifyVote(tampered, identities, pqcrypto.DevSignatureVerifier{}); err == nil {
		t.Fatal("expected tampered vote to fail")
	}
	wrongKey := vote
	wrongKey.VoterID = "validator-2"
	if err := VerifyVote(wrongKey, identities, pqcrypto.DevSignatureVerifier{}); err == nil {
		t.Fatal("expected vote signed by wrong validator key to fail")
	}
	badAlgorithm := vote
	badAlgorithm.Algorithm = "other"
	if err := VerifyVote(badAlgorithm, identities, pqcrypto.DevSignatureVerifier{}); err == nil {
		t.Fatal("expected vote algorithm mismatch to fail")
	}
	unknown := vote
	unknown.VoterID = "validator-99"
	if err := VerifyVote(unknown, identities, pqcrypto.DevSignatureVerifier{}); err == nil {
		t.Fatal("expected unknown voter to fail")
	}
}

func TestQuorumCertificateRejectsInvalidThresholdAndMetadata(t *testing.T) {
	identities := testIdentities()
	ids := identity.DefaultValidatorIDs()
	block := NewBlock(1, GenesisHash, "payload", "validator-1")
	blockHash, err := block.Hash()
	if err != nil {
		t.Fatal(err)
	}
	votes := make([]Vote, 0, 5)
	for i := 0; i < 5; i++ {
		vote, err := SignVote(1, blockHash, ids[i], pqcrypto.NewDeterministicDevSigner(ids[i]))
		if err != nil {
			t.Fatal(err)
		}
		votes = append(votes, vote)
	}
	if _, err := FormQuorumCertificate(1, blockHash, votes, 0); err == nil {
		t.Fatal("expected non-positive threshold to fail")
	}
	cert, err := FormQuorumCertificate(1, blockHash, votes, 5)
	if err != nil {
		t.Fatal(err)
	}
	wrongCount := cert
	wrongCount.ValidatorVoteCount = 4
	if err := VerifyQuorumCertificate(wrongCount, identities, pqcrypto.DevSignatureVerifier{}); err == nil {
		t.Fatal("expected vote count mismatch to fail")
	}
	tooHighThreshold := cert
	tooHighThreshold.Threshold = 8
	if err := VerifyQuorumCertificate(tooHighThreshold, identities, pqcrypto.DevSignatureVerifier{}); err == nil {
		t.Fatal("expected threshold above validator set to fail")
	}
	wrongHash := cert
	wrongHash.BlockHash = ""
	if err := VerifyQuorumCertificate(wrongHash, identities, pqcrypto.DevSignatureVerifier{}); err == nil {
		t.Fatal("expected empty certificate block hash to fail")
	}
	wrongBlockHash := cert
	wrongBlockHash.BlockHash = "different"
	if err := VerifyQuorumCertificate(wrongBlockHash, identities, pqcrypto.DevSignatureVerifier{}); err == nil {
		t.Fatal("expected wrong certificate block hash to fail")
	}
}

func TestProposerSelectionIsDeterministic(t *testing.T) {
	ids := identity.DefaultValidatorIDs()
	tests := []struct {
		height uint64
		round  uint64
		want   string
	}{
		{height: 1, round: 0, want: "validator-1"},
		{height: 1, round: 1, want: "validator-2"},
		{height: 1, round: 2, want: "validator-3"},
		{height: 2, round: 0, want: "validator-2"},
		{height: 7, round: 1, want: "validator-1"},
	}
	for _, tt := range tests {
		got, err := ProposerFor(tt.height, tt.round, ids)
		if err != nil {
			t.Fatal(err)
		}
		if got != tt.want {
			t.Fatalf("height=%d round=%d proposer=%s want=%s", tt.height, tt.round, got, tt.want)
		}
	}
}

func TestStageQuorumCertificateDoesNotCombineMismatchedVotes(t *testing.T) {
	identities := testIdentities()
	ids := identity.DefaultValidatorIDs()
	block := NewRoundBlock(1, 0, GenesisHash, "payload", "validator-1", "state")
	blockHash, err := block.Hash()
	if err != nil {
		t.Fatal(err)
	}
	votes := make([]Vote, 0, 4)
	for i := 0; i < 4; i++ {
		vote, err := SignStageVote(1, 0, StagePrecommit, blockHash, ids[i], pqcrypto.NewDeterministicDevSigner(ids[i]))
		if err != nil {
			t.Fatal(err)
		}
		votes = append(votes, vote)
	}
	if _, err := FormStageQuorumCertificate(1, 0, StagePrecommit, blockHash, append(votes, votes[0]), 5); err == nil {
		t.Fatal("expected duplicate validator vote not to count toward quorum")
	}
	wrongHashVote, err := SignStageVote(1, 0, StagePrecommit, "other-hash", ids[4], pqcrypto.NewDeterministicDevSigner(ids[4]))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := FormStageQuorumCertificate(1, 0, StagePrecommit, blockHash, append(votes, wrongHashVote), 5); err == nil {
		t.Fatal("expected different block hashes not to combine")
	}
	validFifth, err := SignStageVote(1, 0, StagePrecommit, blockHash, ids[4], pqcrypto.NewDeterministicDevSigner(ids[4]))
	if err != nil {
		t.Fatal(err)
	}
	cert, err := FormStageQuorumCertificate(1, 0, StagePrecommit, blockHash, append(votes, validFifth), 5)
	if err != nil {
		t.Fatal(err)
	}
	if err := VerifyQuorumCertificate(cert, identities, pqcrypto.DevSignatureVerifier{}); err != nil {
		t.Fatal(err)
	}
	wrongStage := cert
	wrongStage.Stage = StagePrevote
	if err := VerifyQuorumCertificate(wrongStage, identities, pqcrypto.DevSignatureVerifier{}); err == nil {
		t.Fatal("expected wrong stage to be rejected")
	}
	unknown := cert
	unknown.Votes = append([]Vote(nil), cert.Votes...)
	unknown.Votes[0].VoterID = "validator-99"
	if err := VerifyQuorumCertificate(unknown, identities, pqcrypto.DevSignatureVerifier{}); err == nil {
		t.Fatal("expected unknown validator to be rejected")
	}
}

func testIdentities() map[string]identity.ValidatorIdentity {
	return identity.DefaultValidatorIdentities(testURLs())
}

func testURLs() map[string]string {
	urls := make(map[string]string)
	for _, id := range identity.DefaultValidatorIDs() {
		urls[id] = "http://" + id + ":8080"
	}
	return urls
}
