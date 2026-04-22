#!/bin/sh
set -eu

REPO_ROOT=$(CDPATH= cd -- "$(dirname "$0")/.." && pwd)
PRIVILEGE_SERVICES_ROOT=${PRIVILEGE_SERVICES_ROOT:-"$REPO_ROOT/platform/macos/PrivilegeServices"}
PRIVILEGE_SERVICES_PROJECT_SPEC=${PRIVILEGE_SERVICES_PROJECT_SPEC:-"$PRIVILEGE_SERVICES_ROOT/project.yml"}
PRIVILEGE_SERVICES_PROJECT_FILE=${PRIVILEGE_SERVICES_PROJECT_FILE:-"$PRIVILEGE_SERVICES_ROOT/ThandPrivilegeServices.xcodeproj"}
PRIVILEGE_SERVICES_SCHEME=${PRIVILEGE_SERVICES_SCHEME:-ThandPrivilegeServices}
DERIVED_DATA_PATH=${DERIVED_DATA_PATH:-"$REPO_ROOT/.build/macos/DerivedData"}
PRIVILEGE_SERVICES_SOURCE_PACKAGES_DIR=${PRIVILEGE_SERVICES_SOURCE_PACKAGES_DIR:-"$REPO_ROOT/.build/macos/SourcePackages"}
LOCALBROKER_SWIFT_GENERATED_DIR=${LOCALBROKER_SWIFT_GENERATED_DIR:-"$PRIVILEGE_SERVICES_ROOT/Generated/LocalBroker"}
LOCALBROKER_GO_GENERATED_DIR=${LOCALBROKER_GO_GENERATED_DIR:-"$REPO_ROOT/internal/localbroker/proto/localbroker/v1"}
LOCALBROKER_CODEGEN_TOOLS_ROOT=${LOCALBROKER_CODEGEN_TOOLS_ROOT:-"$REPO_ROOT/.build/macos/CodegenTools"}
LOCALBROKER_CODEGEN_BIN_DIR=${LOCALBROKER_CODEGEN_BIN_DIR:-"$LOCALBROKER_CODEGEN_TOOLS_ROOT/bin"}
LOCALBROKER_CODEGEN_SRC_DIR=${LOCALBROKER_CODEGEN_SRC_DIR:-"$LOCALBROKER_CODEGEN_TOOLS_ROOT/src"}
LOCALBROKER_PROTOC_GEN_GO_VERSION=${LOCALBROKER_PROTOC_GEN_GO_VERSION:-v1.36.11}
LOCALBROKER_PROTOC_GEN_GO_GRPC_VERSION=${LOCALBROKER_PROTOC_GEN_GO_GRPC_VERSION:-v1.6.1}
LOCALBROKER_SWIFT_PROTOBUF_VERSION=${LOCALBROKER_SWIFT_PROTOBUF_VERSION:-1.37.0}
LOCALBROKER_GRPC_SWIFT_PROTOBUF_VERSION=${LOCALBROKER_GRPC_SWIFT_PROTOBUF_VERSION:-2.3.0}
BUILD_CONFIGURATION=${BUILD_CONFIGURATION:-Debug}
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
