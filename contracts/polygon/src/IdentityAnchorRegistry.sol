// SPDX-License-Identifier: MIT
pragma solidity ^0.8.24;

import "./AnchorAccess.sol";

/// @notice Anchors validator identity metadata and key fingerprints.
/// @dev PQ signature verification remains off-chain in pq-fabric validators.
contract IdentityAnchorRegistry is AnchorAccess {
    struct IdentityRecord {
        string validatorId;
        string region;
        string signatureAlgorithm;
        bytes32 signatureKeyFingerprint;
        string kemAlgorithm;
        bytes32 kemKeyFingerprint;
        string metadataURI;
        bytes32 metadataHash;
        uint256 updatedAt;
        bool exists;
    }

    mapping(bytes32 => IdentityRecord) private identities;

    event IdentityRegistered(
        bytes32 indexed validatorIdHash,
        string validatorId,
        string region,
        string signatureAlgorithm,
        bytes32 signatureKeyFingerprint,
        string kemAlgorithm,
        bytes32 kemKeyFingerprint,
        string metadataURI,
        bytes32 metadataHash
    );
    event IdentityUpdated(bytes32 indexed validatorIdHash, string validatorId, bytes32 metadataHash);

    error EmptyValidatorId();
    error EmptyAlgorithm();
    error ZeroFingerprint();
    error IdentityExists();
    error IdentityMissing();

    constructor(address initialOwner) AnchorAccess(initialOwner) {}

    function registerIdentity(
        string calldata validatorId,
        string calldata region,
        string calldata signatureAlgorithm,
        bytes32 signatureKeyFingerprint,
        string calldata kemAlgorithm,
        bytes32 kemKeyFingerprint,
        string calldata metadataURI,
        bytes32 metadataHash
    ) external onlyAuthorized {
        bytes32 idHash = _validateIdentityInput(validatorId, signatureAlgorithm, signatureKeyFingerprint, kemAlgorithm, kemKeyFingerprint);
        if (identities[idHash].exists) revert IdentityExists();
        identities[idHash] = IdentityRecord({
            validatorId: validatorId,
            region: region,
            signatureAlgorithm: signatureAlgorithm,
            signatureKeyFingerprint: signatureKeyFingerprint,
            kemAlgorithm: kemAlgorithm,
            kemKeyFingerprint: kemKeyFingerprint,
            metadataURI: metadataURI,
            metadataHash: metadataHash,
            updatedAt: block.timestamp,
            exists: true
        });
        emit IdentityRegistered(idHash, validatorId, region, signatureAlgorithm, signatureKeyFingerprint, kemAlgorithm, kemKeyFingerprint, metadataURI, metadataHash);
    }

    function updateIdentity(
        string calldata validatorId,
        string calldata region,
        string calldata signatureAlgorithm,
        bytes32 signatureKeyFingerprint,
        string calldata kemAlgorithm,
        bytes32 kemKeyFingerprint,
        string calldata metadataURI,
        bytes32 metadataHash
    ) external onlyAuthorized {
        bytes32 idHash = _validateIdentityInput(validatorId, signatureAlgorithm, signatureKeyFingerprint, kemAlgorithm, kemKeyFingerprint);
        if (!identities[idHash].exists) revert IdentityMissing();
        identities[idHash] = IdentityRecord({
            validatorId: validatorId,
            region: region,
            signatureAlgorithm: signatureAlgorithm,
            signatureKeyFingerprint: signatureKeyFingerprint,
            kemAlgorithm: kemAlgorithm,
            kemKeyFingerprint: kemKeyFingerprint,
            metadataURI: metadataURI,
            metadataHash: metadataHash,
            updatedAt: block.timestamp,
            exists: true
        });
        emit IdentityUpdated(idHash, validatorId, metadataHash);
    }

    function getIdentity(string calldata validatorId) external view returns (IdentityRecord memory) {
        return identities[validatorIdHash(validatorId)];
    }

    function hasIdentity(string calldata validatorId) external view returns (bool) {
        return identities[validatorIdHash(validatorId)].exists;
    }

    function validatorIdHash(string calldata validatorId) public pure returns (bytes32) {
        return keccak256(bytes(validatorId));
    }

    function _validateIdentityInput(
        string calldata validatorId,
        string calldata signatureAlgorithm,
        bytes32 signatureKeyFingerprint,
        string calldata kemAlgorithm,
        bytes32 kemKeyFingerprint
    ) internal pure returns (bytes32) {
        if (bytes(validatorId).length == 0) revert EmptyValidatorId();
        if (bytes(signatureAlgorithm).length == 0 || bytes(kemAlgorithm).length == 0) revert EmptyAlgorithm();
        if (signatureKeyFingerprint == bytes32(0) || kemKeyFingerprint == bytes32(0)) revert ZeroFingerprint();
        return validatorIdHash(validatorId);
    }
}
