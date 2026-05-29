// SPDX-License-Identifier: MIT
pragma solidity ^0.8.24;

import "../src/IdentityAnchorRegistry.sol";
import "../src/CredentialAnchorRegistry.sol";
import "../src/GovernanceAnchorRegistry.sol";
import "../src/QuorumCertificateAnchorRegistry.sol";

interface DeployAllVm {
    function envOr(string calldata key, address defaultValue) external returns (address);
    function startBroadcast() external;
    function stopBroadcast() external;
}

contract DeployAllAnchors {
    DeployAllVm private constant vm = DeployAllVm(address(uint160(uint256(keccak256("hevm cheat code")))));

    function run()
        external
        returns (
            IdentityAnchorRegistry identityRegistry,
            CredentialAnchorRegistry credentialRegistry,
            GovernanceAnchorRegistry governanceRegistry,
            QuorumCertificateAnchorRegistry qcRegistry
        )
    {
        address initialOwner = vm.envOr("PQ_FABRIC_ANCHOR_OWNER", msg.sender);
        vm.startBroadcast();
        identityRegistry = new IdentityAnchorRegistry(initialOwner);
        credentialRegistry = new CredentialAnchorRegistry(initialOwner, identityRegistry);
        governanceRegistry = new GovernanceAnchorRegistry(initialOwner);
        qcRegistry = new QuorumCertificateAnchorRegistry(initialOwner);
        vm.stopBroadcast();
    }
}
