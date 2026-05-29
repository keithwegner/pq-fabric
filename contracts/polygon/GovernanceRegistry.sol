// SPDX-License-Identifier: MIT
pragma solidity ^0.8.24;

/// @notice Stores governance proposal hashes and accepted quorum certificate hashes.
contract GovernanceRegistry {
    address public owner;

    struct ProposalAnchor {
        bytes32 proposalHash;
        bytes32 quorumCertificateHash;
        uint256 createdAt;
        bool accepted;
    }

    mapping(bytes32 => ProposalAnchor) public proposals;

    event ProposalAnchored(bytes32 indexed proposalId, bytes32 proposalHash);
    event ProposalAccepted(bytes32 indexed proposalId, bytes32 quorumCertificateHash);

    modifier onlyOwner() {
        require(msg.sender == owner, "not owner");
        _;
    }

    constructor() {
        owner = msg.sender;
    }

    function anchorProposal(bytes32 proposalId, bytes32 proposalHash) external onlyOwner {
        require(proposalId != bytes32(0), "proposal id required");
        require(proposalHash != bytes32(0), "proposal hash required");
        require(proposals[proposalId].createdAt == 0, "proposal exists");
        proposals[proposalId] = ProposalAnchor(proposalHash, bytes32(0), block.timestamp, false);
        emit ProposalAnchored(proposalId, proposalHash);
    }

    function acceptProposal(bytes32 proposalId, bytes32 quorumCertificateHash) external onlyOwner {
        require(proposals[proposalId].createdAt != 0, "proposal missing");
        require(quorumCertificateHash != bytes32(0), "quorum certificate hash required");
        require(!proposals[proposalId].accepted, "proposal already accepted");
        proposals[proposalId].quorumCertificateHash = quorumCertificateHash;
        proposals[proposalId].accepted = true;
        emit ProposalAccepted(proposalId, quorumCertificateHash);
    }
}
