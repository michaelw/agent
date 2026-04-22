import Foundation
import PrivilegeServicesShared
import UserNotifications

private struct CLIArguments {
    var serviceLabel: String?
    var insecureDevMode = false
}

private func parseArguments() throws -> CLIArguments {
    var parsed = CLIArguments()
    var index = 1
    let arguments = CommandLine.arguments

    while index < arguments.count {
        switch arguments[index] {
        case "--service-label":
            index += 1
            guard index < arguments.count else {
                throw BrokerServiceError.invalidRequest("--service-label requires a value")
            }
            parsed.serviceLabel = arguments[index]
            index += 1
        case "--insecure-dev-mode":
            parsed.insecureDevMode = true
            index += 1
        default:
            throw BrokerServiceError.invalidRequest("unsupported argument \(arguments[index])")
        }
    }

    return parsed
}

private func requestNotificationAuthorization() async throws {
    _ = try await UNUserNotificationCenter.current().requestAuthorization(options: [.alert, .sound, .badge])
}

private func postNotification(for event: BrokerEvent) async {
    let content = UNMutableNotificationContent()
    content.title = NotificationFormatter.title
    content.body = NotificationFormatter.body(for: event)
    content.sound = .default

    let request = UNNotificationRequest(
        identifier: "thand-privilege-broker-\(event.kind.rawValue)-\(event.brokerHandle)",
        content: content,
        trigger: nil
    )

    do {
        try await UNUserNotificationCenter.current().add(request)
    } catch {
        fputs("failed to post notification: \(error)\n", stderr)
    }
}

do {
    let arguments = try parseArguments()

    var config = BrokerConfig.fromEnvironment()
    config = BrokerConfig(
        stateDirectoryURL: config.stateDirectoryURL,
        sudoersDirectoryURL: config.sudoersDirectoryURL,
        visudoPath: config.visudoPath,
        serviceLabel: arguments.serviceLabel ?? config.serviceLabel,
        insecureDevMode: arguments.insecureDevMode || config.insecureDevMode,
        requirePeerEntitlements: config.requirePeerEntitlements,
        agentPeerRequirement: config.agentPeerRequirement,
        notifierPeerRequirement: config.notifierPeerRequirement,
        brokerPeerRequirement: config.brokerPeerRequirement
    )

    brokerLog("starting privilege notifier", fields: [
        "service_label": config.serviceLabel,
        "notifier_service_label": config.notifierServiceLabel,
        "insecure_dev_mode": config.insecureDevMode ? "true" : "false"
    ])

    Task {
        brokerLog("requesting notification authorization")
        do {
            try await requestNotificationAuthorization()
            brokerLog("notification authorization request completed")
        } catch {
            fputs("notification authorization failed: \(error)\n", stderr)
            brokerLog("notification authorization failed", fields: [
                "error": String(describing: error)
            ])
        }
    }

    let notifier = NotifierXPCClient(config: config) { event in
        brokerLog("received broker event in notifier", fields: [
            "event_kind": event.kind.rawValue,
            "broker_handle": event.brokerHandle,
            "username": event.username
        ])
        Task {
            await postNotification(for: event)
        }
    }
    let acknowledgement = try notifier.start()
    brokerLog("privilege notifier subscribed to broker events", fields: [
        "username": acknowledgement.username,
        "audit_session_identifier": String(acknowledgement.auditSessionIdentifier)
    ])
    RunLoop.main.run()
} catch {
    fputs("thand-macos-privilege-notifier failed: \(error)\n", stderr)
    exit(1)
}
