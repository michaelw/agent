#!/bin/sh
set -eu

. "$(dirname "$0")/macos-privilege-services-common.sh"

verify_localbroker_grpc_sources
