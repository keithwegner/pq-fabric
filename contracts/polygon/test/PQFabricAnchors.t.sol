// SPDX-License-Identifier: MIT
pragma solidity ^0.8.24;

import "../src/IdentityAnchorRegistry.sol";
import "../src/CredentialAnchorRegistry.sol";
import "../src/GovernanceAnchorRegistry.sol";
import "../src/QuorumCertificateAnchorRegistry.sol";

interface Vm {
    function expectEmit(bool checkTopic1, bool checkTopic2, bool checkTopic3, bool checkData, address emitter) external;
}

contract UnauthorizedCaller {
    function registerIdentity(IdentityAnchorRegistry registry) external {
        registry.registerIdentity("validator-2", "nyc", "ML-DSA-87", bytes32(uint256(2)), "ML-KEM-768", bytes32(uint256(3)), "ipfs://identity", bytes32(uint256(4)));
    }

    function updateIdentity(IdentityAnchorRegistry registry) external {
        registry.updateIdentity("validator-1", "london", "ML-DSA-87", bytes32(uint256(2)), "ML-KEM-768", bytes32(uint256(3)), "ipfs://identity-2", bytes32(uint256(5)));
    }

    function anchorCredential(CredentialAnchorRegistry registry) external {
        registry.anchorCredential(bytes32(uint256(11)), "validator-1", "validator-1", 1, 10, bytes32(uint256(12)));
    }

    function anchorProposal(GovernanceAnchorRegistry registry) external {
        registry.anchorProposal(bytes32(uint256(21)), "validator-1", "ipfs://proposal", bytes32(uint256(22)));
    }

    function updateProposal(GovernanceAnchorRegistry registry) external {
        registry.updateProposalState(bytes32(uint256(21)), GovernanceAnchorRegistry.ProposalState.Accepted);
    }

    function anchorQC(QuorumCertificateAnchorRegistry registry) external {
        registry.anchorQuorumCertificate(bytes32(uint256(31)), 1, 0, bytes32(uint256(32)), bytes32(0), 5, 5, bytes32(uint256(33)));
    }
}

contract PQFabricAnchorsTest {
    Vm private constant vm = Vm(address(uint160(uint256(keccak256("hevm cheat code")))));

    IdentityAnchorRegistry private identityRegistry;
    CredentialAnchorRegistry private credentialRegistry;
    GovernanceAnchorRegistry private governanceRegistry;
    QuorumCertificateAnchorRegistry private qcRegistry;

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
    event CredentialAnchored(bytes32 indexed credentialHash, string subjectValidatorId, string issuerValidatorId, bytes32 metadataHash);
    event GovernanceProposalAnchored(bytes32 indexed proposalHash, string creatorId, bytes32 metadataHash);
    event GovernanceProposalStateUpdated(bytes32 indexed proposalHash, GovernanceAnchorRegistry.ProposalState state);
    event QuorumCertificateAnchored(bytes32 indexed qcHash, uint64 height, uint64 round, bytes32 blockHash, bytes32 eventHash, uint16 threshold, uint16 signerCount, bytes32 metadataHash);

    function setUp() public {
        identityRegistry = new IdentityAnchorRegistry(address(this));
        credentialRegistry = new CredentialAnchorRegistry(address(this), identityRegistry);
        governanceRegistry = new GovernanceAnchorRegistry(address(this));
        qcRegistry = new QuorumCertificateAnchorRegistry(address(this));
        registerValidatorOne();
    }

    function testRegisterIdentityLookupAndEvent() public {
        IdentityAnchorRegistry local = new IdentityAnchorRegistry(address(this));
        bytes32 idHash = local.validatorIdHash("validator-1");
        vm.expectEmit(true, false, false, true, address(local));
        emit IdentityRegistered(idHash, "validator-1", "nyc", "ML-DSA-87", bytes32(uint256(2)), "ML-KEM-768", bytes32(uint256(3)), "ipfs://identity", bytes32(uint256(4)));
        local.registerIdentity("validator-1", "nyc", "ML-DSA-87", bytes32(uint256(2)), "ML-KEM-768", bytes32(uint256(3)), "ipfs://identity", bytes32(uint256(4)));
        IdentityAnchorRegistry.IdentityRecord memory record = local.getIdentity("validator-1");
        require(record.exists, "identity missing");
        require(keccak256(bytes(record.validatorId)) == keccak256(bytes("validator-1")), "validator id mismatch");
        require(record.signatureKeyFingerprint == bytes32(uint256(2)), "signature fingerprint mismatch");
        require(record.kemKeyFingerprint == bytes32(uint256(3)), "kem fingerprint mismatch");
    }

    function testIdentityUpdateSucceeds() public {
        identityRegistry.updateIdentity("validator-1", "london", "ML-DSA-87", bytes32(uint256(2)), "ML-KEM-768", bytes32(uint256(3)), "ipfs://identity-updated", bytes32(uint256(44)));
        IdentityAnchorRegistry.IdentityRecord memory record = identityRegistry.getIdentity("validator-1");
        require(keccak256(bytes(record.region)) == keccak256(bytes("london")), "region not updated");
        require(record.metadataHash == bytes32(uint256(44)), "metadata not updated");
    }

    function testUnauthorizedIdentityRegistrationAndUpdateFail() public {
        UnauthorizedCaller actor = new UnauthorizedCaller();
        bool registerReverted = false;
        try actor.registerIdentity(identityRegistry) {} catch {
            registerReverted = true;
        }
        require(registerReverted, "unauthorized register should revert");
        bool updateReverted = false;
        try actor.updateIdentity(identityRegistry) {} catch {
            updateReverted = true;
        }
        require(updateReverted, "unauthorized update should revert");
    }

    function testDuplicateAndMalformedIdentityFail() public {
        bool duplicateReverted = false;
        try this.registerDuplicateIdentity() {} catch {
            duplicateReverted = true;
        }
        require(duplicateReverted, "duplicate identity should revert");
        bool emptyIDReverted = false;
        try this.registerMalformedIdentity("") {} catch {
            emptyIDReverted = true;
        }
        require(emptyIDReverted, "empty validator id should revert");
        bool zeroFingerprintReverted = false;
        try this.registerZeroFingerprintIdentity() {} catch {
            zeroFingerprintReverted = true;
        }
        require(zeroFingerprintReverted, "zero fingerprint should revert");
    }

    function testCredentialAnchorLookupEventUnauthorizedDuplicateAndUnknownSubject() public {
        bytes32 credentialHash = bytes32(uint256(100));
        vm.expectEmit(true, false, false, true, address(credentialRegistry));
        emit CredentialAnchored(credentialHash, "validator-1", "validator-1", bytes32(uint256(101)));
        credentialRegistry.anchorCredential(credentialHash, "validator-1", "validator-1", 1, 10, bytes32(uint256(101)));
        CredentialAnchorRegistry.CredentialRecord memory record = credentialRegistry.getCredential(credentialHash);
        require(record.exists, "credential missing");
        require(record.credentialHash == credentialHash, "credential hash mismatch");

        UnauthorizedCaller actor = new UnauthorizedCaller();
        bool unauthorizedReverted = false;
        try actor.anchorCredential(credentialRegistry) {} catch {
            unauthorizedReverted = true;
        }
        require(unauthorizedReverted, "unauthorized credential should revert");

        bool duplicateReverted = false;
        try this.anchorDuplicateCredential(credentialHash) {} catch {
            duplicateReverted = true;
        }
        require(duplicateReverted, "duplicate credential should revert");

        bool unknownSubjectReverted = false;
        try this.anchorUnknownSubjectCredential() {} catch {
            unknownSubjectReverted = true;
        }
        require(unknownSubjectReverted, "unknown subject should revert");
    }

    function testGovernanceAnchorStateEventUnauthorizedAndDuplicate() public {
        bytes32 proposalHash = bytes32(uint256(200));
        vm.expectEmit(true, false, false, true, address(governanceRegistry));
        emit GovernanceProposalAnchored(proposalHash, "validator-1", bytes32(uint256(201)));
        governanceRegistry.anchorProposal(proposalHash, "validator-1", "ipfs://proposal", bytes32(uint256(201)));
        GovernanceAnchorRegistry.ProposalRecord memory record = governanceRegistry.getProposal(proposalHash);
        require(record.exists, "proposal missing");
        require(record.state == GovernanceAnchorRegistry.ProposalState.Anchored, "wrong proposal state");

        vm.expectEmit(true, false, false, true, address(governanceRegistry));
        emit GovernanceProposalStateUpdated(proposalHash, GovernanceAnchorRegistry.ProposalState.Accepted);
        governanceRegistry.updateProposalState(proposalHash, GovernanceAnchorRegistry.ProposalState.Accepted);
        record = governanceRegistry.getProposal(proposalHash);
        require(record.state == GovernanceAnchorRegistry.ProposalState.Accepted, "state not updated");

        UnauthorizedCaller actor = new UnauthorizedCaller();
        bool anchorReverted = false;
        try actor.anchorProposal(governanceRegistry) {} catch {
            anchorReverted = true;
        }
        require(anchorReverted, "unauthorized proposal anchor should revert");
        bool updateReverted = false;
        try actor.updateProposal(governanceRegistry) {} catch {
            updateReverted = true;
        }
        require(updateReverted, "unauthorized proposal update should revert");

        bool duplicateReverted = false;
        try this.anchorDuplicateProposal(proposalHash) {} catch {
            duplicateReverted = true;
        }
        require(duplicateReverted, "duplicate proposal should revert");
    }

    function testQCAnchorLookupEventUnauthorizedDuplicateAndZeroHash() public {
        bytes32 qcHash = bytes32(uint256(300));
        bytes32 blockHash = bytes32(uint256(301));
        vm.expectEmit(true, false, false, true, address(qcRegistry));
        emit QuorumCertificateAnchored(qcHash, 7, 2, blockHash, bytes32(0), 5, 7, bytes32(uint256(302)));
        qcRegistry.anchorQuorumCertificate(qcHash, 7, 2, blockHash, bytes32(0), 5, 7, bytes32(uint256(302)));
        QuorumCertificateAnchorRegistry.QCRecord memory record = qcRegistry.getQuorumCertificate(qcHash);
        require(record.exists, "qc missing");
        require(record.height == 7 && record.round == 2, "qc height/round mismatch");
        require(record.threshold == 5 && record.signerCount == 7, "qc quorum metadata mismatch");

        UnauthorizedCaller actor = new UnauthorizedCaller();
        bool unauthorizedReverted = false;
        try actor.anchorQC(qcRegistry) {} catch {
            unauthorizedReverted = true;
        }
        require(unauthorizedReverted, "unauthorized qc anchor should revert");

        bool duplicateReverted = false;
        try this.anchorDuplicateQC(qcHash) {} catch {
            duplicateReverted = true;
        }
        require(duplicateReverted, "duplicate qc should revert");

        bool zeroHashReverted = false;
        try this.anchorZeroQC() {} catch {
            zeroHashReverted = true;
        }
        require(zeroHashReverted, "zero qc hash should revert");
    }

    function registerValidatorOne() internal {
        identityRegistry.registerIdentity("validator-1", "nyc", "ML-DSA-87", bytes32(uint256(2)), "ML-KEM-768", bytes32(uint256(3)), "ipfs://identity", bytes32(uint256(4)));
    }

    function registerDuplicateIdentity() external {
        identityRegistry.registerIdentity("validator-1", "nyc", "ML-DSA-87", bytes32(uint256(2)), "ML-KEM-768", bytes32(uint256(3)), "ipfs://identity", bytes32(uint256(4)));
    }

    function registerMalformedIdentity(string calldata validatorId) external {
        identityRegistry.registerIdentity(validatorId, "nyc", "ML-DSA-87", bytes32(uint256(2)), "ML-KEM-768", bytes32(uint256(3)), "ipfs://identity", bytes32(uint256(4)));
    }

    function registerZeroFingerprintIdentity() external {
        identityRegistry.registerIdentity("validator-x", "nyc", "ML-DSA-87", bytes32(0), "ML-KEM-768", bytes32(uint256(3)), "ipfs://identity", bytes32(uint256(4)));
    }

    function anchorDuplicateCredential(bytes32 credentialHash) external {
        credentialRegistry.anchorCredential(credentialHash, "validator-1", "validator-1", 1, 10, bytes32(uint256(101)));
    }

    function anchorUnknownSubjectCredential() external {
        credentialRegistry.anchorCredential(bytes32(uint256(102)), "validator-unknown", "validator-1", 1, 10, bytes32(uint256(103)));
    }

    function anchorDuplicateProposal(bytes32 proposalHash) external {
        governanceRegistry.anchorProposal(proposalHash, "validator-1", "ipfs://proposal", bytes32(uint256(201)));
    }

    function anchorDuplicateQC(bytes32 qcHash) external {
        qcRegistry.anchorQuorumCertificate(qcHash, 7, 2, bytes32(uint256(301)), bytes32(0), 5, 7, bytes32(uint256(302)));
    }

    function anchorZeroQC() external {
        qcRegistry.anchorQuorumCertificate(bytes32(0), 7, 2, bytes32(uint256(301)), bytes32(0), 5, 7, bytes32(uint256(302)));
    }
}
