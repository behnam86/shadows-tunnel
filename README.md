# Shadows Tunnel — Phase 2

Shadows Tunnel is an independently implemented, Linux-first reverse TCP tunneling platform written in Go. Phase 2 adds verified TLS WebSocket transport (WSS), bounded TCP multiplexing, a configurable outbound session pool, reconnect backoff, and role-aware `systemd` installation.

> **Repository:** `https://github.com/behnam86/shadows-tunnel`
>
> Before publishing, create this repository under the `behnam86` GitHub account and select/add a license.

## Topology and roles

```text
Client / Internet
      |
      v
Destination server (outside VPS)
  public TCP listener :8443
      |
  WSS + TLS :9443/tunnel
      ||  persistent authenticated WSS sessions
      ||  multiple logical TCP streams per session
      v
Source agent (Iran-side VPS; outbound-only tunnel connection)
      |
      v
Configured local target, e.g. 127.0.0.1:443
```

- **Destination / outside VPS:** accepts WSS agent connections, terminates TLS, and exposes the public TCP listener.
- **Source / Iran-side VPS:** initiates outbound WSS connections to the destination and dials the locally configured target for each logical stream.
- The agent does not need an inbound control port. The destination WSS port must be reachable from the source server.
- This is reverse TCP forwarding, not a full-device VPN or Layer-3 tunnel.

## Phase 2 features

- Standard-library-only Go implementation; no third-party Go modules are required.
- WSS using TLS 1.2+ with certificate verification on the client.
- Bearer-token authentication at the WSS upgrade endpoint.
- Binary WebSocket framing with bounded frame sizes and control-frame handling.
- Multiplexed logical streams over each WSS connection; configurable per-session stream limit.
- Configurable outbound WSS session pool (`min_pool`, `max_pool`): starts at `min_pool`, scales up when active streams approach 75% of capacity, and retains added sessions to avoid churn.
- Exponential reconnect backoff with bounded delay.
- Systemd service units and an interactive installer that installs build tools/Go if required, runs tests and `go vet`, builds the binary, asks for the node role, writes configuration, and enables the service.
- CLI: `version`, `doctor`, `server`, `agent`, and `tunnel test`.

## One-command installation from GitHub

After publishing the repository and updating the owner/repository in `install.sh`, run this on **each VPS**:

```bash
curl -fsSL https://raw.githubusercontent.com/behnam86/shadows-tunnel/main/install.sh | sudo bash
```

The command assumes the public repository is named `shadows-tunnel` under `behnam86`. The installer supports Ubuntu 22.04/24.04 and Debian 12+ best-effort. It uses the installed Go if it is new enough; otherwise it downloads a current Go release from `go.dev` (minimum installer version: 1.24). A server that cannot reach the Go download host must have Go >= 1.24 installed first. The installer intentionally does **not** change firewall rules automatically.

You can also install from a cloned checkout:

```bash
git clone https://github.com/behnam86/shadows-tunnel.git
cd shadows-tunnel
sudo ./install.sh
```

### Setup order

1. On the **destination/outside VPS**, run the installer and choose `1`.
2. Enter WSS bind address (default `0.0.0.0:9443`) and public TCP listen address (default `0.0.0.0:8443`). The installer can issue a Let’s Encrypt certificate automatically if you provide a DNS hostname pointing to the destination VPS and allow TCP port 80 for validation; otherwise, provide existing certificate/key PEM paths. The installer generates a high-entropy token and prints it once. Copy it securely to the source VPS.
3. Permit the WSS port and intended public listener port in the VPS provider firewall and host firewall. Only expose the public listener if intended.
4. On the **source/Iran-side VPS**, run the installer and choose `2`. Enter `wss://YOUR_DOMAIN:9443/tunnel`, the local target such as `127.0.0.1:443`, and the exact token from step 2. Configure the pool limits.
5. Check service logs on both hosts:

```bash
sudo systemctl status shadows.service --no-pager
sudo journalctl -u shadows.service -f
```

The destination installer copies the certificate and private key to `/etc/shadows/tls/`. When Let’s Encrypt is selected, a deploy hook updates the copied certificate and restarts the service after renewal. For manually supplied PEM files, renewal remains your responsibility.

## Build and tests

```bash
gofmt -w .
go test ./...
go test -race ./...
go vet ./...
go build -trimpath -ldflags='-s -w' -o bin/shadows ./cmd/shadows
./bin/shadows version
./bin/shadows doctor
```

## Configuration examples

See `configs/server.example.json` and `configs/agent.example.json`. Config parsing is strict; unknown fields are rejected.

Destination server:

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

Source agent:

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

`ca_file` is optional. Leave it empty to use system trust roots; set it to a PEM CA file when using a private CA. Do not disable certificate verification in production.

## CLI

```bash
shadows version
shadows doctor
shadows server -config /etc/shadows/server.json
shadows agent -config /etc/shadows/agent.json
shadows tunnel test -address 127.0.0.1:8443
```

## Security and operational notes

- Use a valid publicly trusted certificate for the WSS hostname. The agent verifies TLS certificates by default.
- Use a random token, keep config files root-owned and readable only by the `shadows` group, and rotate the token after suspected exposure.
- Do not expose the WSS endpoint or public listener beyond what your deployment requires. The installer does not modify firewall rules.
- The custom WebSocket framing is intentionally bounded, but this implementation has not undergone an independent security audit. Run it only on systems you own or are authorized to administer.
- A connection pool improves resilience and stream concurrency but does not guarantee seamless survival of an already active stream if its underlying WSS session dies.
- The pool currently scales up based on active stream count but does not scale back down, retire idle sessions, add reconnect jitter, or use health scoring.
- TLS certificate renewal, metrics export, health scoring/failover, UDP/QUIC/KCP, pairing/controller/API, SQLite, and web dashboard are not included in Phase 2.
- Phase 1 documentation remains in `docs/phase1.md` for historical context; Phase 2 protocol behavior is documented in `docs/phase2.md`.

## License

No license has been selected yet. Add an explicit license before public distribution.
