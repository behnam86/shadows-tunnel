// Package wss implements the small RFC 6455 subset required by Shadows Tunnel,
// using only the Go standard library. It supports binary messages and control frames.
package wss

import (
	"bufio"
	"crypto/rand"
	"crypto/sha1"
	"crypto/subtle"
	"crypto/tls"
	"crypto/x509"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"
)

const maxFrameSize = 16 << 20
const wsGUID = "258EAFA5-E914-47DA-95CA-C5AB0DC85B11"

// Dial establishes a verified TLS connection and upgrades it to WebSocket.
func Dial(u *url.URL, token string, timeout time.Duration) (net.Conn, error) {
	return DialWithCA(u, token, timeout, "")
}

// DialWithCA optionally trusts a PEM CA file in addition to system roots.
func DialWithCA(u *url.URL, token string, timeout time.Duration, caFile string) (net.Conn, error) {
	if u == nil || u.Scheme != "wss" || u.Host == "" {
		return nil, errors.New("WSS URL must include wss scheme and host")
	}
	host := u.Host
	if !strings.Contains(host, ":") {
		host += ":443"
	}
	d := &net.Dialer{Timeout: timeout, KeepAlive: 30 * time.Second}
	tlsCfg := &tls.Config{MinVersion: tls.VersionTLS12, ServerName: u.Hostname()}
	if caFile != "" {
		pem, readErr := os.ReadFile(caFile)
		if readErr != nil {
			return nil, fmt.Errorf("read CA file: %w", readErr)
		}
		roots, poolErr := x509.SystemCertPool()
		if poolErr != nil || roots == nil {
			roots = x509.NewCertPool()
		}
		if !roots.AppendCertsFromPEM(pem) {
			return nil, errors.New("CA file contains no valid certificates")
		}
		tlsCfg.RootCAs = roots
	}
	raw, err := tls.DialWithDialer(d, "tcp", host, tlsCfg)
	if err != nil {
		return nil, fmt.Errorf("TLS dial: %w", err)
	}
	fail := func(e error) (net.Conn, error) { _ = raw.Close(); return nil, e }
	var nonce [16]byte
	if _, err := rand.Read(nonce[:]); err != nil {
		return fail(err)
	}
	key := base64.StdEncoding.EncodeToString(nonce[:])
	path := u.EscapedPath()
	if path == "" {
		path = "/"
	}
	if u.RawQuery != "" {
		path += "?" + u.RawQuery
	}
	if _, err := fmt.Fprintf(raw, "GET %s HTTP/1.1\r\nHost: %s\r\nUpgrade: websocket\r\nConnection: Upgrade\r\nSec-WebSocket-Key: %s\r\nSec-WebSocket-Version: 13\r\nAuthorization: Bearer %s\r\n\r\n", path, u.Host, key, token); err != nil {
		return fail(err)
	}
	br := bufio.NewReader(raw)
	resp, err := http.ReadResponse(br, &http.Request{Method: "GET"})
	if err != nil {
		return fail(fmt.Errorf("read WSS handshake: %w", err))
	}
	expected := acceptKey(key)
	if resp.StatusCode != http.StatusSwitchingProtocols || !strings.EqualFold(resp.Header.Get("Upgrade"), "websocket") || !strings.Contains(strings.ToLower(resp.Header.Get("Connection")), "upgrade") || resp.Header.Get("Sec-WebSocket-Accept") != expected {
		return fail(fmt.Errorf("WSS handshake rejected: %s", resp.Status))
	}
	return newConn(raw, br, true), nil
}

// Handler upgrades authenticated requests at /tunnel and passes the connection to onConn.
func Handler(token string, onConn func(net.Conn)) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/tunnel" {
			http.NotFound(w, r)
			return
		}
		if !constantTimeEqual(strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer "), token) {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		if !headerHasToken(r.Header, "Connection", "upgrade") || !strings.EqualFold(r.Header.Get("Upgrade"), "websocket") || r.Header.Get("Sec-WebSocket-Version") != "13" {
			http.Error(w, "websocket upgrade required", http.StatusBadRequest)
			return
		}
		key := r.Header.Get("Sec-WebSocket-Key")
		decoded, err := base64.StdEncoding.DecodeString(key)
		if err != nil || len(decoded) != 16 {
			http.Error(w, "invalid websocket key", http.StatusBadRequest)
			return
		}
		hj, ok := w.(http.Hijacker)
		if !ok {
			http.Error(w, "hijacking unavailable", http.StatusInternalServerError)
			return
		}
		nc, rw, err := hj.Hijack()
		if err != nil {
			return
		}
		_, err = fmt.Fprintf(rw, "HTTP/1.1 101 Switching Protocols\r\nUpgrade: websocket\r\nConnection: Upgrade\r\nSec-WebSocket-Accept: %s\r\n\r\n", acceptKey(key))
		if err == nil {
			err = rw.Flush()
		}
		if err != nil {
			_ = nc.Close()
			return
		}
		onConn(newConn(nc, rw.Reader, false))
	})
}

func headerHasToken(h http.Header, name, token string) bool {
	for _, v := range h.Values(name) {
		for _, part := range strings.Split(v, ",") {
			if strings.EqualFold(strings.TrimSpace(part), token) {
				return true
			}
		}
	}
	return false
}
func acceptKey(key string) string {
	s := sha1.Sum([]byte(key + wsGUID))
	return base64.StdEncoding.EncodeToString(s[:])
}
func constantTimeEqual(a, b string) bool {
	return subtle.ConstantTimeCompare([]byte(a), []byte(b)) == 1
}

type conn struct {
	net.Conn
	r       *bufio.Reader
	client  bool
	writeMu sync.Mutex
	readMu  sync.Mutex
	payload []byte
}

func newConn(c net.Conn, r *bufio.Reader, client bool) net.Conn {
	return &conn{Conn: c, r: r, client: client}
}
func (c *conn) Read(p []byte) (int, error) {
	c.readMu.Lock()
	defer c.readMu.Unlock()
	for len(c.payload) == 0 {
		fin, op, body, err := c.readFrame()
		if err != nil {
			return 0, err
		}
		if !fin {
			return 0, errors.New("fragmented WebSocket frames are not supported")
		}
		switch op {
		case 2:
			c.payload = body
		case 8:
			_ = c.writeFrame(8, body)
			return 0, io.EOF
		case 9:
			if err := c.writeFrame(10, body); err != nil {
				return 0, err
			}
		case 10:
			continue
		default:
			return 0, fmt.Errorf("unsupported WebSocket opcode %d", op)
		}
	}
	n := copy(p, c.payload)
	c.payload = c.payload[n:]
	return n, nil
}
func (c *conn) Write(p []byte) (int, error) {
	if len(p) == 0 {
		return 0, nil
	}
	if len(p) > maxFrameSize {
		return 0, errors.New("WebSocket write exceeds frame limit")
	}
	if err := c.writeFrame(2, p); err != nil {
		return 0, err
	}
	return len(p), nil
}
func (c *conn) readFrame() (bool, byte, []byte, error) {
	var h [2]byte
	if _, err := io.ReadFull(c.r, h[:]); err != nil {
		return false, 0, nil, err
	}
	fin := h[0]&0x80 != 0
	if h[0]&0x70 != 0 {
		return false, 0, nil, errors.New("WebSocket RSV bits must be zero")
	}
	op := h[0] & 0x0f
	masked := h[1]&0x80 != 0
	n := uint64(h[1] & 0x7f)
	if n == 126 {
		var b [2]byte
		if _, err := io.ReadFull(c.r, b[:]); err != nil {
			return false, 0, nil, err
		}
		n = uint64(binary.BigEndian.Uint16(b[:]))
	} else if n == 127 {
		var b [8]byte
		if _, err := io.ReadFull(c.r, b[:]); err != nil {
			return false, 0, nil, err
		}
		n = binary.BigEndian.Uint64(b[:])
		if n>>63 != 0 {
			return false, 0, nil, errors.New("invalid WebSocket length")
		}
	}
	if n > maxFrameSize {
		return false, 0, nil, errors.New("WebSocket frame too large")
	}
	if c.client && masked {
		return false, 0, nil, errors.New("server sent masked frame")
	}
	if !c.client && !masked {
		return false, 0, nil, errors.New("client sent unmasked frame")
	}
	var mask [4]byte
	if masked {
		if _, err := io.ReadFull(c.r, mask[:]); err != nil {
			return false, 0, nil, err
		}
	}
	body := make([]byte, int(n))
	if _, err := io.ReadFull(c.r, body); err != nil {
		return false, 0, nil, err
	}
	if masked {
		for i := range body {
			body[i] ^= mask[i&3]
		}
	}
	if op >= 8 && (!fin || n > 125) {
		return false, 0, nil, errors.New("invalid WebSocket control frame")
	}
	return fin, op, body, nil
}
func (c *conn) writeFrame(op byte, p []byte) error {
	c.writeMu.Lock()
	defer c.writeMu.Unlock()
	var h [14]byte
	h[0] = 0x80 | op
	n := len(p)
	off := 2
	maskBit := byte(0)
	if c.client {
		maskBit = 0x80
	}
	if n < 126 {
		h[1] = maskBit | byte(n)
	} else if n <= 65535 {
		h[1] = maskBit | 126
		binary.BigEndian.PutUint16(h[2:4], uint16(n))
		off = 4
	} else {
		h[1] = maskBit | 127
		binary.BigEndian.PutUint64(h[2:10], uint64(n))
		off = 10
	}
	body := p
	if c.client {
		if _, err := rand.Read(h[off : off+4]); err != nil {
			return err
		}
		mask := h[off : off+4]
		off += 4
		body = append([]byte(nil), p...)
		for i := range body {
			body[i] ^= mask[i&3]
		}
	}
	if _, err := c.Conn.Write(h[:off]); err != nil {
		return err
	}
	for len(body) > 0 {
		n, err := c.Conn.Write(body)
		if err != nil {
			return err
		}
		if n == 0 {
			return io.ErrShortWrite
		}
		body = body[n:]
	}
	return nil
}
