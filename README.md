# Shadows Tunnel

**A Linux-first reverse TCP tunnel written in Go, using authenticated WebSocket-over-TLS (WSS) sessions and multiplexed TCP streams.**

- **Repository:** https://github.com/behnam86/shadows-tunnel
- **Current version:** `0.2.0-phase2`
- **Status:** Phase 2 MVP — early development

> **Important:** This is an early-stage project, not a mature VPN or a high-availability product. It has not received an independent security audit or production-scale load test. Review the code and test it in a non-production environment before relying on it for sensitive services.

## Contents

- [Overview](#overview)
- [Features](#features)
- [How it works](#how-it-works)
- [Requirements](#requirements)
- [Quick installation](#quick-installation)
- [Install manually](#install-manually)
- [Configuration reference](#configuration-reference)
- [CLI commands](#cli-commands)
- [Firewall and network setup](#firewall-and-network-setup)
- [Service management](#service-management)
- [Testing and verification](#testing-and-verification)
- [Security recommendations](#security-recommendations)
- [Troubleshooting](#troubleshooting)
- [Known limitations](#known-limitations)
- [Project layout](#project-layout)
- [Contributing](#contributing)
- [License](#license)

## Overview

Shadows Tunnel forwards TCP connections received by a **destination server** through authenticated WSS sessions to a **source agent**. The agent connects each logical stream to a TCP service reachable from the source host. Multiple logical TCP streams can share a WSS session.

A common two-host deployment is:

- **Destination / outside server:** owns the public endpoint and accepts incoming TCP connections.
- **Source / Iran-side server:** initiates outbound WSS connections to the destination and forwards traffic to a configured local target.

Use this software only on servers and networks you own or are explicitly authorized to administer.

Shadows Tunnel is a **TCP forwarder**, not a full-device VPN. It does not create a virtual network interface, assign IP addresses, route arbitrary IP packets, or provide Layer-3 networking.

## Features

Implemented in the Phase 2 code:

- Go implementation with a small dependency footprint.
- WebSocket over TLS (`wss://`) transport.
- TLS certificate validation on the agent.
- Shared bearer-token authentication during the WebSocket upgrade.
- Multiplexing of multiple logical TCP streams over a WSS session.
- Configurable agent session pool with minimum and maximum session counts.
- Bounded reconnect backoff.
- Strict JSON configuration parsing and role validation.
- CLI commands for server, agent, version, diagnostics, and TCP connection probes.
- `systemd` service units and an interactive installer for apt-based Linux systems.
- Unit/integration tests and GitHub Actions checks.

## How it works

```text
      Remote TCP client
             |
             | TCP to destination public listener :8443
             v
  +--------------------------------+
  | Destination server             |
  | Public TCP listener :8443      |
  | WSS/TLS endpoint :9443         |
  +---------------+----------------+
                  ||
                  || Authenticated WSS sessions
                  || Multiplexed TCP streams
                  ||
  +---------------+----------------+
  | Source agent                   |
  | Outbound WSS connections       |
  +---------------+----------------+
                  |
                  | TCP to configured target
                  v
        Local service :443
```

Example only: the destination listens on TCP `9443` for WSS and TCP `8443` for forwarded clients. The source agent connects to `wss://tunnel.example.com:9443/tunnel` and forwards each stream to `127.0.0.1:443`.

The WSS listener and public TCP listener are separate. The source agent initiates outbound WSS sessions, so it does not need an inbound tunnel-control listener. It must, however, be able to reach the configured target service.

## Requirements

The included installer is designed primarily for Ubuntu 22.04/24.04 and Debian 12+, and may work on other apt-based Linux distributions.

- Root access (`root` or `sudo`) for installation and service configuration.
- `apt-get`, `curl`, `git`, `openssl`, `tar`, and `gzip` (the installer installs required packages).
- Go 1.24 or newer for the installer's build process. The installer attempts to install Go if needed.
- CPU architecture `amd64` or `arm64` for automatic Go installation.
- A DNS name pointing to the destination server for the recommended Let's Encrypt setup, or existing TLS certificate and key PEM files.
- Network access from the source host to the destination WSS port.
- The configured target service must be reachable from the source agent.
- Firewall rules must be configured by the operator; the installer does not modify firewalls.

## Quick installation

> **Review the installer before running it as root.** The script installs packages, downloads/builds software, writes configuration, installs a service, and enables it. Do not execute a remote script you have not reviewed and do not run code you do not trust.

After this repository is published and accessible at the URL below, download and inspect the installer:

```bash
curl -fsSL https://raw.githubusercontent.com/behnam86/shadows-tunnel/main/install.sh -o install.sh
less install.sh
sudo bash install.sh
```

If you have reviewed and trust the exact version in the repository, the installer can also be run in one line:

```bash
curl -fsSL https://raw.githubusercontent.com/behnam86/shadows-tunnel/main/install.sh | sudo bash
```

The one-line command will not work until the repository and `install.sh` have actually been published at the expected GitHub URL.

### Recommended deployment order

Install the destination first, then the source agent.

### A. Destination server (outside/public VPS)

1. Run the installer on the public VPS.
2. Select role `1` — Destination server.
3. Set the WSS bind address. Default: `0.0.0.0:9443`.
4. Set the public TCP listener. Default: `0.0.0.0:8443`.
5. For automatic Let's Encrypt issuance, enter a DNS hostname that resolves to this server and an email address. HTTP-01 validation requires inbound TCP port `80` to be reachable during issuance. Enter a hostname only, not a URL.
6. If automatic issuance is skipped or fails, provide paths to an existing certificate/full-chain PEM file and TLS private-key PEM file.
7. Set the maximum public TCP connections and maximum streams per WSS session.
8. Save the generated token securely. It must be entered on the source agent.

The installer writes `/etc/shadows/server.json`, copies certificate/key files to `/etc/shadows/tls/`, installs the service unit, and enables `shadows.service`.

### B. Source agent (Iran-side VPS)

1. Run the same installer on the source VPS.
2. Select role `2` — Source agent.
3. Enter the destination WSS URL, for example `wss://tunnel.example.com:9443/tunnel`.
4. Enter the target TCP service address as seen from the source host, for example `127.0.0.1:443`.
5. Enter the exact 64-character hexadecimal token generated on the destination.
6. Choose the minimum and maximum WSS pool sessions (defaults: `2` and `4`).
7. Set the maximum multiplexed streams per session (default: `256`).

The installer writes `/etc/shadows/agent.json`, installs the agent service unit, and enables `shadows.service`.

### C. Verify both hosts

Run on both hosts:

```bash
sudo systemctl status shadows.service --no-pager -l
sudo journalctl -u shadows.service -n 100 --no-pager
```

Follow live logs:

```bash
sudo journalctl -u shadows.service -f
```

On the destination, a basic TCP probe can verify that the public listener accepts a connection:

```bash
/usr/local/bin/shadows tunnel test -address 127.0.0.1:8443
```

This probe only tests TCP connection establishment. It does **not** prove that the tunnel successfully reaches the target application. Perform an application-level end-to-end test as well.

## Install manually

Manual installation is intended for developers or operators who prefer to inspect each step. Install Git and a compatible Go toolchain first.

```bash
git clone https://github.com/behnam86/shadows-tunnel.git
cd shadows-tunnel

go test ./...
go test -race ./...
go vet ./...
go build -trimpath -ldflags='-s -w' -o ./shadows ./cmd/shadows

./shadows version
./shadows doctor
```

The manual build creates a local executable. It does not by itself create `/etc/shadows` configuration or install the `systemd` service. For a managed service installation, inspect and use `install.sh` on a supported host.

## Configuration reference

Example files in the repository:

- [`configs/server.example.json`](configs/server.example.json)
- [`configs/agent.example.json`](configs/agent.example.json)

Do not commit production configuration or real secrets to a public repository. Configuration uses JSON, and unsupported/unknown fields are rejected.

### Destination server configuration

Example:

```json
{
  "role": "server",
  "wss_listen": "0.0.0.0:9443",
  "listen": "0.0.0.0:8443",
  "tls_cert": "/etc/shadows/tls/fullchain.pem",
  "tls_key": "/etc/shadows/tls/privkey.pem",
  "token": "REPLACE_WITH_A_LONG_RANDOM_SECRET",
  "max_connections": 1000,
  "max_streams": 256
}
```

| Field | Description |
|---|---|
| `role` | Must be `server` on the destination host. |
| `wss_listen` | Address and port for the WSS/TLS endpoint, e.g. `0.0.0.0:9443`. |
| `listen` | Address and port for incoming public TCP clients, e.g. `0.0.0.0:8443`. It must differ from `wss_listen`. |
| `tls_cert` | Path to the TLS certificate/full-chain PEM file. |
| `tls_key` | Path to the TLS private-key PEM file. Keep the private key protected. |
| `token` | Shared secret used to authenticate the agent. Must match the agent's token. |
| `max_connections` | Upper bound for public TCP connections. Installer default: `1000`; explicit valid range: `1..100000`. |
| `max_streams` | Maximum logical streams per WSS session. Installer default: `256`; explicit valid range: `1..100000`. |

`0.0.0.0` binds all IPv4 interfaces. Use a narrower bind address if your topology allows it and you want to reduce exposure.

### Source agent configuration

Example:

```json
{
  "role": "agent",
  "server_url": "wss://tunnel.example.com:9443/tunnel",
  "target": "127.0.0.1:443",
  "token": "REPLACE_WITH_THE_SAME_LONG_RANDOM_SECRET",
  "ca_file": "",
  "min_pool": 2,
  "max_pool": 4,
  "max_streams": 256,
  "reconnect_min": "1s",
  "reconnect_max": "30s"
}
```

| Field | Description |
|---|---|
| `role` | Must be `agent` on the source host. |
| `server_url` | Destination WSS URL. Use `wss://` and the `/tunnel` path. |
| `target` | TCP service address that the agent should connect to for each stream, e.g. `127.0.0.1:443`. |
| `token` | Must be identical to the destination server's token. |
| `ca_file` | Optional PEM CA certificate file for a private CA. Empty means use system trust roots. TLS verification remains enabled. |
| `min_pool` | Number of WSS sessions/workers to start. Installer default: `2`; range `1..64`. |
| `max_pool` | Maximum WSS sessions/workers. Installer default: `4`; must be greater than or equal to `min_pool` and at most `64`. |
| `max_streams` | Maximum logical streams per WSS session. Installer default: `256`; explicit valid range: `1..100000`. |
| `reconnect_min` | Minimum reconnect backoff, expressed as a Go duration such as `1s`. |
| `reconnect_max` | Maximum reconnect backoff, e.g. `30s`; must not be less than `reconnect_min`. |

### Generate a token manually

The installer generates a random 32-byte token represented by 64 hexadecimal characters. If configuring manually, a token can be generated with:

```bash
openssl rand -hex 32
```

Use the same value on both hosts. Transfer it over a secure channel. Do not publish it in issues, screenshots, shell history, or committed configuration files. Rotate it if you suspect it has been exposed.

### Example traffic path

With the example values:

- Remote clients connect to the destination server's public TCP listener on port `8443`.
- The destination sends the connection through an authenticated WSS session.
- The source agent connects to `127.0.0.1:443` on the source host.
- Bytes are forwarded between the remote client and that target service.

This is raw TCP forwarding. The application protocol carried inside the TCP stream is determined by the target service and client.

## CLI commands

```text
shadows version
shadows doctor
shadows server -config /etc/shadows/server.json
shadows agent -config /etc/shadows/agent.json
shadows tunnel test -address host:port [-timeout 3s]
```

- `version`: prints the build version.
- `doctor`: runs basic local environment checks.
- `server`: starts the destination role using a server JSON configuration.
- `agent`: starts the source role using an agent JSON configuration.
- `tunnel test`: attempts a TCP connection to the specified address. It is not a complete tunnel or application health check.

For system installations, the binary is at `/usr/local/bin/shadows`.

## Firewall and network setup

The installer does **not** change host or cloud firewall rules. Configure both layers yourself.

For the example topology:

- Destination TCP `9443`: allow the source agent to reach the WSS endpoint. Restrict source addresses where practical.
- Destination TCP `8443`: allow only the remote clients intended to use the forwarded service.
- Destination TCP `80`: needed for HTTP-01 Let's Encrypt certificate issuance and as required by your renewal setup.
- Source host: allow outbound TCP access to destination port `9443`.
- Source host: ensure the configured target is reachable locally.

Do not expose the public listener to the entire internet unless that is intentional. A listener bound to `0.0.0.0` is reachable through every interface permitted by firewall and routing rules.

## Service management

The installer installs a systemd service named `shadows.service` on each host.

```bash
# Check status
sudo systemctl status shadows.service --no-pager -l

# View recent logs
sudo journalctl -u shadows.service -n 100 --no-pager

# Follow live logs
sudo journalctl -u shadows.service -f

# Restart after a configuration change
sudo systemctl restart shadows.service

# Stop or start
sudo systemctl stop shadows.service
sudo systemctl start shadows.service

# Enable at boot
sudo systemctl enable shadows.service
```

### Important paths

| Item | Path |
|---|---|
| Installed binary | `/usr/local/bin/shadows` |
| Destination configuration | `/etc/shadows/server.json` |
| Source agent configuration | `/etc/shadows/agent.json` |
| Copied TLS certificate | `/etc/shadows/tls/fullchain.pem` |
| Copied TLS private key | `/etc/shadows/tls/privkey.pem` |
| Installer source checkout | `/opt/shadows-tunnel-src` |
| systemd unit | `/etc/systemd/system/shadows.service` |

Back up `/etc/shadows/` securely before upgrades or configuration changes. It contains secrets and TLS material.

### TLS certificate renewal

If the installer successfully obtains a Let's Encrypt certificate, it creates a deploy hook that copies renewed certificate/key files to `/etc/shadows/tls/` and restarts the service. Verify that renewal and the deploy hook work in your environment. If you provide your own certificate, renewal and deployment are your responsibility.

## Testing and verification

Run the following checks from the source checkout:

```bash
gofmt -w .
go test ./...
go test -race ./...
go vet ./...
go build -trimpath -ldflags='-s -w' -o ./shadows ./cmd/shadows
./shadows version
./shadows doctor
```

The repository includes an integration test that starts a local test setup and checks TCP data forwarding through WSS and multiplexing. Automated tests cover specific scenarios; they are not a security audit or proof of reliability under every network condition.

For end-to-end verification, connect an application-aware client to the destination public listener and verify the expected response from the source-side target.

## Security recommendations

1. Use a valid TLS certificate whose hostname matches the hostname in `server_url`.
2. Keep TLS certificate verification enabled. Do not weaken verification to work around certificate problems.
3. Use a high-entropy token and keep it private.
4. Restrict permissions on `/etc/shadows/` and TLS private-key files.
5. Never commit real tokens, private keys, or production configuration to a public repository.
6. Restrict the WSS port to known source hosts where feasible.
7. Restrict the public TCP listener to intended clients or networks where possible.
8. Keep the operating system and toolchain updated.
9. Test updates in a staging environment and back up configuration before changing production systems.
10. Perform an independent security review before exposing sensitive workloads.

The shared bearer token authenticates the agent to the destination; it is not a complete identity-management or authorization system. Anyone who can reach the public TCP listener may be able to use the forwarded service, depending on the target protocol and network controls.

## Troubleshooting

### The service exits immediately

Check status and logs:

```bash
sudo systemctl status shadows.service --no-pager -l
sudo journalctl -u shadows.service -n 150 --no-pager
```

Verify that the correct role-specific configuration exists, JSON syntax is valid, required fields are present, and the service can read the config and TLS files.

### Address already in use

Check listening sockets:

```bash
sudo ss -lntp
```

Another process may be using the WSS or public TCP port. Select unused ports or reconfigure the conflicting service.

### The agent cannot connect to WSS

Check DNS, firewall rules, service status, the WSS port, URL path (`/tunnel`), system clock, and certificate validity/hostname. From the source host, this command tests only TCP reachability:

```bash
nc -vz tunnel.example.com 9443
```

A successful TCP probe does not prove TLS or token authentication works.

### Authentication fails

Confirm that the token on both hosts is identical and contains no leading/trailing whitespace or newline. If the token may have been exposed, replace it on both sides and restart the services.

### The public port accepts TCP but the application does not work

Confirm that the source-side target service is running and reachable from the source agent, and that the client is speaking the protocol expected by that service. Review logs on both hosts and perform an application-level test.

### Disconnects interrupt active connections

In this version, streams carried by a failed WSS session are interrupted. The agent reconnect loop can establish a new session, but active TCP streams are not migrated transparently. The session pool does not guarantee seamless failover.

### Let's Encrypt certificate issuance fails

Verify that the hostname resolves to the destination server and that TCP port `80` is reachable for HTTP-01 validation. If issuance fails, provide readable existing PEM certificate and key files when prompted.

### The installer cannot download Go

The installer needs Go 1.24 or newer for its build flow and attempts to download Go from `go.dev` on `amd64` or `arm64`. If the server cannot access `go.dev`, install a compatible Go toolchain manually and rerun the installer.

When reporting a bug, include the project version, OS version, sanitized configuration, relevant logs, and steps to reproduce. **Remove tokens, private keys, and sensitive server details first.**

## Known limitations

This is an early Phase 2 MVP. Current limitations include:

- No independent security audit, penetration test, or production load benchmark.
- The WebSocket implementation supports the subset required by the project; fragmented WebSocket messages and text messages are not supported.
- The session pool can scale up within configured bounds but does not scale down, retire idle sessions, or use reconnect jitter.
- A failed WSS session interrupts streams carried by that session; active streams are not migrated to another session.
- No health scoring or intelligent failover.
- No web dashboard, management API, database, or multi-tenant control plane.
- No UDP forwarding, QUIC/KCP transport, virtual network interface, or Layer-3 VPN functionality.
- The installer does not configure firewalls.
- Certificate renewal deployment is automated only when the installer successfully obtains a Let's Encrypt certificate; manually supplied certificates must be managed by the operator.
- No license has been selected yet. Public visibility alone does not grant permission to reuse, modify, redistribute, or commercially use the code.

## Project layout

```text
shadows-tunnel/
├── cmd/shadows/             # CLI entry point
├── configs/                 # Example server and agent JSON
├── deployments/systemd/     # systemd unit templates
├── docs/                    # Phase implementation notes
├── internal/
│   ├── config/              # JSON loading and validation
│   ├── doctor/              # Local diagnostic checks
│   ├── logging/             # Logging setup
│   ├── transport/
│   │   ├── wss/             # WebSocket-over-TLS transport
│   │   └── tcpmux/          # TCP stream multiplexing
│   └── tunnel/reverse/      # Destination and source roles
├── tests/                   # Tests
├── .github/workflows/       # CI workflow
├── install.sh               # Interactive Linux installer
├── go.mod
├── Makefile
└── README.md
```

## Contributing

Bug reports, reproducible test cases, documentation improvements, and focused pull requests are welcome.

- Keep changes focused and explain the motivation.
- Add or update tests for behavior changes.
- Update the README and example configuration when options change.
- Do not include secrets or production configuration in commits.
- Run formatting, tests, race tests, and `go vet` before opening a pull request.
- Protocol and authentication changes require particular care because they can affect compatibility and security.

## License

**No license has been selected for this repository yet.** Until the repository owner adds an explicit `LICENSE` file, do not assume the code is licensed for reuse, modification, redistribution, or commercial use.
