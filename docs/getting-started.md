---
layout: default
title: Getting Started
nav_order: 2
description: "Get started with Thand Agent - installation, configuration, and first steps"
---

# Getting Started
{: .no_toc }

Get up and running with Thand Agent quickly and easily.
{: .fs-6 .fw-300 }

## Table of contents
{: .no_toc .text-delta }

1. TOC
{:toc}

---

{: .note }
By default thand will authenticate to **auth.thand.io** but you can specify your own server.
Your administrator will provide the server URL. If you are setting this up in your own environment you will need to first deploy the Thand server in your environment.
Please see the [Environment Setup](../environments/) for more information.

---

## Installation

### Install via script

```bash
# Install agent (https://github.com/thand-io/agent/blob/main/scripts/install.sh). Trust but verify!
curl -sSL https://get.thand.io | sh
```

### Install via Homebrew (macOS/Linux)

```bash
brew tap thand-io/tap
brew install thand
```

### Install via Binary Release

Download the latest binary for your platform from the [GitHub Releases](https://github.com/thand-io/agent/releases) page:

```bash
# Linux/macOS
curl -L -o thand https://github.com/thand-io/agent/releases/latest/download/agent-$(uname -s)-$(uname -m)
chmod +x thand
sudo mv thand /usr/local/bin/
```

```bash
# Windows (PowerShell)
Invoke-WebRequest -Uri "https://github.com/thand-io/agent/releases/latest/download/agent-windows-amd64.exe" -OutFile "thand.exe"
```

### Build from Source

Before installing Thand Agent, ensure you have:

- Go 1.21 or later (if building from source)
- Access to your target infrastructure (GCP, AWS, etc.)

Clone the repository and build the agent:

```bash
git clone https://github.com/thand-io/agent.git
cd agent
make submodules
make build
```

The binary will be available at `bin/agent`.

---

## Configuration

### Basic Configuration

Create a configuration file at `~/.thand/config.yaml`:

```yaml
server:
  url: "https://your-thand-server.com"
  
agent:
  listen_port: 8080
  session_timeout: "1h"
  
logging:
  level: "info"
  format: "json"
```

### Environment Variables

You can also configure the agent using environment variables:

```bash
export THAND_SERVER_URL="https://your-thand-server.com"
export THAND_AGENT_PORT="8080"
export THAND_LOGGING_LEVEL="info"
```

---

## First Steps

### 1. Authenticate

Initialize your authentication with the Thand server:

```bash
thand login
```

Or via a thand login server.

```bash
thand login --server http://localhost:9090
```

This will open your browser for authentication or provide a device code flow. Once authenticated, your session will be cached locally. You can view your current session with:

```bash
thand sessions
```

### 3. Request Access

Request access to a resource:

```bash
# Request AWS access for 1 hour
thand request access --provider aws-prod --role Admin --duration 1h --reason "Deploying new version"

# Request GCP access
thand request access --provider gcp --role viewer --duration 2h --reason "Data analysis"

# Request via natural language
thand "Get me admin access to AWS production for 2 hours to perform maintenance"
```

### 4. Use Your Access

Once approved, the agent will provide temporary credentials:

```bash
# AWS credentials are automatically configured
aws s3 ls

# GCP credentials are set via environment
gcloud projects list
```

---

## Common Use Cases

### Local Sudo Access

Request temporary sudo access on your local machine:

```bash
thand request sudo --duration 30m --reason "System maintenance"

# Or broker a single privileged command
thand request sudo --reason "Inspect listening ports" -- netstat -ab
```

Local sudo is handled by the `local-elevation` provider on the target agent.
For Unix targets, the agent chooses the local account in this order:

1. `providers.local-elevation.config.username`
2. `req.Identity.User.Username`, but only when the identity carries at least one
   trusted provider ID in `Identity.Providers`

Identity-username fallback is disabled by default. To enable it for a specific
provider instance, trust that provider ID explicitly:

```yaml
providers:
  local-elevation:
    provider: local
    enabled: true
    config:
      trusted_username_sources:
        - oauth2-jumpcloud
```

If you want to pin the local account regardless of the authenticated identity,
set it directly:

```yaml
providers:
  local-elevation:
    provider: local
    enabled: true
    config:
      username: localadmin
```

You can also add explicit safety policy:

```yaml
providers:
  local-elevation:
    provider: local
    enabled: true
    config:
      trusted_username_sources:
        - oauth2-jumpcloud
      denied_usernames:
        - root
        - daemon
        - nobody
      allowed_uid_ranges:
        - "1000-60000"
```

Notes:
- `trusted_username_sources` matches provider IDs, not provider types. For an identity
  like `{"oauth2-jumpcloud":"oauth2"}`, the trusted value is `oauth2-jumpcloud`.
- If `trusted_username_sources` is unset or empty, identity-username fallback is
  disabled and the agent will only use `config.username`.
- Browser/server local sudo requests require an explicit target agent string.
  CLI `thand request sudo` still defaults to the local environment target when
  `--target` is omitted.
- If `allowed_uid_ranges` is omitted, Unix agents try `/etc/login.defs` first
  using `UID_MIN` and `UID_MAX`, then fall back to conservative defaults.
- If trust or account validation fails, the sudo request fails closed and no
  sudoers fragment is written.

## Next Steps

- **[Environment Setup](../environments/)** - Configure Thand for specific environments
- **[Configuration](../configuration/)** - Detailed configuration options
- **[API Reference](../api/)** - REST API documentation
- **[Workflows](../configuration/workflows/)** - Custom workflow configuration
