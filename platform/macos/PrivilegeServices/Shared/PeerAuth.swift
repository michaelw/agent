import Foundation
import LightweightCodeRequirements
import Security
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

    @available(macOS 26.0, *)
    public func sessionRequirement(for role: PeerRole) -> XPCPeerRequirement? {
        guard let signingIdentifier = signingIdentifier(for: role) else {
            return nil
        }
        return .isFromSameTeam(andMatchesSigningIdentifier: signingIdentifier)
    }

    @available(macOS 26.0, *)
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

    public func nsxpcRequirementString(for role: PeerRole) throws -> String? {
        guard !insecureDevMode else {
            return nil
        }

        guard let signingIdentifier = signingIdentifier(for: role) else {
            return nil
        }

        let teamIdentifier = try currentProcessTeamIdentifier()
        return """
        anchor apple generic and identifier "\(escapeRequirementValue(signingIdentifier))" and certificate leaf[subject.OU] = "\(escapeRequirementValue(teamIdentifier))"
        """
    }

    public func connectionSatisfies(_ connection: NSXPCConnection, role: PeerRole) -> Bool {
        do {
            return try validate(connection, role: role)
        } catch {
            return false
        }
    }

    public func validate(_ connection: NSXPCConnection, role: PeerRole) throws -> Bool {
        guard !insecureDevMode else {
            return true
        }

        guard let requirementString = try nsxpcRequirementString(for: role) else {
            return false
        }

        let requirement = try makeRequirement(from: requirementString)
        let code = try copyCode(for: connection.processIdentifier)
        let validityStatus = SecCodeCheckValidity(code, SecCSFlags(), requirement)
        guard validityStatus == errSecSuccess else {
            throw PeerValidationError.invalidCodeSignature(message: copySecurityMessage(for: validityStatus))
        }

        guard requirePeerEntitlements else {
            return true
        }

        let signingInformation = try copySigningInformation(for: code)
        guard let entitlements = signingInformation[kSecCodeInfoEntitlementsDict as String] as? [String: Any] else {
            throw PeerValidationError.missingEntitlement(entitlement(for: role))
        }
        guard entitlements[entitlement(for: role)] != nil else {
            throw PeerValidationError.missingEntitlement(entitlement(for: role))
        }

        return true
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

    @available(macOS 26.0, *)
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

    private func currentProcessTeamIdentifier() throws -> String {
        var selfCode: SecCode?
        let selfStatus = SecCodeCopySelf(SecCSFlags(), &selfCode)
        guard selfStatus == errSecSuccess, let selfCode else {
            throw PeerValidationError.currentProcessTeamIdentifierUnavailable(message: copySecurityMessage(for: selfStatus))
        }

        let signingInformation = try copySigningInformation(for: selfCode)
        guard let teamIdentifier = signingInformation[kSecCodeInfoTeamIdentifier as String] as? String,
              !teamIdentifier.isEmpty else {
            throw PeerValidationError.currentProcessTeamIdentifierUnavailable(message: "missing team identifier")
        }
        return teamIdentifier
    }

    private func makeRequirement(from requirementString: String) throws -> SecRequirement {
        var requirement: SecRequirement?
        let status = SecRequirementCreateWithString(requirementString as CFString, SecCSFlags(), &requirement)
        guard status == errSecSuccess, let requirement else {
            throw PeerValidationError.invalidRequirement(message: copySecurityMessage(for: status))
        }
        return requirement
    }

    private func copyCode(for processIdentifier: pid_t) throws -> SecCode {
        let attributes = [kSecGuestAttributePid as String: processIdentifier] as CFDictionary
        var code: SecCode?
        let status = SecCodeCopyGuestWithAttributes(nil, attributes, SecCSFlags(), &code)
        guard status == errSecSuccess, let code else {
            throw PeerValidationError.invalidPeer(message: copySecurityMessage(for: status))
        }
        return code
    }

    private func copySigningInformation(for code: SecCode) throws -> [String: Any] {
        var staticCode: SecStaticCode?
        let copyStaticStatus = SecCodeCopyStaticCode(code, SecCSFlags(), &staticCode)
        guard copyStaticStatus == errSecSuccess, let staticCode else {
            throw PeerValidationError.invalidPeer(message: copySecurityMessage(for: copyStaticStatus))
        }

        var signingInformation: CFDictionary?
        let status = SecCodeCopySigningInformation(staticCode, SecCSFlags(rawValue: kSecCSSigningInformation), &signingInformation)
        guard status == errSecSuccess,
              let info = signingInformation as? [String: Any] else {
            throw PeerValidationError.invalidPeer(message: copySecurityMessage(for: status))
        }
        return info
    }

    private func copySecurityMessage(for status: OSStatus) -> String {
        (SecCopyErrorMessageString(status, nil) as String?) ?? "OSStatus \(status)"
    }

    private func escapeRequirementValue(_ rawValue: String) -> String {
        rawValue.replacingOccurrences(of: "\\", with: "\\\\")
            .replacingOccurrences(of: "\"", with: "\\\"")
    }
}

public enum PeerValidationError: Error, CustomStringConvertible {
    case currentProcessTeamIdentifierUnavailable(message: String)
    case invalidRequirement(message: String)
    case invalidPeer(message: String)
    case invalidCodeSignature(message: String)
    case missingEntitlement(String)

    public var description: String {
        switch self {
        case .currentProcessTeamIdentifierUnavailable(let message):
            return "unable to resolve current process team identifier: \(message)"
        case .invalidRequirement(let message):
            return "invalid code-signing requirement: \(message)"
        case .invalidPeer(let message):
            return "unable to inspect peer identity: \(message)"
        case .invalidCodeSignature(let message):
            return "peer code-signing validation failed: \(message)"
        case .missingEntitlement(let entitlement):
            return "peer is missing required entitlement \(entitlement)"
        }
    }
}
