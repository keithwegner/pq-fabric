// SPDX-License-Identifier: MIT
pragma solidity ^0.8.24;

import "./AnchorAccess.sol";

/// @notice Anchors quorum-certificate hashes and metadata.
/// @dev The EVM stores evidence hashes only. It does not verify ML-DSA, ML-KEM,
/// validator signatures, quorum membership, or consensus safety.
contract QuorumCertificateAnchorRegistry is AnchorAccess {
    struct QCRecord {
        bytes32 qcHash;
        uint64 height;
        uint64 round;
        bytes32 blockHash;
        bytes32 eventHash;
        uint16 threshold;
        uint16 signerCount;
        bytes32 metadataHash;
        uint256 anchoredAt;
        bool exists;
    }

    mapping(bytes32 => QCRecord) private qcs;

    event QuorumCertificateAnchored(
        bytes32 indexed qcHash,
        uint64 height,
        uint64 round,
        bytes32 blockHash,
        bytes32 eventHash,
        uint16 threshold,
        uint16 signerCount,
        bytes32 metadataHash
    );

    error ZeroQCHash();
    error ZeroBlockOrEventHash();
    error InvalidQuorumMetadata();
    error QCExists();

    constructor(address initialOwner) AnchorAccess(initialOwner) {}

    function anchorQuorumCertificate(
        bytes32 qcHash,
        uint64 height,
        uint64 round,
        bytes32 blockHash,
        bytes32 eventHash,
        uint16 threshold,
        uint16 signerCount,
        bytes32 metadataHash
    ) external onlyAuthorized {
        if (qcHash == bytes32(0)) revert ZeroQCHash();
        if (blockHash == bytes32(0) && eventHash == bytes32(0)) revert ZeroBlockOrEventHash();
        if (threshold == 0 || signerCount < threshold) revert InvalidQuorumMetadata();
        if (qcs[qcHash].exists) revert QCExists();
        qcs[qcHash] = QCRecord({
            qcHash: qcHash,
            height: height,
            round: round,
            blockHash: blockHash,
            eventHash: eventHash,
            threshold: threshold,
            signerCount: signerCount,
            metadataHash: metadataHash,
            anchoredAt: block.timestamp,
            exists: true
        });
        emit QuorumCertificateAnchored(qcHash, height, round, blockHash, eventHash, threshold, signerCount, metadataHash);
    }

    function getQuorumCertificate(bytes32 qcHash) external view returns (QCRecord memory) {
        return qcs[qcHash];
    }
}
