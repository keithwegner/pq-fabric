// SPDX-License-Identifier: MIT
pragma solidity ^0.8.24;

import "../src/IdentityAnchorRegistry.sol";

interface DeployVm {
    function envOr(string calldata key, address defaultValue) external returns (address);
    function startBroadcast() external;
    function stopBroadcast() external;
}

contract DeployIdentityAnchors {
    DeployVm private constant vm = DeployVm(address(uint160(uint256(keccak256("hevm cheat code")))));

    function run() external returns (IdentityAnchorRegistry identityRegistry) {
        address initialOwner = vm.envOr("PQ_FABRIC_ANCHOR_OWNER", msg.sender);
        vm.startBroadcast();
        identityRegistry = new IdentityAnchorRegistry(initialOwner);
        vm.stopBroadcast();
    }
}
