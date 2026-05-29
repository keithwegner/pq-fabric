// SPDX-License-Identifier: MIT
pragma solidity ^0.8.24;

/// @notice Minimal NFT-like identity anchor for the pq-fabric prototype.
/// @dev This deliberately avoids external dependencies so the repo remains self-contained.
contract IdentityNFT {
    address public owner;
    uint256 public nextTokenId = 1;

    mapping(uint256 => address) public tokenOwner;
    mapping(uint256 => bytes32) public identityMetadataHash;
    mapping(address => uint256) public tokenByOwner;

    event IdentityMinted(uint256 indexed tokenId, address indexed subject, bytes32 metadataHash);

    modifier onlyOwner() {
        require(msg.sender == owner, "not owner");
        _;
    }

    constructor() {
        owner = msg.sender;
    }

    function mint(address subject, bytes32 metadataHash) external onlyOwner returns (uint256 tokenId) {
        require(subject != address(0), "subject required");
        require(metadataHash != bytes32(0), "metadata hash required");
        require(tokenByOwner[subject] == 0, "subject already has identity");
        tokenId = nextTokenId++;
        tokenOwner[tokenId] = subject;
        tokenByOwner[subject] = tokenId;
        identityMetadataHash[tokenId] = metadataHash;
        emit IdentityMinted(tokenId, subject, metadataHash);
    }
}
