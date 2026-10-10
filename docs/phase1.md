# Phase 1 implementation notes

## Components delivered

- `internal/config`: strict JSON decoder, role-specific validation, address validation, and reconnect duration parsing.
- `internal/logging`: structured JSON logger based on `log/slog`.
- `internal/transport/tcp`: TCP dial/listen primitive.
- `internal/tunnel/engine`: transport-independent lifecycle types and interface.
- `internal/tunnel/reverse`: reverse TCP server/agent protocol and stream bridging.
- `internal/doctor`: read-only runtime and loopback checks.
- `cmd/shadows`: CLI entry points.

## Wire protocol (Phase 1 only)

Control and data connections are newline-delimited JSON for a bounded handshake/control message. The first message is one of:

- `{"type":"control","token":"..."}` — agent control channel.
- `{"type":"data","token":"...","id":"..."}` — data channel for a pending public client stream.
- `{"type":"open","id":"..."}` — server request sent over the established control channel.

The token is compared without early exit on equal-length values. This is not a complete authenticated protocol: traffic is plaintext, no key rotation or replay protection exists, and TLS/WSS is a later phase.

## Reverse TCP stream sequence

1. Server accepts a client on `listen` and creates a random stream ID.
2. Server registers a pending stream and sends `open` to the active agent.
3. Agent dials the configured local target.
4. Agent opens an outbound connection to the server's control port and sends a `data` handshake containing the stream ID.
5. Server binds that data connection to the pending client connection.
6. Both ends copy bytes bidirectionally until either direction ends.

One active control agent is supported per server process. A reconnecting agent replaces the previous control session. This behavior is intentionally simple for Phase 1 and will be revisited with node identity and pairing in later phases.

## Validation performed

Run `go test ./...`, `go test -race ./...`, `go vet ./...`, and `go build ./cmd/shadows` on the selected Go toolchain. The integration test starts an echo target, the server, and an agent on loopback and verifies a complete reverse TCP round trip.
