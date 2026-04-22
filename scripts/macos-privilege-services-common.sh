#!/bin/sh
set -eu

REPO_ROOT=$(CDPATH= cd -- "$(dirname "$0")/.." && pwd)
PRIVILEGE_SERVICES_ROOT=${PRIVILEGE_SERVICES_ROOT:-"$REPO_ROOT/platform/macos/PrivilegeServices"}
PRIVILEGE_SERVICES_PROJECT_SPEC=${PRIVILEGE_SERVICES_PROJECT_SPEC:-"$PRIVILEGE_SERVICES_ROOT/project.yml"}
PRIVILEGE_SERVICES_PROJECT_FILE=${PRIVILEGE_SERVICES_PROJECT_FILE:-"$PRIVILEGE_SERVICES_ROOT/ThandPrivilegeServices.xcodeproj"}
PRIVILEGE_SERVICES_SCHEME=${PRIVILEGE_SERVICES_SCHEME:-ThandPrivilegeServices}
DERIVED_DATA_PATH=${DERIVED_DATA_PATH:-"$REPO_ROOT/.build/macos/DerivedData"}
PRIVILEGE_SERVICES_BUILD_ROOT=${PRIVILEGE_SERVICES_BUILD_ROOT:-"$REPO_ROOT/.build/macos/PrivilegeServices"}
PRIVILEGE_SERVICES_SOURCE_PACKAGES_DIR=${PRIVILEGE_SERVICES_SOURCE_PACKAGES_DIR:-"$REPO_ROOT/.build/macos/SourcePackages"}
LOCALBROKER_SWIFT_GENERATED_DIR=${LOCALBROKER_SWIFT_GENERATED_DIR:-"$PRIVILEGE_SERVICES_ROOT/Generated/LocalBroker"}
LOCALBROKER_GO_GENERATED_DIR=${LOCALBROKER_GO_GENERATED_DIR:-"$REPO_ROOT/internal/localbroker/proto/localbroker/v1"}
LOCALBROKER_PROTO_FILE=${LOCALBROKER_PROTO_FILE:-"$REPO_ROOT/proto/localbroker/v1/localbroker.proto"}
LOCALBROKER_CODEGEN_TOOLS_ROOT=${LOCALBROKER_CODEGEN_TOOLS_ROOT:-"$REPO_ROOT/.build/macos/CodegenTools"}
LOCALBROKER_CODEGEN_BIN_DIR=${LOCALBROKER_CODEGEN_BIN_DIR:-"$LOCALBROKER_CODEGEN_TOOLS_ROOT/bin"}
LOCALBROKER_CODEGEN_SRC_DIR=${LOCALBROKER_CODEGEN_SRC_DIR:-"$LOCALBROKER_CODEGEN_TOOLS_ROOT/src"}
LOCALBROKER_PROTOC_GEN_GO_VERSION=${LOCALBROKER_PROTOC_GEN_GO_VERSION:-v1.36.11}
LOCALBROKER_PROTOC_GEN_GO_GRPC_VERSION=${LOCALBROKER_PROTOC_GEN_GO_GRPC_VERSION:-v1.6.1}
LOCALBROKER_SWIFT_PROTOBUF_VERSION=${LOCALBROKER_SWIFT_PROTOBUF_VERSION:-1.37.0}
LOCALBROKER_GRPC_SWIFT_PROTOBUF_VERSION=${LOCALBROKER_GRPC_SWIFT_PROTOBUF_VERSION:-2.3.0}
BUILD_CONFIGURATION=${BUILD_CONFIGURATION:-Debug}
SERVICE_LABEL=${SERVICE_LABEL:-io.thand.agent.privilege-broker}
APP_BUNDLE_ID=${APP_BUNDLE_ID:-io.thand.agent.privilege-services}
BROKER_SIGNING_IDENTIFIER=${BROKER_SIGNING_IDENTIFIER:-io.thand.agent.privilege-broker}
BROKER_CTL_SIGNING_IDENTIFIER=${BROKER_CTL_SIGNING_IDENTIFIER:-io.thand.agent}
AGENT_SIGNING_IDENTIFIER=${AGENT_SIGNING_IDENTIFIER:-io.thand.agent}
STATE_DIR=${STATE_DIR:-/var/db/thand/local-privilege-broker}
REQUIRE_PEER_ENTITLEMENTS=${REQUIRE_PEER_ENTITLEMENTS:-1}
INSTALL_APP_PATH=${INSTALL_APP_PATH:-/Applications/ThandPrivilegeServices.app}
BROKER_CTL_INSTALL_DIR=${BROKER_CTL_INSTALL_DIR:-/Library/Application Support/Thand/PrivilegeBroker/bin}
STAGED_APP_NAME=ThandPrivilegeServices.app
LOGIN_ITEM_NAME=ThandPrivilegeNotifier.app
DAEMON_BINARY_NAME=ThandPrivilegeBrokerDaemon
BROKER_CTL_BINARY_NAME=thand-macos-privilege-brokerctl
HOST_ARCH=$(uname -m)
XCODEBUILD_BUILD_DESTINATION=${XCODEBUILD_BUILD_DESTINATION:-"platform=macOS,arch=$HOST_ARCH"}
XCODEBUILD_TEST_DESTINATION=${XCODEBUILD_TEST_DESTINATION:-"platform=macOS,arch=$HOST_ARCH"}

ensure_command() {
    if ! command -v "$1" >/dev/null 2>&1; then
        echo "missing required command: $1" >&2
        exit 1
    fi
}

require_darwin() {
    if [ "$(uname -s)" != "Darwin" ]; then
        echo "macOS privilege services can only be built on Darwin hosts" >&2
        exit 1
    fi
}

generate_privilege_services_project() {
    require_darwin
    ensure_command xcodegen
    xcodegen generate \
        --use-cache \
        --spec "$PRIVILEGE_SERVICES_PROJECT_SPEC" \
        --project "$PRIVILEGE_SERVICES_ROOT" \
        >/dev/null
}

resolve_privilege_services_packages() {
    require_darwin
    xcodebuild \
        -project "$PRIVILEGE_SERVICES_PROJECT_FILE" \
        -scheme "$PRIVILEGE_SERVICES_SCHEME" \
        -resolvePackageDependencies \
        -clonedSourcePackagesDirPath "$PRIVILEGE_SERVICES_SOURCE_PACKAGES_DIR" \
        >/dev/null
}

ensure_codegen_source_checkout() {
    name=$1
    repo_url=$2
    version=$3
    checkout_path="$LOCALBROKER_CODEGEN_SRC_DIR/$name-$version"

    mkdir -p "$LOCALBROKER_CODEGEN_SRC_DIR"
    if [ -d "$checkout_path/.git" ]; then
        current_tag=$(git -C "$checkout_path" describe --tags --exact-match 2>/dev/null || true)
        if [ "$current_tag" = "$version" ]; then
            printf '%s\n' "$checkout_path"
            return
        fi
        rm -rf "$checkout_path"
    fi

    git clone --depth 1 --branch "$version" "$repo_url" "$checkout_path" >/dev/null 2>&1
    printf '%s\n' "$checkout_path"
}

bootstrap_localbroker_codegen_tools() {
    require_darwin
    ensure_command buf
    ensure_command go
    ensure_command swift
    ensure_command git

    mkdir -p "$LOCALBROKER_CODEGEN_BIN_DIR"

    GOBIN="$LOCALBROKER_CODEGEN_BIN_DIR" GOEXPERIMENT=jsonv2 \
        go install "google.golang.org/protobuf/cmd/protoc-gen-go@$LOCALBROKER_PROTOC_GEN_GO_VERSION"
    GOBIN="$LOCALBROKER_CODEGEN_BIN_DIR" GOEXPERIMENT=jsonv2 \
        go install "google.golang.org/grpc/cmd/protoc-gen-go-grpc@$LOCALBROKER_PROTOC_GEN_GO_GRPC_VERSION"

    swift_protobuf_checkout=$(ensure_codegen_source_checkout "swift-protobuf" "https://github.com/apple/swift-protobuf.git" "$LOCALBROKER_SWIFT_PROTOBUF_VERSION")
    grpc_swift_protobuf_checkout=$(ensure_codegen_source_checkout "grpc-swift-protobuf" "https://github.com/grpc/grpc-swift-protobuf.git" "$LOCALBROKER_GRPC_SWIFT_PROTOBUF_VERSION")

    swift build \
        --package-path "$swift_protobuf_checkout" \
        --product protoc-gen-swift \
        --configuration release \
        --scratch-path "$LOCALBROKER_CODEGEN_TOOLS_ROOT/build/swift-protobuf" \
        >/dev/null
    cp \
        "$LOCALBROKER_CODEGEN_TOOLS_ROOT/build/swift-protobuf/release/protoc-gen-swift" \
        "$LOCALBROKER_CODEGEN_BIN_DIR/protoc-gen-swift"

    swift build \
        --package-path "$grpc_swift_protobuf_checkout" \
        --product protoc-gen-grpc-swift-2 \
        --configuration release \
        --scratch-path "$LOCALBROKER_CODEGEN_TOOLS_ROOT/build/grpc-swift-protobuf" \
        >/dev/null
    cp \
        "$LOCALBROKER_CODEGEN_TOOLS_ROOT/build/grpc-swift-protobuf/release/protoc-gen-grpc-swift-2" \
        "$LOCALBROKER_CODEGEN_BIN_DIR/protoc-gen-grpc-swift-2"
}

generate_localbroker_grpc_sources() {
    require_darwin
    bootstrap_localbroker_codegen_tools

    rm -rf "$LOCALBROKER_GO_GENERATED_DIR" "$LOCALBROKER_SWIFT_GENERATED_DIR"
    mkdir -p "$LOCALBROKER_GO_GENERATED_DIR" "$LOCALBROKER_SWIFT_GENERATED_DIR"

    PATH="$LOCALBROKER_CODEGEN_BIN_DIR:$PATH" \
        buf generate --template "$REPO_ROOT/buf.gen.yaml" "$REPO_ROOT"
}

verify_localbroker_grpc_sources() {
    require_darwin
    bootstrap_localbroker_codegen_tools

    temp_root=$(mktemp -d)
    trap 'rm -rf "$temp_root"' EXIT INT TERM

    temp_template="$temp_root/buf.gen.yaml"
    cat >"$temp_template" <<EOF
version: v2
plugins:
  - local: protoc-gen-go
    out: $temp_root/internal/localbroker/proto
    opt:
      - paths=source_relative
  - local: protoc-gen-go-grpc
    out: $temp_root/internal/localbroker/proto
    opt:
      - paths=source_relative
  - local: protoc-gen-swift
    out: $temp_root/platform/macos/PrivilegeServices/Generated/LocalBroker
    opt:
      - Visibility=Public
      - FileNaming=DropPath
  - local: protoc-gen-grpc-swift-2
    out: $temp_root/platform/macos/PrivilegeServices/Generated/LocalBroker
    opt:
      - Visibility=Public
      - FileNaming=DropPath
      - Client=true
      - Server=true
EOF

    PATH="$LOCALBROKER_CODEGEN_BIN_DIR:$PATH" \
        buf generate \
        --template "$temp_template" \
        "$REPO_ROOT"

    diff -ru "$LOCALBROKER_GO_GENERATED_DIR" "$temp_root/internal/localbroker/proto/localbroker/v1" >/dev/null
    diff -ru "$LOCALBROKER_SWIFT_GENERATED_DIR" "$temp_root/platform/macos/PrivilegeServices/Generated/LocalBroker" >/dev/null
}

run_unsigned_xcodebuild() {
    action=$1
    shift

    xcodebuild \
        -project "$PRIVILEGE_SERVICES_PROJECT_FILE" \
        -scheme "$PRIVILEGE_SERVICES_SCHEME" \
        -configuration "$BUILD_CONFIGURATION" \
        -derivedDataPath "$DERIVED_DATA_PATH" \
        CODE_SIGNING_ALLOWED=NO \
        CODE_SIGNING_REQUIRED=NO \
        "$action" \
        "$@"
}

privilege_services_products_dir() {
    printf '%s\n' "$DERIVED_DATA_PATH/Build/Products/$BUILD_CONFIGURATION"
}

find_apple_development_identity() {
    if [ -z "${APPLE_TEAM_ID:-}" ]; then
        echo "APPLE_TEAM_ID must be set for Apple Development signing" >&2
        exit 1
    fi

    identity=$(find_matching_codesigning_identity "Apple Development")
    if [ -z "$identity" ]; then
        echo "unable to locate an Apple Development signing identity for team ${APPLE_TEAM_ID}" >&2
        exit 1
    fi
    printf '%s\n' "$identity"
}

find_developer_id_application_identity() {
    if [ -z "${APPLE_TEAM_ID:-}" ]; then
        echo "APPLE_TEAM_ID must be set for Developer ID signing" >&2
        exit 1
    fi

    identity=$(find_matching_codesigning_identity "Developer ID Application")
    if [ -z "$identity" ]; then
        echo "unable to locate a Developer ID Application identity for team ${APPLE_TEAM_ID}" >&2
        exit 1
    fi
    printf '%s\n' "$identity"
}

find_developer_id_installer_identity() {
    if [ -z "${APPLE_TEAM_ID:-}" ]; then
        echo "APPLE_TEAM_ID must be set for Developer ID installer signing" >&2
        exit 1
    fi

    identity=$(find_matching_codesigning_identity "Developer ID Installer")
    if [ -z "$identity" ]; then
        echo "unable to locate a Developer ID Installer identity for team ${APPLE_TEAM_ID}" >&2
        exit 1
    fi
    printf '%s\n' "$identity"
}

find_matching_codesigning_identity() {
    label_prefix=$1

    security find-identity -v -p codesigning \
        | while IFS= read -r line; do
            if ! printf '%s\n' "$line" | grep -F "\"$label_prefix" >/dev/null 2>&1; then
                continue
            fi

            identity_hash=$(printf '%s\n' "$line" | sed -E 's/.* ([A-F0-9]{40}) \".*/\1/')
            identity_label=$(printf '%s\n' "$line" | sed -E 's/.* \"([^\"]+)\".*/\1/')

            if printf '%s\n' "$identity_label" | grep -F "(${APPLE_TEAM_ID})" >/dev/null 2>&1; then
                printf '%s\n' "$identity_hash"
                break
            fi

            cert_file=$(mktemp)
            security find-certificate -p -c "$identity_label" ~/Library/Keychains/login.keychain-db >"$cert_file" 2>/dev/null || true
            if [ -s "$cert_file" ]; then
                cert_subject=$(openssl x509 -in "$cert_file" -noout -subject -nameopt RFC2253 2>/dev/null || true)
                rm -f "$cert_file"
                if printf '%s\n' "$cert_subject" | grep -F "OU=${APPLE_TEAM_ID}" >/dev/null 2>&1; then
                    printf '%s\n' "$identity_hash"
                    break
                fi
            else
                rm -f "$cert_file"
            fi
        done
}

render_daemon_plist() {
    output_path=$1

    mkdir -p "$(dirname "$output_path")"
    sed \
        -e "s|__SERVICE_LABEL__|$SERVICE_LABEL|g" \
        -e "s|__STATE_DIR__|$STATE_DIR|g" \
        -e "s|__INSECURE_DEV_MODE__|0|g" \
        -e "s|__REQUIRE_PEER_ENTITLEMENTS__|$REQUIRE_PEER_ENTITLEMENTS|g" \
        "$PRIVILEGE_SERVICES_ROOT/Packaging/LaunchDaemons/io.thand.agent.privilege-broker.plist.template" \
        >"$output_path"
}

prepare_stage_root() {
    stage_root=$1
    rm -rf "$stage_root"
    mkdir -p \
        "$stage_root/Applications" \
        "$stage_root/Library/Application Support/Thand/PrivilegeBroker/bin"
}

stage_unsigned_payload() {
    stage_root=$1
    products_dir=$(privilege_services_products_dir)
    app_path="$stage_root/Applications/$STAGED_APP_NAME"
    login_item_path="$app_path/Contents/Library/LoginItems/$LOGIN_ITEM_NAME"
    daemon_binary_path="$app_path/Contents/Resources/$DAEMON_BINARY_NAME"
    daemon_plist_path="$app_path/Contents/Library/LaunchDaemons/$SERVICE_LABEL.plist"

    if [ ! -d "$products_dir/$STAGED_APP_NAME" ]; then
        echo "missing app product at $products_dir/$STAGED_APP_NAME" >&2
        exit 1
    fi
    if [ ! -d "$products_dir/$LOGIN_ITEM_NAME" ]; then
        echo "missing login item product at $products_dir/$LOGIN_ITEM_NAME" >&2
        exit 1
    fi
    if [ ! -x "$products_dir/$DAEMON_BINARY_NAME" ]; then
        echo "missing daemon product at $products_dir/$DAEMON_BINARY_NAME" >&2
        exit 1
    fi
    if [ ! -x "$products_dir/$BROKER_CTL_BINARY_NAME" ]; then
        echo "missing brokerctl product at $products_dir/$BROKER_CTL_BINARY_NAME" >&2
        exit 1
    fi

    prepare_stage_root "$stage_root"

    ditto "$products_dir/$STAGED_APP_NAME" "$app_path"
    mkdir -p "$(dirname "$login_item_path")" "$(dirname "$daemon_binary_path")"
    ditto "$products_dir/$LOGIN_ITEM_NAME" "$login_item_path"
    install -m 0755 "$products_dir/$DAEMON_BINARY_NAME" "$daemon_binary_path"
    install -m 0755 "$products_dir/$BROKER_CTL_BINARY_NAME" \
        "$stage_root/Library/Application Support/Thand/PrivilegeBroker/bin/$BROKER_CTL_BINARY_NAME"
    render_daemon_plist "$daemon_plist_path"
}

codesign_component() {
    identity=$1
    entitlements=$2
    path=$3
    hardened_runtime=$4
    signing_identifier=$5

    if [ "$hardened_runtime" = "1" ]; then
        runtime_flags="--options runtime --timestamp"
    else
        runtime_flags=""
    fi

    if [ -n "$entitlements" ]; then
        # shellcheck disable=SC2086
        codesign --force --sign "$identity" $runtime_flags --identifier "$signing_identifier" --entitlements "$entitlements" "$path"
        return
    fi

    # shellcheck disable=SC2086
    codesign --force --sign "$identity" $runtime_flags --identifier "$signing_identifier" "$path"
}

sign_staged_payload() {
    stage_root=$1
    identity=$2
    hardened_runtime=$3
    include_peer_entitlements=$4

    app_path="$stage_root/Applications/$STAGED_APP_NAME"
    login_item_path="$app_path/Contents/Library/LoginItems/$LOGIN_ITEM_NAME"
    daemon_binary_path="$app_path/Contents/Resources/$DAEMON_BINARY_NAME"
    brokerctl_path="$stage_root/Library/Application Support/Thand/PrivilegeBroker/bin/$BROKER_CTL_BINARY_NAME"

    daemon_entitlements=""
    login_item_entitlements=""
    brokerctl_entitlements=""
    if [ "$include_peer_entitlements" = "1" ]; then
        daemon_entitlements="$PRIVILEGE_SERVICES_ROOT/Resources/Daemon/ThandPrivilegeBrokerDaemon.entitlements"
        login_item_entitlements="$PRIVILEGE_SERVICES_ROOT/Resources/LoginItem/ThandPrivilegeNotifier.entitlements"
        brokerctl_entitlements="$PRIVILEGE_SERVICES_ROOT/Resources/Ctl/ThandPrivilegeBrokerCtl.entitlements"
    fi

    codesign_component "$identity" "$daemon_entitlements" "$daemon_binary_path" "$hardened_runtime" "$BROKER_SIGNING_IDENTIFIER"
    codesign_component "$identity" "$login_item_entitlements" "$login_item_path" "$hardened_runtime" "io.thand.agent.privilege-notifier"
    codesign_component "$identity" "$brokerctl_entitlements" "$brokerctl_path" "$hardened_runtime" "$BROKER_CTL_SIGNING_IDENTIFIER"
    codesign_component "$identity" "" "$app_path" "$hardened_runtime" "$APP_BUNDLE_ID"

    codesign --verify --deep --strict --verbose=2 "$app_path" >/dev/null
    codesign --verify --strict --verbose=2 "$brokerctl_path" >/dev/null
}

sign_local_agent_binary() {
    binary_path=$1
    identity=$2

    if [ ! -x "$binary_path" ]; then
        echo "missing local agent binary at $binary_path" >&2
        exit 1
    fi

    codesign_component "$identity" "" "$binary_path" 0 "$AGENT_SIGNING_IDENTIFIER"
    codesign --verify --strict --verbose=2 "$binary_path" >/dev/null
}

verify_staged_layout() {
    stage_root=$1
    app_path="$stage_root/Applications/$STAGED_APP_NAME"
    login_item_path="$app_path/Contents/Library/LoginItems/$LOGIN_ITEM_NAME"
    daemon_binary_path="$app_path/Contents/Resources/$DAEMON_BINARY_NAME"
    daemon_plist_path="$app_path/Contents/Library/LaunchDaemons/$SERVICE_LABEL.plist"
    brokerctl_path="$stage_root/Library/Application Support/Thand/PrivilegeBroker/bin/$BROKER_CTL_BINARY_NAME"

    [ -d "$app_path" ]
    [ -d "$login_item_path" ]
    [ -x "$daemon_binary_path" ]
    [ -f "$daemon_plist_path" ]
    [ -x "$brokerctl_path" ]
}

verify_signed_payload() {
    app_path=$1
    brokerctl_path=$2

    codesign --verify --deep --strict --verbose=2 "$app_path" >/dev/null
    codesign --verify --strict --verbose=2 "$brokerctl_path" >/dev/null
}

normalize_modes_tree() {
    target_path=$1

    find "$target_path" -type d -exec chmod 0755 {} +
    find "$target_path" -type f -perm -111 -exec chmod 0755 {} +
    find "$target_path" -type f ! -perm -111 -exec chmod 0644 {} +
}

normalize_stage_root_modes() {
    stage_root=$1

    normalize_modes_tree "$stage_root/Applications"
    normalize_modes_tree "$stage_root/Library"
}

normalize_installed_payload() {
    chown -R root:wheel "$INSTALL_APP_PATH" "$BROKER_CTL_INSTALL_DIR"
    normalize_modes_tree "$INSTALL_APP_PATH"
    normalize_modes_tree "$BROKER_CTL_INSTALL_DIR"
}

copy_stage_into_system() {
    stage_root=$1

    rm -rf "$INSTALL_APP_PATH"
    ditto "$stage_root/Applications/$STAGED_APP_NAME" "$INSTALL_APP_PATH"

    install -d "$BROKER_CTL_INSTALL_DIR"
    install -m 0755 \
        "$stage_root/Library/Application Support/Thand/PrivilegeBroker/bin/$BROKER_CTL_BINARY_NAME" \
        "$BROKER_CTL_INSTALL_DIR/$BROKER_CTL_BINARY_NAME"
}

run_privilege_services_as_user() {
    command=$1

    if [ -z "${SUDO_USER:-}" ] || [ "$SUDO_USER" = "root" ]; then
        echo "SUDO_USER is required to run ThandPrivilegeServices $command as the desktop user" >&2
        exit 1
    fi

    sudo -u "$SUDO_USER" "$INSTALL_APP_PATH/Contents/MacOS/ThandPrivilegeServices" "$command"
}
