// SPDX-License-Identifier: MIT
pragma solidity ^0.8.24;

import "./AnchorAccess.sol";

/// @notice Anchors governance proposal hashes and lifecycle metadata.
/// @dev Proposal quorum and PQ signatures are validated off-chain.
contract GovernanceAnchorRegistry is AnchorAccess {
    enum ProposalState {
        Unknown,
        Anchored,
        Accepted,
        Rejected,
        Executed
    }

    struct ProposalRecord {
        bytes32 proposalHash;
        string creatorId;
        string metadataURI;
        bytes32 metadataHash;
        ProposalState state;
        uint256 createdAt;
        uint256 updatedAt;
        bool exists;
    }

    mapping(bytes32 => ProposalRecord) private proposals;

    event GovernanceProposalAnchored(bytes32 indexed proposalHash, string creatorId, bytes32 metadataHash);
    event GovernanceProposalStateUpdated(bytes32 indexed proposalHash, ProposalState state);

    error ZeroProposalHash();
    error ProposalExists();
    error ProposalMissing();
    error InvalidState();

    constructor(address initialOwner) AnchorAccess(initialOwner) {}

    function anchorProposal(
        bytes32 proposalHash,
        string calldata creatorId,
        string calldata metadataURI,
        bytes32 metadataHash
    ) external onlyAuthorized {
        if (proposalHash == bytes32(0)) revert ZeroProposalHash();
        if (proposals[proposalHash].exists) revert ProposalExists();
        proposals[proposalHash] = ProposalRecord({
            proposalHash: proposalHash,
            creatorId: creatorId,
            metadataURI: metadataURI,
            metadataHash: metadataHash,
            state: ProposalState.Anchored,
            createdAt: block.timestamp,
            updatedAt: block.timestamp,
            exists: true
        });
        emit GovernanceProposalAnchored(proposalHash, creatorId, metadataHash);
    }

    function updateProposalState(bytes32 proposalHash, ProposalState state) external onlyAuthorized {
        if (!proposals[proposalHash].exists) revert ProposalMissing();
        if (state == ProposalState.Unknown) revert InvalidState();
        proposals[proposalHash].state = state;
        proposals[proposalHash].updatedAt = block.timestamp;
        emit GovernanceProposalStateUpdated(proposalHash, state);
    }

    function getProposal(bytes32 proposalHash) external view returns (ProposalRecord memory) {
        return proposals[proposalHash];
    }
}
