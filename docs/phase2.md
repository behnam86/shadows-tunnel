# Phase 2 — implementation notes

## Components

- `internal/transport/wss`: standard-library RFC 6455 binary WebSocket subset over verified TLS. Server-side upgrades require a bearer token. Frame size is bounded; masked client frames, unmasked server frames, ping/pong, and close frames are handled.
- `internal/transport/tcpmux`: multiplex session and stream abstraction over an ordered `net.Conn`. Each frame contains type, stream ID, and bounded payload length. The writer is serialized, incoming streams are queued with a fixed capacity, and stream count is limited.
- `internal/tunnel/reverse`: destination accepts authenticated WSS sessions and forwards public TCP clients as logical streams; source agent maintains outbound WSS sessions and dials its configured target for each accepted stream.
- `install.sh`: apt-based dependency bootstrap, Go bootstrap if needed, tests, vet, build, role prompt, token generation/input, config creation, and systemd enable/start.
- `deployments/systemd`: least-privilege service examples.

## Connection sequence

1. Agent verifies the destination certificate and performs a WSS upgrade with `Authorization: Bearer <token>`.
2. Destination validates the token and upgrades the connection.
3. Both peers create a multiplex session over the WebSocket connection.
4. Destination accepts a public TCP client and sends an `OPEN` frame for a new logical stream.
5. Agent accepts the stream, dials its local configured target, and copies bytes bidirectionally.
6. `DATA` frames carry bytes; `CLOSE` frames close a logical stream. Each WSS session has a configured maximum stream count.

## Configuration

Server fields: `role`, `wss_listen`, `listen`, `tls_cert`, `tls_key`, `token`, `max_connections`, `max_streams`.

Agent fields: `role`, `server_url`, `ca_file`, `target`, `token`, `min_pool`, `max_pool`, `max_streams`, `reconnect_min`, `reconnect_max`.

The WSS listener is distinct from the public TCP listener. `server_url` must use `wss://` and point to `/tunnel`. `ca_file` is optional; when supplied, the PEM CA is added to system roots. TLS verification is never disabled.

## Validation

The integration test creates a local TLS certificate, starts the destination and source agent, forwards a TCP echo target over WSS + multiplexing, and verifies the round trip. Run:

```sh
go test ./...
go test -race ./...
go vet ./...
go build ./cmd/shadows
```

## Known limitations

- The WebSocket implementation intentionally supports the protocol subset needed here; fragmentation and text messages are not supported.
- The agent starts `min_pool` workers and scales up when active streams reach about 75% of configured stream capacity, up to `max_pool`. It retains extra workers after scale-up; scale-down, idle retirement, reconnect jitter, and health scoring are not implemented.
- A WSS session failure terminates streams carried by that session; clients must reconnect at the application layer.
- The installer copies TLS files; certificate renewal automation is not included.
- No independent penetration test or production load benchmark has been run.
