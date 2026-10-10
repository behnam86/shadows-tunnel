package reverse

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"fmt"
	"io"
	"log/slog"
	"math/big"
	"net"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

func freeAddress(t *testing.T) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	a := ln.Addr().String()
	_ = ln.Close()
	return a
}
func testCertificate(t *testing.T) (string, string, string) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	tmpl := x509.Certificate{SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "localhost"}, DNSNames: []string{"localhost"}, NotBefore: time.Now().Add(-time.Minute), NotAfter: time.Now().Add(time.Hour), KeyUsage: x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}, BasicConstraintsValid: true}
	der, err := x509.CreateCertificate(rand.Reader, &tmpl, &tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	certDER, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	cert := filepath.Join(dir, "cert.pem")
	keyFile := filepath.Join(dir, "key.pem")
	ca := filepath.Join(dir, "ca.pem")
	if err = os.WriteFile(cert, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), 0600); err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(ca, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), 0600); err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(keyFile, pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: certDER}), 0600); err != nil {
		t.Fatal(err)
	}
	return cert, keyFile, ca
}
func TestReverseTunnelEcho(t *testing.T) {
	targetLn, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer targetLn.Close()
	go func() {
		for {
			c, e := targetLn.Accept()
			if e != nil {
				return
			}
			go func(c net.Conn) { defer c.Close(); _, _ = io.Copy(c, c) }(c)
		}
	}()
	wssAddr, publicAddr := freeAddress(t), freeAddress(t)
	cert, key, ca := testCertificate(t)
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	server := NewServer(wssAddr, publicAddr, "test-secret", cert, key, 20, 32, logger)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	serverErr := make(chan error, 1)
	go func() { serverErr <- server.Run(ctx) }()
	deadline := time.Now().Add(4 * time.Second)
	for time.Now().Before(deadline) {
		c, e := net.DialTimeout("tcp", publicAddr, 50*time.Millisecond)
		if e == nil {
			_ = c.Close()
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	agentCtx, agentCancel := context.WithCancel(ctx)
	defer agentCancel()
	agent := &Agent{ServerURL: "wss://localhost:" + portOf(wssAddr) + "/tunnel", TargetAddr: targetLn.Addr().String(), Token: "test-secret", CAFile: ca, MinPool: 1, MaxPool: 2, MaxStreams: 32, ReconnectMin: 20 * time.Millisecond, ReconnectMax: 100 * time.Millisecond, Logger: logger}
	go func() { _ = agent.Run(agentCtx) }()
	time.Sleep(300 * time.Millisecond)
	var client net.Conn
	for time.Now().Before(deadline) {
		client, err = net.DialTimeout("tcp", publicAddr, 50*time.Millisecond)
		if err == nil {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if err != nil {
		t.Fatalf("connect public listener: %v", err)
	}
	defer client.Close()
	_ = client.SetDeadline(time.Now().Add(3 * time.Second))
	payload := []byte("reverse-tunnel-phase2-mux-ok")
	if _, err := client.Write(payload); err != nil {
		t.Fatal(err)
	}
	got := make([]byte, len(payload))
	if _, err := io.ReadFull(client, got); err != nil {
		t.Fatal(err)
	}
	if string(got) != string(payload) {
		t.Fatalf("echo mismatch: %q", got)
	}
	// Exercise concurrent logical streams over the pooled WSS sessions.
	var clients sync.WaitGroup
	for i := 0; i < 6; i++ {
		clients.Add(1)
		go func(i int) {
			defer clients.Done()
			c, e := net.DialTimeout("tcp", publicAddr, time.Second)
			if e != nil {
				t.Errorf("concurrent client %d connect: %v", i, e)
				return
			}
			defer c.Close()
			_ = c.SetDeadline(time.Now().Add(3 * time.Second))
			want := []byte(fmt.Sprintf("mux-stream-%d", i))
			if _, e = c.Write(want); e != nil {
				t.Errorf("concurrent client %d write: %v", i, e)
				return
			}
			buf := make([]byte, len(want))
			if _, e = io.ReadFull(c, buf); e != nil {
				t.Errorf("concurrent client %d read: %v", i, e)
				return
			}
			if string(buf) != string(want) {
				t.Errorf("concurrent client %d got %q", i, buf)
			}
		}(i)
	}
	clients.Wait()
	cancel()
	select {
	case <-serverErr:
	case <-time.After(2 * time.Second):
		t.Fatal("server did not stop after cancellation")
	}
}
func portOf(addr string) string { _, p, _ := net.SplitHostPort(addr); return p }
