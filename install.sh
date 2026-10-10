#!/usr/bin/env bash
set -Eeuo pipefail

# Shadows Tunnel Phase 2 installer for Ubuntu 22.04+, Ubuntu 24.04+, and Debian 12+.
# Supports both: sudo ./install.sh and curl .../install.sh | sudo bash
REPO_URL="${SHADOWS_REPO_URL:-https://github.com/behnam86/shadows-tunnel.git}"
SRC_DIR="${SHADOWS_SRC_DIR:-/opt/shadows-tunnel-src}"
BIN="/usr/local/bin/shadows"
CONF_DIR="/etc/shadows"
UNIT_DIR="/etc/systemd/system"
MIN_GO="1.24"

say(){ printf '\n[shadows] %s\n' "$*"; }
die(){ printf '\n[shadows] ERROR: %s\n' "$*" >&2; exit 1; }
need_root(){ [[ "$(id -u)" -eq 0 ]] || die "Run as root: sudo ./install.sh"; }

version_ge(){ # version_ge 1.23.0 1.23
  [[ "$(printf '%s\n%s\n' "$2" "$1" | sort -V | head -n1)" == "$2" ]]
}
install_go(){
  if command -v go >/dev/null 2>&1; then
    local v; v="$(go version | awk '{print $3}' | sed 's/^go//')"
    if version_ge "$v" "$MIN_GO"; then say "Using Go $v"; return; fi
    say "Existing Go $v is older than $MIN_GO; installing a current Go release."
  else
    say "Go not found; installing a current Go release."
  fi
  local arch goarch version url tmp
  case "$(uname -m)" in x86_64|amd64) goarch=amd64;; aarch64|arm64) goarch=arm64;; *) die "Unsupported CPU architecture: $(uname -m)";; esac
  version="${GO_VERSION:-}"
  if [[ -z "$version" ]]; then
    version="$(curl -fsSL --connect-timeout 8 --max-time 15 https://go.dev/VERSION?m=text 2>/dev/null | sed -n '1p' | sed 's/^go//')" || true
  fi
  [[ -n "$version" ]] || version="1.24.6"
  url="https://go.dev/dl/go${version}.linux-${goarch}.tar.gz"
  tmp="$(mktemp -d)"
  if ! curl -fL --retry 2 --connect-timeout 10 "$url" -o "$tmp/go.tgz"; then
    rm -rf "$tmp"
    die "Cannot download Go from go.dev. Install Go >= $MIN_GO manually or set GO_VERSION to an available release, then rerun."
  fi
  rm -rf /usr/local/go
  tar -C /usr/local -xzf "$tmp/go.tgz"
  rm -rf "$tmp"
  ln -sf /usr/local/go/bin/go /usr/local/bin/go
  ln -sf /usr/local/go/bin/gofmt /usr/local/bin/gofmt
  export PATH="/usr/local/go/bin:/usr/local/bin:$PATH"
  say "Installed $(go version)"
}

need_root
[[ -r /etc/os-release ]] || die "Cannot identify Linux distribution."
. /etc/os-release
case "${ID:-}:${VERSION_ID:-}" in ubuntu:22.04|ubuntu:24.04|ubuntu:25.04|ubuntu:25.10|debian:12|debian:13) ;; *) say "Untested distribution ${PRETTY_NAME:-unknown}; continuing best-effort.";; esac
command -v apt-get >/dev/null 2>&1 || die "This installer currently supports apt-based Linux distributions."
export DEBIAN_FRONTEND=noninteractive
apt-get update -y
apt-get install -y ca-certificates curl git openssl tar gzip
install_go

SCRIPT_DIR=""
if [[ -f "${BASH_SOURCE[0]}" ]]; then SCRIPT_DIR="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"; fi
if [[ -n "$SCRIPT_DIR" && -f "$SCRIPT_DIR/go.mod" ]]; then
  SRC_DIR="$SCRIPT_DIR"
else
  if [[ "$REPO_URL" != https://github.com/*.git ]]; then die "SHADOWS_REPO_URL must be an HTTPS GitHub .git URL."; fi
  if [[ -d "$SRC_DIR/.git" ]]; then
    say "Updating source checkout at $SRC_DIR"
    git -C "$SRC_DIR" pull --ff-only
  else
    say "Cloning $REPO_URL"
    rm -rf "$SRC_DIR"
    git clone --depth 1 "$REPO_URL" "$SRC_DIR"
  fi
fi
cd "$SRC_DIR"
[[ -f go.mod ]] || die "go.mod not found in $SRC_DIR"
say "Building Shadows Tunnel"
go test ./...
go vet ./...
go build -trimpath -ldflags='-s -w' -o "$BIN" ./cmd/shadows
chmod 0755 "$BIN"

if ! getent group shadows >/dev/null; then groupadd --system shadows; fi
if ! id shadows >/dev/null 2>&1; then useradd --system --gid shadows --home-dir /var/lib/shadows --shell /usr/sbin/nologin shadows; fi
install -d -o root -g shadows -m 0750 "$CONF_DIR"
install -d -o shadows -g shadows -m 0750 /var/lib/shadows
install -d -o root -g root -m 0755 "$UNIT_DIR"

printf '\nSelect this machine role:\n  1) Destination server (outside/public VPS)\n  2) Source agent (Iran-side VPS)\n'
read -r -p 'Role [1/2]: ' ROLE
case "$ROLE" in
  1)
    read -r -p 'WSS bind address [0.0.0.0:9443]: ' WSS_ADDR; WSS_ADDR="${WSS_ADDR:-0.0.0.0:9443}"
    [[ "$WSS_ADDR" != *'"'* && "$WSS_ADDR" != *\\* ]] || die "Invalid characters in WSS bind address."
    read -r -p 'Public TCP listen address [0.0.0.0:8443]: ' PUB_ADDR; PUB_ADDR="${PUB_ADDR:-0.0.0.0:8443}"
    [[ "$PUB_ADDR" != *'"'* && "$PUB_ADDR" != *\\* ]] || die "Invalid characters in public bind address."
    CERT_DOMAIN=""
    CERTBOT_ISSUED=0
    read -r -p "WSS DNS hostname for automatic Let's Encrypt setup (blank to use existing PEM files): " CERT_DOMAIN
    if [[ -n "$CERT_DOMAIN" ]]; then
      [[ "$CERT_DOMAIN" =~ ^[A-Za-z0-9.-]+$ ]] || die "Enter a DNS hostname only, without scheme or path."
      read -r -p 'Email address for certificate expiry notices: ' CERT_EMAIL
      apt-get install -y certbot
      if certbot certonly --standalone --preferred-challenges http -d "$CERT_DOMAIN" --non-interactive --agree-tos -m "$CERT_EMAIL"; then
        CERT_IN="/etc/letsencrypt/live/$CERT_DOMAIN/fullchain.pem"
        KEY_IN="/etc/letsencrypt/live/$CERT_DOMAIN/privkey.pem"
        CERTBOT_ISSUED=1
      else
        say "Automatic certificate issuance failed. Check DNS and ensure TCP port 80 is reachable, then provide existing certificate files."
        read -r -p 'Path to TLS fullchain/certificate PEM: ' CERT_IN
        read -r -p 'Path to TLS private-key PEM: ' KEY_IN
      fi
    else
      read -r -p 'Path to TLS fullchain/certificate PEM: ' CERT_IN
      read -r -p 'Path to TLS private-key PEM: ' KEY_IN
    fi
    [[ -r "$CERT_IN" && -r "$KEY_IN" ]] || die "Certificate and key must exist and be readable."
    install -d -o root -g shadows -m 0750 "$CONF_DIR/tls"
    install -o root -g shadows -m 0640 "$CERT_IN" "$CONF_DIR/tls/fullchain.pem"
    install -o root -g shadows -m 0640 "$KEY_IN" "$CONF_DIR/tls/privkey.pem"
    [[ "$WSS_ADDR" != "$PUB_ADDR" ]] || die "WSS and public listener addresses must differ."
    read -r -p 'Max public TCP connections [1000]: ' MAX_CONN; MAX_CONN="${MAX_CONN:-1000}"
    read -r -p 'Max multiplexed streams per WSS session [256]: ' MAX_STREAMS; MAX_STREAMS="${MAX_STREAMS:-256}"
    [[ "$MAX_CONN" =~ ^[1-9][0-9]{0,5}$ && "$MAX_CONN" -le 100000 ]] || die "Max connections must be between 1 and 100000."
    [[ "$MAX_STREAMS" =~ ^[1-9][0-9]{0,5}$ && "$MAX_STREAMS" -le 100000 ]] || die "Max streams must be between 1 and 100000."
    TOKEN="$(openssl rand -hex 32)"
    cat > "$CONF_DIR/server.json" <<JSON
{
  "role": "server",
  "wss_listen": "$WSS_ADDR",
  "listen": "$PUB_ADDR",
  "tls_cert": "$CONF_DIR/tls/fullchain.pem",
  "tls_key": "$CONF_DIR/tls/privkey.pem",
  "token": "$TOKEN",
  "max_connections": $MAX_CONN,
  "max_streams": $MAX_STREAMS
}
JSON
    chown root:shadows "$CONF_DIR/server.json"; chmod 0640 "$CONF_DIR/server.json"
    install -m 0644 deployments/systemd/shadows-server.service "$UNIT_DIR/shadows.service"
    if [[ "${CERTBOT_ISSUED:-0}" -eq 1 ]]; then
      install -d -m 0755 /etc/letsencrypt/renewal-hooks/deploy
      cat > /etc/letsencrypt/renewal-hooks/deploy/shadows-tunnel-cert.sh <<HOOK
#!/usr/bin/env bash
set -eu
install -o root -g shadows -m 0640 /etc/letsencrypt/live/$CERT_DOMAIN/fullchain.pem /etc/shadows/tls/fullchain.pem
install -o root -g shadows -m 0640 /etc/letsencrypt/live/$CERT_DOMAIN/privkey.pem /etc/shadows/tls/privkey.pem
systemctl restart shadows.service
HOOK
      chmod 0750 /etc/letsencrypt/renewal-hooks/deploy/shadows-tunnel-cert.sh
    fi
    say "Server role configured. Save this token securely and enter it on the Iran-side agent:"
    printf '\nTOKEN=%s\n\n' "$TOKEN"
    say "Open TCP $WSS_ADDR (WSS) and $PUB_ADDR (public listener) in your provider firewall."
    SERVICE="shadows.service"
    ;;
  2)
    read -r -p 'Destination WSS URL (e.g. wss://tunnel.example.com:9443/tunnel): ' SERVER_URL
    [[ "$SERVER_URL" == wss://* && "$SERVER_URL" != *'"'* && "$SERVER_URL" != *\\* ]] || die "server_url must be a valid wss:// URL without quotes or backslashes"
    read -r -p 'Local target address on this source server [127.0.0.1:443]: ' TARGET; TARGET="${TARGET:-127.0.0.1:443}"
    [[ "$TARGET" != *'"'* && "$TARGET" != *\\* ]] || die "Invalid characters in target address."
    read -r -s -p 'Paste the token printed by destination-server setup: ' TOKEN; printf '\n'
    [[ "$TOKEN" =~ ^[a-fA-F0-9]{64}$ ]] || die "Token must be the 64-character hexadecimal token generated on the destination server."
    read -r -p 'Minimum WSS pool sessions [2]: ' MIN_POOL; MIN_POOL="${MIN_POOL:-2}"
    read -r -p 'Maximum WSS pool sessions [4]: ' MAX_POOL; MAX_POOL="${MAX_POOL:-4}"
    read -r -p 'Max multiplexed streams per session [256]: ' MAX_STREAMS; MAX_STREAMS="${MAX_STREAMS:-256}"
    [[ "$MIN_POOL" =~ ^[1-9][0-9]?$ && "$MIN_POOL" -le 64 ]] || die "Minimum pool must be between 1 and 64."
    [[ "$MAX_POOL" =~ ^[1-9][0-9]?$ && "$MAX_POOL" -le 64 && "$MAX_POOL" -ge "$MIN_POOL" ]] || die "Maximum pool must be between min_pool and 64."
    [[ "$MAX_STREAMS" =~ ^[1-9][0-9]{0,5}$ && "$MAX_STREAMS" -le 100000 ]] || die "Max streams must be between 1 and 100000."
    cat > "$CONF_DIR/agent.json" <<JSON
{
  "role": "agent",
  "server_url": "$SERVER_URL",
  "target": "$TARGET",
  "token": "$TOKEN",
  "min_pool": $MIN_POOL,
  "max_pool": $MAX_POOL,
  "max_streams": $MAX_STREAMS,
  "reconnect_min": "1s",
  "reconnect_max": "30s"
}
JSON
    chown root:shadows "$CONF_DIR/agent.json"; chmod 0640 "$CONF_DIR/agent.json"
    install -m 0644 deployments/systemd/shadows-agent.service "$UNIT_DIR/shadows.service"
    SERVICE="shadows.service"
    ;;
  *) die "Choose 1 or 2.";;
esac

"$BIN" doctor
"$BIN" version
systemctl daemon-reload
systemctl enable --now "$SERVICE"
sleep 1
systemctl --no-pager --full status "$SERVICE" || true
say "Installation finished. Logs: journalctl -u $SERVICE -f"
say "Config: $CONF_DIR/$([[ "$ROLE" == 1 ]] && echo server || echo agent).json"
