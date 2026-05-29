// SPDX-License-Identifier: MIT
pragma solidity ^0.8.24;

import "../src/GovernanceAnchorRegistry.sol";
import "../src/QuorumCertificateAnchorRegistry.sol";

interface DeployGovernanceVm {
    function envOr(string calldata key, address defaultValue) external returns (address);
    function startBroadcast() external;
    function stopBroadcast() external;
}

contract DeployGovernanceAnchors {
    DeployGovernanceVm private constant vm = DeployGovernanceVm(address(uint160(uint256(keccak256("hevm cheat code")))));

    function run() external returns (GovernanceAnchorRegistry governanceRegistry, QuorumCertificateAnchorRegistry qcRegistry) {
        address initialOwner = vm.envOr("PQ_FABRIC_ANCHOR_OWNER", msg.sender);
        vm.startBroadcast();
        governanceRegistry = new GovernanceAnchorRegistry(initialOwner);
        qcRegistry = new QuorumCertificateAnchorRegistry(initialOwner);
        vm.stopBroadcast();
    }
}
