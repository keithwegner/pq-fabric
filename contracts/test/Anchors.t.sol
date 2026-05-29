// SPDX-License-Identifier: MIT
pragma solidity ^0.8.24;

import "../polygon/CredentialRegistry.sol";
import "../polygon/GovernanceRegistry.sol";
import "../polygon/IdentityNFT.sol";

contract NonOwner {
    function mint(IdentityNFT nft, address subject, bytes32 metadataHash) external returns (uint256) {
        return nft.mint(subject, metadataHash);
    }

    function issue(
        CredentialRegistry registry,
        bytes32 credentialId,
        uint256 identityTokenId,
        bytes32 credentialHash,
        bytes32 issuerHash
    ) external {
        registry.issue(credentialId, identityTokenId, credentialHash, issuerHash);
    }

    function revoke(CredentialRegistry registry, bytes32 credentialId) external {
        registry.revoke(credentialId);
    }

    function anchorProposal(GovernanceRegistry registry, bytes32 proposalId, bytes32 proposalHash) external {
        registry.anchorProposal(proposalId, proposalHash);
    }

    function acceptProposal(GovernanceRegistry registry, bytes32 proposalId, bytes32 quorumCertificateHash) external {
        registry.acceptProposal(proposalId, quorumCertificateHash);
    }
}

contract AnchorsTest {
    function testIdentityMintGuardrailsAndOwnerOnly() public {
        IdentityNFT nft = new IdentityNFT();
        NonOwner actor = new NonOwner();
        bytes32 metadataHash = keccak256("metadata");

        uint256 tokenId = nft.mint(address(0xBEEF), metadataHash);
        require(tokenId == 1, "token id");
        require(nft.tokenByOwner(address(0xBEEF)) == tokenId, "owner index");

        try nft.mint(address(0), metadataHash) {
            revert("expected zero subject revert");
        } catch {}
        try nft.mint(address(0xCAFE), bytes32(0)) {
            revert("expected zero metadata revert");
        } catch {}
        try nft.mint(address(0xBEEF), keccak256("other")) {
            revert("expected duplicate subject revert");
        } catch {}
        try actor.mint(nft, address(0xCAFE), metadataHash) {
            revert("expected owner-only revert");
        } catch {}
    }

    function testCredentialRegistryGuardrailsAndOwnerOnly() public {
        CredentialRegistry registry = new CredentialRegistry();
        NonOwner actor = new NonOwner();
        bytes32 credentialId = keccak256("credential");
        bytes32 credentialHash = keccak256("credential-hash");
        bytes32 issuerHash = keccak256("issuer");

        registry.issue(credentialId, 1, credentialHash, issuerHash);
        (uint256 identityTokenId, bytes32 storedCredentialHash,, uint256 issuedAt, bool revoked) =
            registry.credentials(credentialId);
        require(identityTokenId == 1, "identity token");
        require(storedCredentialHash == credentialHash, "credential hash");
        require(issuedAt != 0, "issued at");
        require(!revoked, "not revoked");

        try registry.issue(credentialId, 1, credentialHash, issuerHash) {
            revert("expected duplicate issue revert");
        } catch {}
        try registry.issue(bytes32(0), 1, credentialHash, issuerHash) {
            revert("expected zero id revert");
        } catch {}
        try registry.issue(keccak256("missing identity"), 0, credentialHash, issuerHash) {
            revert("expected zero identity revert");
        } catch {}
        try registry.issue(keccak256("missing hash"), 1, bytes32(0), issuerHash) {
            revert("expected zero credential hash revert");
        } catch {}
        try registry.issue(keccak256("missing issuer"), 1, credentialHash, bytes32(0)) {
            revert("expected zero issuer hash revert");
        } catch {}

        registry.revoke(credentialId);
        (,,,, bool revokedAfter) = registry.credentials(credentialId);
        require(revokedAfter, "revoked");
        try registry.revoke(credentialId) {
            revert("expected double revoke revert");
        } catch {}
        try actor.revoke(registry, credentialId) {
            revert("expected owner-only revoke revert");
        } catch {}
        try actor.issue(registry, keccak256("other"), 1, credentialHash, issuerHash) {
            revert("expected owner-only issue revert");
        } catch {}
    }

    function testGovernanceRegistryGuardrailsAndOwnerOnly() public {
        GovernanceRegistry registry = new GovernanceRegistry();
        NonOwner actor = new NonOwner();
        bytes32 proposalId = keccak256("proposal");
        bytes32 proposalHash = keccak256("proposal-hash");
        bytes32 qcHash = keccak256("qc");

        registry.anchorProposal(proposalId, proposalHash);
        (bytes32 storedProposalHash,, uint256 createdAt, bool accepted) = registry.proposals(proposalId);
        require(storedProposalHash == proposalHash, "proposal hash");
        require(createdAt != 0, "created at");
        require(!accepted, "not accepted");

        try registry.anchorProposal(proposalId, proposalHash) {
            revert("expected duplicate proposal revert");
        } catch {}
        try registry.anchorProposal(bytes32(0), proposalHash) {
            revert("expected zero proposal id revert");
        } catch {}
        try registry.anchorProposal(keccak256("missing hash"), bytes32(0)) {
            revert("expected zero proposal hash revert");
        } catch {}
        try actor.anchorProposal(registry, keccak256("other"), proposalHash) {
            revert("expected owner-only anchor revert");
        } catch {}

        registry.acceptProposal(proposalId, qcHash);
        (, bytes32 storedQCHash,, bool acceptedAfter) = registry.proposals(proposalId);
        require(storedQCHash == qcHash, "qc hash");
        require(acceptedAfter, "accepted");
        try registry.acceptProposal(proposalId, qcHash) {
            revert("expected double accept revert");
        } catch {}
        try registry.acceptProposal(keccak256("missing"), qcHash) {
            revert("expected missing proposal revert");
        } catch {}
        bytes32 zeroQCProposal = keccak256("zero qc");
        registry.anchorProposal(zeroQCProposal, proposalHash);
        try registry.acceptProposal(zeroQCProposal, bytes32(0)) {
            revert("expected zero qc hash revert");
        } catch {}
        try actor.acceptProposal(registry, zeroQCProposal, qcHash) {
            revert("expected owner-only accept revert");
        } catch {}
    }
}
