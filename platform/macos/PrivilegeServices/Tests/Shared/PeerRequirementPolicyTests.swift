import XCTest
@testable import PrivilegeServicesShared

final class PeerRequirementPolicyTests: XCTestCase {
    func testDefaultsToSecureMode() {
        let policy = PeerRequirementPolicy(config: BrokerConfig())

        XCTAssertFalse(policy.insecureDevMode)
        XCTAssertTrue(policy.requirePeerEntitlements)
        XCTAssertTrue(policy.combinedClientRequirement?.contains("io.thand.agent") == true)
        XCTAssertEqual(policy.entitlement(for: .agent), "io.thand.agent.privileged-broker-client")
    }

    func testSkipsRequirementsInInsecureDevMode() {
        let policy = PeerRequirementPolicy(config: BrokerConfig(insecureDevMode: true))

        XCTAssertNil(policy.combinedClientRequirement)
        XCTAssertNil(policy.signingIdentifier(for: .broker))
    }

    func testCanDisablePeerEntitlementRequirementWithoutDisablingSigningChecks() {
        let policy = PeerRequirementPolicy(config: BrokerConfig(requirePeerEntitlements: false))

        XCTAssertFalse(policy.insecureDevMode)
        XCTAssertFalse(policy.requirePeerEntitlements)
        XCTAssertEqual(policy.signingIdentifier(for: .broker), "io.thand.agent.privilege-broker")
        XCTAssertTrue(policy.combinedClientRequirement?.contains("io.thand.agent") == true)
    }
}
