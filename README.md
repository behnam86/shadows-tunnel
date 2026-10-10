# Shadows Tunnel

**A Linux-first reverse TCP tunnel written in Go, using authenticated WebSocket-over-TLS (WSS) sessions and multiplexed TCP streams.**

[English](#english) · [فارسی](#فارسی)

<a id="english"></a>

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
- [راهنمای فارسی](#فارسی)

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
- The project is intended to be licensed under GNU GPL v3.0. See the [License](#license) section and ensure the repository contains the full `LICENSE` file.

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

Shadows Tunnel is intended to be distributed under the **GNU General Public License v3.0 (GPL-3.0)**. See the [`LICENSE`](LICENSE) file for the complete terms. Before publishing, make sure this file contains the full, unmodified GPL v3.0 license text and that you have the rights to license all included code.

GPL v3.0 permits use, modification, and redistribution subject to its terms. If you distribute a modified version, the applicable source-code and license-notice requirements must be followed. Merely running the software as a network service does not, by itself, trigger the same network-source disclosure requirement associated with the GNU AGPL.


---

<a id="فارسی"></a>

# راهنمای فارسی Shadows Tunnel

**Shadows Tunnel یک تونل معکوس TCP مبتنی بر Go برای لینوکس است که از ارتباط‌های WebSocket امن روی TLS (یعنی WSS)، احراز هویت با توکن و چندگانه‌سازی جریان‌های TCP استفاده می‌کند.**

- **مخزن پروژه:** https://github.com/behnam86/shadows-tunnel
- **نسخه فعلی:** `0.2.0-phase2`
- **وضعیت:** MVP فاز دوم؛ پروژه هنوز در مراحل اولیه توسعه است.
- **زبان اصلی مستندات:** انگلیسی و فارسی

> **هشدار مهم:** این پروژه در مراحل اولیه قرار دارد و محصول VPN کامل یا سامانه‌ای با دسترس‌پذیری بالا محسوب نمی‌شود. هنوز ممیزی امنیتی مستقل یا آزمون بار در مقیاس تولید روی آن انجام نشده است. پیش از استفاده برای سرویس‌های حساس، کد را بررسی کنید و ابتدا در محیط آزمایشی آن را بسنجید.

## فهرست مطالب فارسی

- [معرفی](#معرفی)
- [قابلیت‌ها](#قابلیتهای-پیادهسازی‌شده)
- [نحوه عملکرد](#نحوه-عملکرد)
- [پیش‌نیازها](#پیشنیازها)
- [نصب سریع](#نصب-سریع)
- [نصب دستی و آزمون](#نصب-دستی-و-آزمون)
- [پیکربندی سرور مقصد](#پیکربندی-سرور-مقصد)
- [پیکربندی Agent مبدأ](#پیکربندی-agent-مبدأ)
- [دستورهای CLI](#دستورهای-cli)
- [شبکه و فایروال](#شبکه-و-فایروال)
- [مدیریت سرویس](#مدیریت-سرویس)
- [توصیه‌های امنیتی](#توصیههای-امنیتی)
- [عیب‌یابی](#عیبیابی)
- [محدودیت‌های فعلی](#محدودیتهای-فعلی)
- [ساختار پروژه](#ساختار-پروژه)
- [مشارکت در توسعه](#مشارکت-در-توسعه)
- [مجوز نرم‌افزار](#مجوز-نرم‌افزار)

## معرفی

Shadows Tunnel اتصال‌های TCP ورودی به **سرور مقصد** را از طریق نشست‌های WSS احراز هویت‌شده به **Agent مبدأ** منتقل می‌کند. Agent هر جریان منطقی را به یک سرویس TCP قابل‌دسترسی از میزبان مبدأ وصل می‌کند. چند جریان TCP می‌توانند از یک نشست WSS مشترک عبور کنند.

در یک استقرار معمول با دو سرور:

- **سرور مقصد / سرور خارج:** نقطه اتصال عمومی را در اختیار دارد و اتصال‌های TCP ورودی را می‌پذیرد.
- **سرور مبدأ / سرور ایران:** اتصال WSS خروجی را به سرور مقصد آغاز می‌کند و ترافیک را به سرویس هدف تنظیم‌شده منتقل می‌کند.

از این نرم‌افزار فقط روی سرورها و شبکه‌هایی استفاده کنید که مالک آن هستید یا مجوز صریح مدیریت آن‌ها را دارید.

Shadows Tunnel یک **انتقال‌دهنده TCP** است، نه VPN کامل دستگاه. این نرم‌افزار رابط شبکه مجازی ایجاد نمی‌کند، IP اختصاص نمی‌دهد، بسته‌های IP دلخواه را مسیریابی نمی‌کند و شبکه‌سازی لایه ۳ ارائه نمی‌دهد.

## قابلیت‌های پیاده‌سازی‌شده

- پیاده‌سازی با Go و وابستگی‌های محدود.
- انتقال از طریق WebSocket روی TLS با نشانی `wss://`.
- اعتبارسنجی گواهی TLS در Agent.
- احراز هویت هنگام ارتقای اتصال WebSocket با توکن Bearer مشترک.
- چندگانه‌سازی جریان‌های منطقی TCP روی یک نشست WSS.
- تنظیم حداقل و حداکثر تعداد نشست‌های Agent.
- تلاش مجدد اتصال با تأخیر افزایشی و سقف مشخص.
- اعتبارسنجی سخت‌گیرانه پیکربندی JSON و نقش هر میزبان.
- فرمان‌های CLI برای سرور، Agent، نسخه، بررسی‌های تشخیصی و آزمون اتصال TCP.
- فایل‌های سرویس `systemd` و نصب‌کننده تعاملی برای توزیع‌های لینوکسی مبتنی بر apt.
- آزمون‌های واحد/یکپارچه‌سازی و بررسی‌های خودکار GitHub Actions.

## نحوه عملکرد

در پیکربندی نمونه، کلاینت‌های راه دور به پورت عمومی TCP شماره `8443` روی سرور مقصد وصل می‌شوند. سرور مقصد اتصال را از طریق نشست WSS احراز هویت‌شده عبور می‌دهد و Agent مبدأ به سرویس هدف، مثلاً `127.0.0.1:443`، متصل می‌شود.

پورت WSS و پورت عمومی TCP دو نقطه شنود جداگانه‌اند. Agent اتصال WSS خروجی را آغاز می‌کند و بنابراین برای کنترل تونل به پورت ورودی جداگانه‌ای روی سرور مبدأ نیاز ندارد؛ بااین‌حال، باید بتواند به سرویس هدف تنظیم‌شده دسترسی پیدا کند.

```text
کلاینت TCP راه دور
       | TCP به پورت عمومی مقصد :8443
       v
سرور مقصد: شنونده TCP :8443 و نقطه WSS/TLS :9443
       || نشست‌های WSS احراز هویت‌شده و جریان‌های چندگانه‌شده
       v
Agent مبدأ: اتصال WSS خروجی
       | TCP به سرویس هدف تنظیم‌شده
       v
سرویس محلی :443
```

## پیش‌نیازها

- نصب‌کننده عمدتاً برای Ubuntu 22.04/24.04 و Debian 12 به بعد طراحی شده و ممکن است روی سایر توزیع‌های مبتنی بر apt نیز کار کند.
- دسترسی `root` یا `sudo` برای نصب و پیکربندی سرویس.
- ابزارهای `apt-get`، `curl`، `git`، `openssl`، `tar` و `gzip`؛ نصب‌کننده بسته‌های لازم را نصب می‌کند.
- Go نسخه 1.24 یا جدیدتر برای فرایند ساخت؛ نصب‌کننده تلاش می‌کند در صورت نیاز Go را نصب کند.
- برای نصب خودکار Go، معماری `amd64` یا `arm64`.
- برای روش پیشنهادی Let's Encrypt، نام دامنه‌ای که به سرور مقصد اشاره کند؛ یا مسیر گواهی TLS و کلید خصوصی موجود با قالب PEM.
- امکان اتصال شبکه از سرور مبدأ به پورت WSS مقصد.
- سرویس هدف باید از میزبان Agent قابل دسترسی باشد.
- تنظیم فایروال بر عهده مدیر سرور است و نصب‌کننده قوانین فایروال را تغییر نمی‌دهد.

## نصب سریع

> **پیش از اجرا، نصب‌کننده را بررسی کنید.** این اسکریپت ممکن است بسته نصب کند، نرم‌افزار را دریافت یا بسازد، فایل پیکربندی بنویسد و سرویس را فعال کند. اسکریپت بررسی‌نشده را با دسترسی root اجرا نکنید.

پس از آنکه مخزن و فایل `install.sh` واقعاً در نشانی GitHub زیر منتشر شدند، اسکریپت را دریافت و بررسی کنید:

```bash
curl -fsSL https://raw.githubusercontent.com/behnam86/shadows-tunnel/main/install.sh -o install.sh
less install.sh
sudo bash install.sh
```

فرمان بالا تا زمانی که مخزن و فایل نصب‌کننده در نشانی مورد انتظار منتشر نشده باشند کار نخواهد کرد.

### ترتیب پیشنهادی نصب

ابتدا سرور مقصد و سپس Agent مبدأ را نصب کنید.

### الف) سرور مقصد (سرور عمومی/خارج)

1. نصب‌کننده را روی سرور عمومی اجرا کنید.
2. نقش `1` یعنی Destination server را انتخاب کنید.
3. نشانی شنود WSS را تنظیم کنید؛ مقدار پیش‌فرض `0.0.0.0:9443` است.
4. شنونده عمومی TCP را تنظیم کنید؛ مقدار پیش‌فرض `0.0.0.0:8443` است.
5. برای دریافت خودکار گواهی Let's Encrypt، نام دامنه‌ای را وارد کنید که به این سرور اشاره می‌کند و یک ایمیل بدهید. اعتبارسنجی HTTP-01 هنگام صدور گواهی به دسترسی ورودی پورت TCP شماره `80` نیاز دارد. فقط نام میزبان را وارد کنید، نه URL.
6. اگر صدور خودکار انجام نشد یا از آن صرف‌نظر کردید، مسیر فایل گواهی/زنجیره کامل PEM و کلید خصوصی PEM را بدهید.
7. حداکثر تعداد اتصال‌های عمومی و جریان‌ها در هر نشست WSS را تنظیم کنید.
8. توکن تولیدشده را محرمانه نگه دارید؛ همان توکن باید در Agent وارد شود.

نصب‌کننده پیکربندی را در `/etc/shadows/server.json` می‌نویسد، فایل‌های گواهی/کلید را به `/etc/shadows/tls/` کپی می‌کند و سرویس `shadows.service` را نصب و فعال می‌کند.

### ب) Agent مبدأ (سرور ایران)

1. همان نصب‌کننده را روی سرور مبدأ اجرا کنید.
2. نقش `2` یعنی Source agent را انتخاب کنید.
3. نشانی WSS مقصد را وارد کنید؛ برای نمونه `wss://tunnel.example.com:9443/tunnel`.
4. نشانی سرویس TCP هدف را از دید سرور مبدأ وارد کنید؛ برای نمونه `127.0.0.1:443`.
5. توکن هگزادسیمال ۶۴ کاراکتری را که روی مقصد تولید شده، عیناً وارد کنید.
6. حداقل و حداکثر نشست‌های WSS را انتخاب کنید؛ مقادیر پیش‌فرض `2` و `4` هستند.
7. حداکثر جریان‌های چندگانه‌شده در هر نشست را تنظیم کنید؛ مقدار پیش‌فرض `256` است.

نصب‌کننده فایل `/etc/shadows/agent.json` را می‌نویسد و سرویس را نصب و فعال می‌کند.

### ج) بررسی هر دو میزبان

روی هر دو سرور اجرا کنید:

```bash
sudo systemctl status shadows.service --no-pager -l
sudo journalctl -u shadows.service -n 100 --no-pager
```

برای مشاهده زنده گزارش‌ها:

```bash
sudo journalctl -u shadows.service -f
```

روی سرور مقصد، برای بررسی اولیه شنونده TCP می‌توانید اجرا کنید:

```bash
/usr/local/bin/shadows tunnel test -address 127.0.0.1:8443
```

این آزمون فقط برقراری اتصال TCP را بررسی می‌کند و ثابت نمی‌کند که تونل تا برنامه مقصد کار می‌کند. آزمون سرتاسری در سطح خود برنامه نیز انجام دهید.

## نصب دستی و آزمون

این روش برای توسعه‌دهندگان یا مدیرانی است که می‌خواهند مراحل نصب را جداگانه بررسی کنند. ابتدا Git و نسخه سازگار Go را نصب کنید.

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

ساخت دستی فقط فایل اجرایی محلی تولید می‌کند؛ به‌تنهایی پیکربندی `/etc/shadows` یا سرویس `systemd` را نصب نمی‌کند. برای نصب سرویس، فایل `install.sh` را بررسی و روی میزبان پشتیبانی‌شده اجرا کنید.

## پیکربندی سرور مقصد

فایل نمونه: [`configs/server.example.json`](configs/server.example.json)

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

| فیلد | توضیح |
|---|---|
| `role` | در میزبان مقصد باید `server` باشد. |
| `wss_listen` | نشانی و پورت نقطه WSS/TLS، برای نمونه `0.0.0.0:9443`. |
| `listen` | نشانی و پورت کلاینت‌های TCP ورودی، برای نمونه `0.0.0.0:8443`؛ باید با `wss_listen` متفاوت باشد. |
| `tls_cert` | مسیر فایل گواهی TLS یا زنجیره کامل با قالب PEM. |
| `tls_key` | مسیر کلید خصوصی TLS؛ آن را محافظت کنید. |
| `token` | راز مشترک احراز هویت Agent؛ باید با مقدار Agent یکسان باشد. |
| `max_connections` | سقف اتصال‌های عمومی؛ مقدار پیش‌فرض نصب‌کننده `1000` و بازه معتبر `1..100000` است. |
| `max_streams` | سقف جریان‌های منطقی در هر نشست WSS؛ مقدار پیش‌فرض `256` و بازه معتبر `1..100000` است. |

نشانی `0.0.0.0` روی تمام رابط‌های IPv4 شنود می‌کند. اگر معماری شبکه اجازه می‌دهد، برای کاهش سطح دسترسی از نشانی محدودتری استفاده کنید.

## پیکربندی Agent مبدأ

فایل نمونه: [`configs/agent.example.json`](configs/agent.example.json)

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

| فیلد | توضیح |
|---|---|
| `role` | در میزبان مبدأ باید `agent` باشد. |
| `server_url` | نشانی WSS سرور مقصد؛ از `wss://` و مسیر `/tunnel` استفاده کنید. |
| `target` | سرویس TCP که Agent برای هر جریان به آن وصل می‌شود؛ برای نمونه `127.0.0.1:443`. |
| `token` | باید دقیقاً با توکن سرور مقصد یکسان باشد. |
| `ca_file` | مسیر اختیاری گواهی CA با قالب PEM برای CA خصوصی؛ مقدار خالی از ریشه‌های اعتماد سیستم استفاده می‌کند و اعتبارسنجی TLS همچنان فعال است. |
| `min_pool` | تعداد حداقل نشست‌ها؛ پیش‌فرض `2` و بازه `1..64`. |
| `max_pool` | تعداد حداکثر نشست‌ها؛ پیش‌فرض `4` و باید بزرگ‌تر یا مساوی `min_pool` و حداکثر `64` باشد. |
| `max_streams` | سقف جریان منطقی در هر نشست؛ پیش‌فرض `256` و بازه معتبر `1..100000`. |
| `reconnect_min` | حداقل تأخیر تلاش مجدد، با قالب مدت‌زمان Go مانند `1s`. |
| `reconnect_max` | حداکثر تأخیر تلاش مجدد، مانند `30s`؛ نباید از `reconnect_min` کمتر باشد. |

### تولید توکن به‌صورت دستی

نصب‌کننده یک توکن تصادفی ۳۲ بایتی می‌سازد که به شکل ۶۴ نویسه هگزادسیمال نمایش داده می‌شود. برای تولید دستی:

```bash
openssl rand -hex 32
```

همان مقدار را در هر دو میزبان قرار دهید و از کانال امن منتقل کنید. توکن را در مخزن عمومی، گزارش خطا، تصویر صفحه، تاریخچه قابل‌دسترسی shell یا فایل پیکربندی commit نکنید. اگر احتمال افشا وجود دارد، آن را تعویض کنید.

## دستورهای CLI

```text
shadows version
shadows doctor
shadows server -config /etc/shadows/server.json
shadows agent -config /etc/shadows/agent.json
shadows tunnel test -address host:port [-timeout 3s]
```

- `version`: نسخه ساخت را نمایش می‌دهد.
- `doctor`: بررسی‌های پایه محیط محلی را اجرا می‌کند.
- `server`: نقش مقصد را با پیکربندی JSON سرور اجرا می‌کند.
- `agent`: نقش مبدأ را با پیکربندی JSON Agent اجرا می‌کند.
- `tunnel test`: تلاش می‌کند به نشانی TCP مشخص‌شده وصل شود؛ این دستور آزمون کامل تونل یا سلامت برنامه نیست.

در نصب سیستمی، فایل اجرایی در `/usr/local/bin/shadows` قرار دارد.

## شبکه و فایروال

نصب‌کننده قوانین فایروال سیستم‌عامل یا پنل ابری را تغییر نمی‌دهد؛ هر دو لایه را خودتان تنظیم کنید. در معماری نمونه:

- پورت TCP `9443` مقصد: اجازه دسترسی Agent مبدأ به نقطه WSS را بدهید و در صورت امکان IP مبدأ را محدود کنید.
- پورت TCP `8443` مقصد: فقط برای کلاینت‌هایی باز باشد که باید از سرویس فورواردشده استفاده کنند.
- پورت TCP `80` مقصد: برای اعتبارسنجی HTTP-01 گواهی Let's Encrypt و مطابق نیاز تمدید آن لازم است.
- سرور مبدأ: اتصال خروجی TCP به پورت `9443` مقصد مجاز باشد.
- سرور مبدأ: سرویس هدف تنظیم‌شده از Agent قابل دسترسی باشد.

شنونده عمومی را جز در صورت نیاز واقعی در معرض کل اینترنت قرار ندهید. شنود روی `0.0.0.0` از همه رابط‌هایی قابل دسترسی است که فایروال و مسیریابی اجازه دهند.

## مدیریت سرویس

نصب‌کننده روی هر میزبان سرویس `shadows.service` را در systemd نصب می‌کند.

```bash
# مشاهده وضعیت
sudo systemctl status shadows.service --no-pager -l

# مشاهده گزارش‌های اخیر
sudo journalctl -u shadows.service -n 100 --no-pager

# دنبال‌کردن زنده گزارش‌ها
sudo journalctl -u shadows.service -f

# راه‌اندازی مجدد پس از تغییر پیکربندی
sudo systemctl restart shadows.service

# توقف یا شروع
sudo systemctl stop shadows.service
sudo systemctl start shadows.service

# فعال‌سازی هنگام بوت
sudo systemctl enable shadows.service
```

### مسیرهای مهم

| مورد | مسیر |
|---|---|
| فایل اجرایی نصب‌شده | `/usr/local/bin/shadows` |
| پیکربندی سرور مقصد | `/etc/shadows/server.json` |
| پیکربندی Agent مبدأ | `/etc/shadows/agent.json` |
| گواهی TLS کپی‌شده | `/etc/shadows/tls/fullchain.pem` |
| کلید خصوصی TLS کپی‌شده | `/etc/shadows/tls/privkey.pem` |
| نسخه منبع دریافت‌شده توسط نصب‌کننده | `/opt/shadows-tunnel-src` |
| فایل واحد systemd | `/etc/systemd/system/shadows.service` |

پیش از ارتقا یا تغییر پیکربندی، از `/etc/shadows/` نسخه پشتیبان امن تهیه کنید؛ این پوشه شامل اسرار و اطلاعات TLS است.

### تمدید گواهی TLS

اگر نصب‌کننده با موفقیت گواهی Let's Encrypt دریافت کند، یک deploy hook می‌سازد تا گواهی/کلید تمدیدشده را به `/etc/shadows/tls/` کپی و سرویس را راه‌اندازی مجدد کند. کارکرد تمدید و hook را در محیط خود بررسی کنید. اگر گواهی را دستی وارد کرده‌اید، تمدید و استقرار آن بر عهده شماست.

## نصب دستی و آزمون

از پوشه مخزن اجرا کنید:

```bash
gofmt -w .
go test ./...
go test -race ./...
go vet ./...
go build -trimpath -ldflags='-s -w' -o ./shadows ./cmd/shadows
./shadows version
./shadows doctor
```

مخزن یک آزمون یکپارچه‌سازی دارد که محیط آزمایشی محلی ایجاد می‌کند و انتقال داده TCP از طریق WSS و چندگانه‌سازی را بررسی می‌کند. آزمون‌های خودکار فقط سناریوهای مشخص را پوشش می‌دهند و جای ممیزی امنیتی یا تضمین پایداری در همه شرایط شبکه را نمی‌گیرند. برای بررسی سرتاسری، با یک کلاینت آگاه از پروتکل به شنونده عمومی مقصد وصل شوید و پاسخ سرویس هدف سمت مبدأ را بررسی کنید.

## توصیه‌های امنیتی

1. از گواهی TLS معتبر استفاده کنید که نام میزبان آن با نام موجود در `server_url` مطابقت داشته باشد.
2. اعتبارسنجی گواهی TLS را فعال نگه دارید؛ برای رفع خطای گواهی آن را تضعیف نکنید.
3. از توکن با آنتروپی بالا استفاده کنید و آن را محرمانه نگه دارید.
4. دسترسی فایل‌های `/etc/shadows/` و کلید خصوصی TLS را محدود کنید.
5. توکن واقعی، کلید خصوصی یا پیکربندی تولید را در مخزن عمومی commit نکنید.
6. در صورت امکان دسترسی به پورت WSS را به میزبان‌های مبدأ شناخته‌شده محدود کنید.
7. شنونده عمومی TCP را تا حد امکان به کلاینت‌ها یا شبکه‌های موردنظر محدود کنید.
8. سیستم‌عامل و ابزارهای ساخت را به‌روز نگه دارید.
9. به‌روزرسانی‌ها را در محیط آزمایشی بررسی کنید و پیش از تغییر سامانه عملیاتی از پیکربندی نسخه پشتیبان بگیرید.
10. پیش از انتقال بارهای کاری حساس، بازبینی امنیتی مستقل انجام دهید.

توکن مشترک، Agent را نزد سرور مقصد احراز هویت می‌کند؛ این توکن جایگزین سامانه کامل مدیریت هویت و مجوزدهی نیست. بسته به پروتکل سرویس هدف و کنترل‌های شبکه، هر کسی که به شنونده عمومی TCP دسترسی دارد ممکن است بتواند از سرویس فورواردشده استفاده کند.

## عیب‌یابی

### سرویس بلافاصله خارج می‌شود

```bash
sudo systemctl status shadows.service --no-pager -l
sudo journalctl -u shadows.service -n 150 --no-pager
```

بررسی کنید پیکربندی متناسب با نقش میزبان وجود داشته باشد، نحو JSON معتبر باشد، فیلدهای لازم تنظیم شده باشند و سرویس امکان خواندن پیکربندی و فایل‌های TLS را داشته باشد.

### پورت قبلاً در حال استفاده است

```bash
sudo ss -lntp
```

ممکن است فرایند دیگری از پورت WSS یا TCP عمومی استفاده کند. پورت آزاد انتخاب کنید یا سرویس متداخل را دوباره پیکربندی کنید.

### Agent به WSS وصل نمی‌شود

DNS، قوانین فایروال، وضعیت سرویس، پورت WSS، مسیر URL یعنی `/tunnel`، ساعت سیستم و اعتبار/نام گواهی را بررسی کنید. روی مبدأ این دستور فقط دسترسی TCP را می‌سنجد:

```bash
nc -vz tunnel.example.com 9443
```

موفقیت این آزمون به‌تنهایی TLS یا احراز هویت توکن را تأیید نمی‌کند.

### احراز هویت ناموفق است

مطمئن شوید توکن دو میزبان دقیقاً یکسان است و فاصله یا خط جدید اضافی ندارد. اگر احتمال افشا وجود دارد، توکن را در هر دو طرف تغییر دهید و سرویس‌ها را مجدداً راه‌اندازی کنید.

### پورت عمومی TCP وصل می‌شود اما برنامه کار نمی‌کند

فعال بودن سرویس هدف سمت مبدأ و دسترسی Agent به آن را بررسی کنید. همچنین مطمئن شوید کلاینت از پروتکل مورد انتظار سرویس هدف استفاده می‌کند. گزارش‌های هر دو میزبان و آزمون در سطح برنامه را بررسی کنید.

### قطع ارتباط، اتصال‌های فعال را مختل می‌کند

در نسخه فعلی، جریان‌هایی که از نشست WSS خراب‌شده عبور می‌کنند قطع می‌شوند. حلقه اتصال مجدد Agent می‌تواند نشست تازه‌ای برقرار کند، اما جریان‌های TCP فعال به‌صورت شفاف به نشست دیگر منتقل نمی‌شوند. وجود pool نشست‌ها تضمین‌کننده failover بدون قطعی نیست.

### صدور گواهی Let's Encrypt شکست می‌خورد

مطمئن شوید دامنه به سرور مقصد اشاره می‌کند و پورت TCP شماره `80` برای اعتبارسنجی HTTP-01 قابل دسترسی است. در صورت شکست، هنگام درخواست نصب‌کننده مسیر فایل‌های PEM موجود و قابل‌خواندن را وارد کنید.

### نصب‌کننده نمی‌تواند Go را دریافت کند

فرایند ساخت به Go نسخه 1.24 یا جدیدتر نیاز دارد. نصب‌کننده روی `amd64` و `arm64` تلاش می‌کند Go را از `go.dev` دریافت کند. اگر سرور به آن دسترسی ندارد، Go سازگار را دستی نصب و سپس نصب‌کننده را دوباره اجرا کنید.

هنگام گزارش اشکال، نسخه پروژه، نسخه سیستم‌عامل، پیکربندی پاک‌سازی‌شده، گزارش‌های مرتبط و مراحل بازتولید را ارائه دهید. **توکن‌ها، کلیدهای خصوصی و جزئیات حساس سرور را حذف کنید.**

## محدودیت‌های فعلی

این پروژه یک MVP اولیه از فاز دوم است و محدودیت‌های زیر را دارد:

- ممیزی امنیتی مستقل، آزمون نفوذ یا معیارسنجی بار در مقیاس تولید انجام نشده است.
- پیاده‌سازی WebSocket فقط زیرمجموعه موردنیاز پروژه را پشتیبانی می‌کند؛ پیام‌های تکه‌تکه‌شده و پیام‌های متنی پشتیبانی نمی‌شوند.
- pool نشست‌ها می‌تواند تا محدوده تنظیم‌شده افزایش یابد، اما کوچک نمی‌شود، نشست‌های بیکار را بازنشسته نمی‌کند و از jitter برای اتصال مجدد استفاده نمی‌کند.
- خرابی نشست WSS، جریان‌های همان نشست را قطع می‌کند؛ جریان فعال به نشست دیگری منتقل نمی‌شود.
- امتیازدهی سلامت یا failover هوشمند وجود ندارد.
- داشبورد وب، API مدیریتی، پایگاه داده یا پنل چندمستاجری وجود ندارد.
- فوروارد UDP، انتقال QUIC/KCP، رابط شبکه مجازی و قابلیت VPN لایه ۳ وجود ندارد.
- نصب‌کننده فایروال را پیکربندی نمی‌کند.
- تمدید و استقرار خودکار گواهی فقط زمانی تنظیم می‌شود که نصب‌کننده موفق به دریافت گواهی Let's Encrypt شود؛ گواهی دستی را مدیر باید مدیریت کند.

## ساختار پروژه

```text
shadows-tunnel/
├── cmd/shadows/             # نقطه ورود CLI
├── configs/                 # فایل‌های نمونه JSON برای سرور و Agent
├── deployments/systemd/     # قالب‌های سرویس systemd
├── docs/                    # یادداشت‌های پیاده‌سازی فازها
├── internal/
│   ├── config/              # بارگذاری و اعتبارسنجی JSON
│   ├── doctor/              # بررسی‌های تشخیصی محلی
│   ├── logging/             # تنظیم گزارش‌گیری
│   ├── transport/
│   │   ├── wss/             # انتقال WebSocket روی TLS
│   │   └── tcpmux/          # چندگانه‌سازی جریان‌های TCP
│   └── tunnel/reverse/      # نقش‌های مقصد و مبدأ
├── tests/                   # آزمون‌ها
├── .github/workflows/       # گردش‌کارهای CI
├── install.sh               # نصب‌کننده تعاملی لینوکس
├── go.mod
├── Makefile
├── LICENSE                  # متن کامل مجوز نرم‌افزار
└── README.md                # مستندات انگلیسی و فارسی
```

## مشارکت در توسعه

گزارش خطا، آزمون قابل بازتولید، بهبود مستندات و pull requestهای متمرکز مورد استقبال است.

- تغییرات را محدود و دلیل آن را روشن بیان کنید.
- برای تغییر رفتار، آزمون اضافه یا به‌روزرسانی کنید.
- هنگام تغییر گزینه‌ها، README و فایل‌های پیکربندی نمونه را نیز به‌روز کنید.
- اسرار یا پیکربندی تولید را در commitها قرار ندهید.
- پیش از ارسال pull request، قالب‌بندی، آزمون‌ها، آزمون race و `go vet` را اجرا کنید.
- تغییرات پروتکل و احراز هویت به دلیل تأثیر احتمالی بر سازگاری و امنیت نیازمند دقت ویژه‌اند.

## مجوز نرم‌افزار

پروژه قرار است تحت **GNU General Public License نسخه 3.0 (GPL-3.0)** منتشر شود. برای متن کامل و شرایط قانونی، فایل [`LICENSE`](LICENSE) را ببینید. پیش از انتشار، مطمئن شوید فایل `LICENSE` شامل متن کامل و بدون تغییر لایسنس GPL v3.0 است و حق اعطای مجوز برای تمام کدهای موجود را دارید.

GPL v3.0 استفاده، تغییر و بازتوزیع را با رعایت شرایط خود مجاز می‌کند. هنگام توزیع نسخه تغییر‌یافته، باید الزامات مربوط به ارائه کد منبع و اعلان‌های مجوز رعایت شوند. صرف اجرای نرم‌افزار به‌عنوان سرویس شبکه، به‌تنهایی همان الزام افشای منبع شبکه‌ای را که در GNU AGPL وجود دارد ایجاد نمی‌کند.

متن رسمی: https://www.gnu.org/licenses/gpl-3.0.html
