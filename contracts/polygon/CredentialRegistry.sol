// SPDX-License-Identifier: MIT
pragma solidity ^0.8.24;

/// @notice Anchors credential hashes on-chain. PQ signature verification remains off-chain in validators.
contract CredentialRegistry {
    address public owner;

    struct CredentialAnchor {
        uint256 identityTokenId;
        bytes32 credentialHash;
        bytes32 issuerHash;
        uint256 issuedAt;
        bool revoked;
    }

    mapping(bytes32 => CredentialAnchor) public credentials;

    event CredentialIssued(bytes32 indexed credentialId, uint256 indexed identityTokenId, bytes32 credentialHash);
    event CredentialRevoked(bytes32 indexed credentialId);

    modifier onlyOwner() {
        require(msg.sender == owner, "not owner");
        _;
    }

    constructor() {
        owner = msg.sender;
    }

    function issue(bytes32 credentialId, uint256 identityTokenId, bytes32 credentialHash, bytes32 issuerHash) external onlyOwner {
        require(credentialId != bytes32(0), "credential id required");
        require(identityTokenId != 0, "identity token required");
        require(credentialHash != bytes32(0), "credential hash required");
        require(issuerHash != bytes32(0), "issuer hash required");
        require(credentials[credentialId].issuedAt == 0, "credential exists");
        credentials[credentialId] = CredentialAnchor(identityTokenId, credentialHash, issuerHash, block.timestamp, false);
        emit CredentialIssued(credentialId, identityTokenId, credentialHash);
    }

    function revoke(bytes32 credentialId) external onlyOwner {
        require(credentials[credentialId].issuedAt != 0, "credential missing");
        require(!credentials[credentialId].revoked, "credential already revoked");
        credentials[credentialId].revoked = true;
        emit CredentialRevoked(credentialId);
    }
}
