import Foundation
import LightweightCodeRequirements
import XPC

public enum PeerRole {
    case agent
    case notifier
    case broker
}

public struct PeerRequirementPolicy: Sendable, Equatable {
    public static let agentEntitlement = "io.thand.agent.privileged-broker-client"
    public static let notifierEntitlement = "io.thand.agent.privileged-broker-notifier"
    public static let brokerEntitlement = "io.thand.agent.privileged-broker-server"

    public let agentRequirement: String
    public let notifierRequirement: String
    public let brokerRequirement: String
    public let insecureDevMode: Bool
    public let requirePeerEntitlements: Bool

    public init(config: BrokerConfig) {
        self.agentRequirement = config.agentPeerRequirement
        self.notifierRequirement = config.notifierPeerRequirement
        self.brokerRequirement = config.brokerPeerRequirement
        self.insecureDevMode = config.insecureDevMode
        self.requirePeerEntitlements = config.requirePeerEntitlements
    }

    public var combinedClientRequirement: String? {
        guard !insecureDevMode else {
            return nil
        }
        return "\(agentRequirement)|\(notifierRequirement)"
    }

    public func signingIdentifier(for role: PeerRole) -> String? {
        guard !insecureDevMode else {
            return nil
        }

        switch role {
        case .agent:
            return agentRequirement
        case .notifier:
            return notifierRequirement
        case .broker:
            return brokerRequirement
        }
    }

    public func sessionRequirement(for role: PeerRole) -> XPCPeerRequirement? {
        guard let signingIdentifier = signingIdentifier(for: role) else {
            return nil
        }
        return .isFromSameTeam(andMatchesSigningIdentifier: signingIdentifier)
    }

    public func senderSatisfies(_ message: XPCReceivedMessage, role: PeerRole) -> Bool {
        guard !insecureDevMode else {
            return true
        }

        guard let signingIdentifier = signingIdentifier(for: role) else {
            return false
        }

        let identityRequirement = XPCPeerRequirement.isFromSameTeam(andMatchesSigningIdentifier: signingIdentifier)
        guard message.senderSatisfies(identityRequirement) else {
            guard !requirePeerEntitlements else {
                return false
            }
            if let sameTeamRequirement = relaxedSameTeamRequirement() {
                return message.senderSatisfies(sameTeamRequirement)
            }
            return false
        }
        guard requirePeerEntitlements else {
            return true
        }
        let entitlementRequirement = XPCPeerRequirement.hasEntitlement(entitlement(for: role))
        return message.senderSatisfies(entitlementRequirement)
    }

    public func entitlement(for role: PeerRole) -> String {
        switch role {
        case .agent:
            return Self.agentEntitlement
        case .notifier:
            return Self.notifierEntitlement
        case .broker:
            return Self.brokerEntitlement
        }
    }

    private func relaxedSameTeamRequirement() -> XPCPeerRequirement? {
        do {
            let requirement = try ProcessCodeRequirement.allOf {
                TeamIdentifierMatchesCurrentProcess()
            }
            return .codeRequirement(requirement)
        } catch {
            return nil
        }
    }
}
