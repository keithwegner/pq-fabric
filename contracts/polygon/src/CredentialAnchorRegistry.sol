// SPDX-License-Identifier: MIT
pragma solidity ^0.8.24;

import "./AnchorAccess.sol";
import "./IdentityAnchorRegistry.sol";

/// @notice Anchors credential hashes associated with validator identities.
/// @dev Credential signature verification remains off-chain in validators.
contract CredentialAnchorRegistry is AnchorAccess {
    struct CredentialRecord {
        bytes32 credentialHash;
        string subjectValidatorId;
        string issuerValidatorId;
        uint256 validFrom;
        uint256 validUntil;
        bytes32 metadataHash;
        uint256 anchoredAt;
        bool exists;
    }

    IdentityAnchorRegistry public immutable identityRegistry;
    mapping(bytes32 => CredentialRecord) private credentials;

    event CredentialAnchored(bytes32 indexed credentialHash, string subjectValidatorId, string issuerValidatorId, bytes32 metadataHash);

    error ZeroCredentialHash();
    error CredentialExists();
    error UnknownSubjectIdentity();
    error UnknownIssuerIdentity();
    error InvalidValidityWindow();

    constructor(address initialOwner, IdentityAnchorRegistry registry) AnchorAccess(initialOwner) {
        identityRegistry = registry;
    }

    function anchorCredential(
        bytes32 credentialHash,
        string calldata subjectValidatorId,
        string calldata issuerValidatorId,
        uint256 validFrom,
        uint256 validUntil,
        bytes32 metadataHash
    ) external onlyAuthorized {
        if (credentialHash == bytes32(0)) revert ZeroCredentialHash();
        if (credentials[credentialHash].exists) revert CredentialExists();
        if (!identityRegistry.hasIdentity(subjectValidatorId)) revert UnknownSubjectIdentity();
        if (!identityRegistry.hasIdentity(issuerValidatorId)) revert UnknownIssuerIdentity();
        if (validUntil != 0 && validUntil < validFrom) revert InvalidValidityWindow();
        credentials[credentialHash] = CredentialRecord({
            credentialHash: credentialHash,
            subjectValidatorId: subjectValidatorId,
            issuerValidatorId: issuerValidatorId,
            validFrom: validFrom,
            validUntil: validUntil,
            metadataHash: metadataHash,
            anchoredAt: block.timestamp,
            exists: true
        });
        emit CredentialAnchored(credentialHash, subjectValidatorId, issuerValidatorId, metadataHash);
    }

    function getCredential(bytes32 credentialHash) external view returns (CredentialRecord memory) {
        return credentials[credentialHash];
    }
}
