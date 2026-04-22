import Foundation
import PrivilegeServicesShared

do {
    let config = BrokerConfig.fromEnvironment()
    brokerLog("starting broker daemon", fields: [
        "service_label": config.serviceLabel,
        "state_dir": config.stateDirectoryURL.path,
        "sudoers_dir": config.sudoersDirectoryURL.path,
        "insecure_dev_mode": config.insecureDevMode ? "true" : "false",
        "require_peer_entitlements": config.requirePeerEntitlements ? "true" : "false",
        "agent_requirement": config.agentPeerRequirement,
        "notifier_requirement": config.notifierPeerRequirement,
        "broker_requirement": config.brokerPeerRequirement
    ])
    let service = try PrivilegeBrokerService(config: config)
    let controlServer = XPCBrokerServer(service: service)
    let notifierServer = NotifierXPCServer(service: service)
    try controlServer.start()
    try notifierServer.start()
    brokerLog("broker daemon activated", fields: [
        "service_label": config.serviceLabel,
        "notifier_service_label": config.notifierServiceLabel
    ])
    RunLoop.main.run()
} catch {
    fputs("thand-macos-privilege-brokerd failed: \(error)\n", stderr)
    exit(1)
}
