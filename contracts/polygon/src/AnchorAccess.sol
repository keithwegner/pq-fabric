// SPDX-License-Identifier: MIT
pragma solidity ^0.8.24;

/// @notice Minimal self-contained owner/role helper for pq-fabric anchors.
/// @dev This prototype intentionally avoids external dependencies.
contract AnchorAccess {
    address public owner;
    mapping(address => bool) public authorized;

    event AuthorizationUpdated(address indexed account, bool authorized);
    event OwnershipTransferred(address indexed previousOwner, address indexed newOwner);

    error NotAuthorized();
    error ZeroAddress();

    modifier onlyAuthorized() {
        if (!authorized[msg.sender]) revert NotAuthorized();
        _;
    }

    constructor(address initialOwner) {
        if (initialOwner == address(0)) revert ZeroAddress();
        owner = initialOwner;
        authorized[initialOwner] = true;
        emit AuthorizationUpdated(initialOwner, true);
    }

    function setAuthorized(address account, bool allowed) external onlyAuthorized {
        if (account == address(0)) revert ZeroAddress();
        authorized[account] = allowed;
        emit AuthorizationUpdated(account, allowed);
    }

    function transferOwnership(address newOwner) external onlyAuthorized {
        if (newOwner == address(0)) revert ZeroAddress();
        address previous = owner;
        owner = newOwner;
        authorized[newOwner] = true;
        emit OwnershipTransferred(previous, newOwner);
        emit AuthorizationUpdated(newOwner, true);
    }
}
