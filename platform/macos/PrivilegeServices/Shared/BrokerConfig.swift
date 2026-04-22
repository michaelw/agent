import Foundation

public struct BrokerConfig: Sendable, Equatable {
    public static let defaultServiceLabel = "io.thand.agent.privilege-broker"
    public static let defaultStateDirectory = "/var/db/thand/local-privilege-broker"
    public static let defaultSudoersDirectory = "/etc/sudoers.d"
    public static let defaultVisudoPath = "/usr/sbin/visudo"
    public static let defaultAgentRequirement = "io.thand.agent"
    public static let defaultNotifierRequirement = "io.thand.agent.privilege-notifier"
    public static let defaultBrokerRequirement = "io.thand.agent.privilege-broker"

    public let stateDirectoryURL: URL
    public let sudoersDirectoryURL: URL
    public let visudoPath: String
    public let serviceLabel: String
    public let insecureDevMode: Bool
    public let requirePeerEntitlements: Bool
    public let agentPeerRequirement: String
    public let notifierPeerRequirement: String
    public let brokerPeerRequirement: String

    public var notifierServiceLabel: String {
        "\(serviceLabel).notifier"
    }

    public init(
        stateDirectoryURL: URL = URL(fileURLWithPath: Self.defaultStateDirectory),
        sudoersDirectoryURL: URL = URL(fileURLWithPath: Self.defaultSudoersDirectory),
        visudoPath: String = Self.defaultVisudoPath,
        serviceLabel: String = Self.defaultServiceLabel,
        insecureDevMode: Bool = false,
        requirePeerEntitlements: Bool = true,
        agentPeerRequirement: String = Self.defaultAgentRequirement,
        notifierPeerRequirement: String = Self.defaultNotifierRequirement,
        brokerPeerRequirement: String = Self.defaultBrokerRequirement
    ) {
        self.stateDirectoryURL = stateDirectoryURL
        self.sudoersDirectoryURL = sudoersDirectoryURL
        self.visudoPath = visudoPath
        self.serviceLabel = serviceLabel
        self.insecureDevMode = insecureDevMode
        self.requirePeerEntitlements = requirePeerEntitlements
        self.agentPeerRequirement = agentPeerRequirement
        self.notifierPeerRequirement = notifierPeerRequirement
        self.brokerPeerRequirement = brokerPeerRequirement
    }

    public static func fromEnvironment(processInfo: ProcessInfo = .processInfo) -> BrokerConfig {
        let environment = processInfo.environment

        let stateDirectory = environment["THAND_PRIVILEGE_BROKER_STATE_DIR"] ?? Self.defaultStateDirectory
        let sudoersDirectory = environment["THAND_PRIVILEGE_BROKER_SUDOERS_DIR"] ?? Self.defaultSudoersDirectory
        let visudoPath = environment["THAND_PRIVILEGE_BROKER_VISUDO_PATH"] ?? Self.defaultVisudoPath
        let serviceLabel = environment["THAND_PRIVILEGE_BROKER_SERVICE_LABEL"] ?? Self.defaultServiceLabel
        let insecureDevMode = environment["THAND_PRIVILEGE_BROKER_INSECURE_DEV_MODE"] == "1"
        let requirePeerEntitlements = environment["THAND_PRIVILEGE_BROKER_REQUIRE_PEER_ENTITLEMENTS"] != "0"
        let agentRequirement = environment["THAND_PRIVILEGE_BROKER_AGENT_REQUIREMENT"] ?? Self.defaultAgentRequirement
        let notifierRequirement = environment["THAND_PRIVILEGE_BROKER_NOTIFIER_REQUIREMENT"] ?? Self.defaultNotifierRequirement
        let brokerRequirement = environment["THAND_PRIVILEGE_BROKER_SERVER_REQUIREMENT"] ?? Self.defaultBrokerRequirement

        return BrokerConfig(
            stateDirectoryURL: URL(fileURLWithPath: stateDirectory),
            sudoersDirectoryURL: URL(fileURLWithPath: sudoersDirectory),
            visudoPath: visudoPath,
            serviceLabel: serviceLabel,
            insecureDevMode: insecureDevMode,
            requirePeerEntitlements: requirePeerEntitlements,
            agentPeerRequirement: agentRequirement,
            notifierPeerRequirement: notifierRequirement,
            brokerPeerRequirement: brokerRequirement
        )
    }
}
