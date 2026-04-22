# Makefile for agent project

# Default executable name
BINARY_NAME=thand
BUILD_DIR=bin

# If UPX is available on the system, we'll use it to compress binaries.
# Customize compression flags via UPX_FLAGS.
UPX_FLAGS ?= --best --lzma --force-macos

GO_BUILD_FLAGS= -ldflags "-s -w"
HOST_OS := $(shell uname -s)
PRIVILEGE_SERVICES_FILES := $(shell find platform/macos/PrivilegeServices -type f ! -path '*/ThandPrivilegeServices.xcodeproj/*' 2>/dev/null)
PRIVILEGE_SERVICES_BUILD_STAMP := .build/macos/PrivilegeServices/.build-stamp
PRIVILEGE_SERVICES_TEST_STAMP := .build/macos/PrivilegeServices/.test-stamp

# Default target - builds the application
all: build

# Initialize and update submodules
submodules:
	git submodule update --init --recursive

# Update submodules to latest version
update-submodules:
	git submodule update --remote --recursive

# Build the application
build: submodules
	go build -o $(BUILD_DIR)/$(BINARY_NAME) .
ifeq ($(HOST_OS),Darwin)
	./scripts/sign-macos-local-agent.sh $(BUILD_DIR)/$(BINARY_NAME)
endif
ifeq ($(HOST_OS),Darwin)
build: build-macos-privilege-services
endif

# Build for multiple platforms
build-all: submodules
	GOOS=linux GOARCH=amd64 GOEXPERIMENT=jsonv2 go build $(GO_BUILD_FLAGS) -o $(BUILD_DIR)/$(BINARY_NAME)-linux-amd64 .
	GOOS=linux GOARCH=arm64 GOEXPERIMENT=jsonv2 go build $(GO_BUILD_FLAGS) -o $(BUILD_DIR)/$(BINARY_NAME)-linux-arm64 .
	GOOS=darwin GOARCH=amd64 GOEXPERIMENT=jsonv2 go build $(GO_BUILD_FLAGS) -o $(BUILD_DIR)/$(BINARY_NAME)-darwin-amd64 .
	GOOS=windows GOARCH=amd64 GOEXPERIMENT=jsonv2 go build $(GO_BUILD_FLAGS) -o $(BUILD_DIR)/$(BINARY_NAME)-windows-amd64.exe .

# Build linux/amd64 binary for frontend E2E tests
build-linux-amd64: submodules
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 GOEXPERIMENT=jsonv2 go build $(GO_BUILD_FLAGS) -o $(BUILD_DIR)/$(BINARY_NAME)-linux-amd64 .

# Build the macOS native privilege services app, login item, daemon, and brokerctl.
build-macos-privilege-services: $(PRIVILEGE_SERVICES_BUILD_STAMP)

$(PRIVILEGE_SERVICES_BUILD_STAMP): $(PRIVILEGE_SERVICES_FILES) scripts/build-macos-privilege-services.sh scripts/macos-privilege-services-common.sh
	./scripts/build-macos-privilege-services.sh
	@mkdir -p $(dir $@)
	@touch $@

# Run the macOS native privilege services test suite.
test-macos-privilege-services: $(PRIVILEGE_SERVICES_TEST_STAMP)

$(PRIVILEGE_SERVICES_TEST_STAMP): $(PRIVILEGE_SERVICES_FILES) scripts/test-macos-privilege-services.sh scripts/macos-privilege-services-common.sh
	./scripts/test-macos-privilege-services.sh
	@mkdir -p $(dir $@)
	@touch $@

# Package a development payload for local Apple Development-signed integration testing.
package-macos-privilege-services-dev:
	./scripts/package-macos-privilege-services-dev.sh

# Install the packaged development payload for local end-to-end testing.
install-macos-privilege-services-dev:
	./scripts/install-macos-privilege-services-dev.sh

# Remove the installed development payload from a local machine.
uninstall-macos-privilege-services-dev:
	./scripts/uninstall-macos-privilege-services-dev.sh

# Produce the signed macOS release installer when Developer ID material is available.
package-macos-privilege-services-release:
	./scripts/package-macos-privilege-services-release.sh

# Clean build artifacts
clean:
	rm -rf $(BUILD_DIR) .build/macos platform/macos/PrivilegeServices/ThandPrivilegeServices.xcodeproj

# Manually compress any binaries in $(BUILD_DIR) using UPX
compress:
	@if command -v upx >/dev/null 2>&1; then \
	  echo "Compressing all binaries in $(BUILD_DIR)/ with UPX..."; \
	  upx $(UPX_FLAGS) $(BUILD_DIR)/* || true; \
	else \
	  echo "'upx' not found. Install via 'brew install upx' (macOS) or your package manager."; \
	fi

# Install the binary to GOPATH/bin
install: submodules
	go install .

# Run the application
run: submodules
	go run .

# Run tests
test: submodules
	go test ./...
ifeq ($(HOST_OS),Darwin)
test: test-macos-privilege-services
endif

# Run functional tests
test-functional: submodules
	cd test && go test -v ./functional/...

# Run integration tests
test-integration: submodules
	cd test && go test -v ./integration/...

# Run UI E2E tests (requires linux/amd64 binary for Thand server container)
test-e2e: submodules build-linux-amd64
	cd test && go test -v -timeout 20m ./integration/frontend/...

# Generate FlatBuffers from JSON data
generate-data:
	@echo "Generating FlatBuffer schemas..."
	flatc --go -o internal/data/generated internal/data/schemas/aws.fbs
	flatc --go -o internal/data/generated internal/data/schemas/gcp.fbs
	flatc --go -o internal/data/generated internal/data/schemas/azure.fbs
	@echo "Generating FlatBuffer data files..."
	go run tools/generate-iam-dataset/main.go
	@echo "Data generation complete!"

# Generate Swagger documentation
swagger:
	@echo "Generating Swagger documentation..."
	@if command -v swag >/dev/null 2>&1; then \
		swag init -g internal/daemon/server.go --parseDependency --parseInternal; \
		echo "Swagger documentation generated successfully!"; \
		echo "View at: http://localhost:8080/swagger/index.html"; \
	else \
		echo "Error: 'swag' command not found."; \
		echo "Install with: go install github.com/swaggo/swag/cmd/swag@latest"; \
		exit 1; \
	fi

.PHONY: all build build-all build-linux-amd64 build-macos-privilege-services test-macos-privilege-services package-macos-privilege-services-dev install-macos-privilege-services-dev uninstall-macos-privilege-services-dev package-macos-privilege-services-release clean install run test test-functional test-integration test-e2e submodules update-submodules compress generate-data swagger
