import Dispatch
import Foundation
import XPC

public struct BrokerRemoteError: Error, CustomStringConvertible, Sendable, Equatable {
    public let failure: BrokerFailure

    public init(failure: BrokerFailure) {
        self.failure = failure
    }

    public var code: BrokerFailureCode {
        failure.code
    }

    public var description: String {
        failure.message
    }
}

private func shouldLogCancellation(_ description: String) -> Bool {
    !description.lowercased().contains("session manually canceled")
}

public final class XPCBrokerClient: @unchecked Sendable {
    private let config: BrokerConfig
    private let peerRequirementPolicy: PeerRequirementPolicy

    public init(config: BrokerConfig, peerRequirementPolicy: PeerRequirementPolicy? = nil) {
        self.config = config
        self.peerRequirementPolicy = peerRequirementPolicy ?? PeerRequirementPolicy(config: config)
    }

    public func send(_ request: BrokerControlRequest) throws -> BrokerControlResponse {
        let session = try makeSession(incomingMessageHandler: nil)
        defer { session.cancel(reason: "request complete") }

        let reply: BrokerWireMessage = try session.sendSync(BrokerWireMessage(controlRequest: request))
        if let failure = reply.failure {
            throw BrokerRemoteError(failure: failure)
        }
        if let error = reply.error {
            throw XPCTransportError.xpcFailure(error)
        }
        return reply.controlResponse ?? BrokerControlResponse()
    }

    fileprivate func makeSession(
        incomingMessageHandler: (@Sendable (BrokerWireMessage) -> (any Encodable)?)?
    ) throws -> XPCSession {
        let session: XPCSession
        let requirementEnabled = peerRequirementPolicy.sessionRequirement(for: .broker) != nil
        brokerLog("creating broker xpc client session", fields: [
            "service_label": config.serviceLabel,
            "insecure_dev_mode": config.insecureDevMode ? "true" : "false",
            "requirement_enabled": requirementEnabled ? "true" : "false"
        ])
        if let requirement = peerRequirementPolicy.sessionRequirement(for: .broker) {
            session = try XPCSession(
                machService: config.serviceLabel,
                options: [.inactive, .privileged],
                requirement: requirement,
                incomingMessageHandler: incomingMessageHandler,
                cancellationHandler: { richError in
                    guard shouldLogCancellation(richError.debugDescription) else {
                        return
                    }
                    fputs("broker session cancelled: \(richError.debugDescription)\n", stderr)
                }
            )
        } else {
            session = try XPCSession(
                machService: config.serviceLabel,
                options: [.inactive, .privileged],
                incomingMessageHandler: incomingMessageHandler,
                cancellationHandler: { richError in
                    guard shouldLogCancellation(richError.debugDescription) else {
                        return
                    }
                    fputs("broker session cancelled: \(richError.debugDescription)\n", stderr)
                }
            )
        }

        try session.activate()
        brokerLog("activated broker xpc client session", fields: [
            "service_label": config.serviceLabel
        ])
        return session
    }
}

public final class XPCBrokerServer: @unchecked Sendable {
    private let service: PrivilegeBrokerService
    private let queue = DispatchQueue(label: "io.thand.agent.privilege-broker.listener")

    private var listener: XPCListener?

    public init(service: PrivilegeBrokerService) {
        self.service = service
    }

    public func start() throws {
        brokerLog("starting broker xpc listener", fields: [
            "service_label": service.config.serviceLabel,
            "insecure_dev_mode": service.config.insecureDevMode ? "true" : "false"
        ])
        let listener = try XPCListener(
            service: service.config.serviceLabel,
            targetQueue: queue,
            options: [.inactive],
            incomingSessionHandler: { [weak self] request in
                guard let self else {
                    return request.reject(reason: "broker unavailable")
                }

                brokerLog("received incoming xpc session")

                let (decision, _) = request.accept(
                    incomingMessageHandler: { [weak self] (message: XPCReceivedMessage) -> (any Encodable)? in
                        guard let self else {
                            return BrokerWireMessage(error: "broker unavailable")
                        }
                        return self.handleMessage(message: message)
                    },
                    cancellationHandler: { _ in }
                )
                return decision
            }
        )
        self.listener = listener
        try listener.activate()
        brokerLog("activated broker xpc listener", fields: [
            "service_label": service.config.serviceLabel
        ])
    }

    private func handleMessage(message: XPCReceivedMessage) -> (any Encodable)? {
        do {
            let wireMessage: BrokerWireMessage = try message.decode()

            if let controlRequest = wireMessage.controlRequest {
                let senderAllowed = service.peerRequirementPolicy.senderSatisfies(message, role: .agent)
                brokerLog("handling control request", fields: [
                    "operation": controlRequest.operation.rawValue,
                    "sender_allowed": senderAllowed ? "true" : "false"
                ])
                guard senderAllowed else {
                    return BrokerWireMessage(failure: BrokerFailure(
                        code: .peerRejected,
                        message: "agent peer identity check failed"
                    ))
                }
                let response = try service.handle(controlRequest)
                return BrokerWireMessage(controlResponse: response)
            }

            return BrokerWireMessage(failure: BrokerFailure(
                code: .invalidRequest,
                message: "unsupported broker message"
            ))
        } catch let error as BrokerServiceError {
            return BrokerWireMessage(failure: error.failure)
        } catch let error as XPCTransportError {
            let failure: BrokerFailure
            switch error {
            case .invalidMessage(let message):
                failure = BrokerFailure(code: .invalidRequest, message: message)
            case .peerRequirementRejected(let message):
                failure = BrokerFailure(code: .peerRejected, message: message)
            case .xpcFailure(let message):
                failure = BrokerFailure(code: .unavailable, message: message)
            }
            return BrokerWireMessage(failure: failure)
        } catch {
            return BrokerWireMessage(failure: BrokerFailure(
                code: .internalError,
                message: String(describing: error)
            ))
        }
    }
}
