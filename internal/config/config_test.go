package config

import (
	"strings"
	"testing"
)

func TestDecodeServerConfig(t *testing.T) {
	input := `{"role":"server","wss_listen":"127.0.0.1:9001","listen":"127.0.0.1:9002","tls_cert":"cert.pem","tls_key":"key.pem","token":"long-secret","max_connections":10}`
	cfg, err := Decode(strings.NewReader(input))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Role != "server" || cfg.MaxConnections != 10 {
		t.Fatalf("unexpected config: %+v", cfg)
	}
}
func TestDecodeRejectsUnknownField(t *testing.T) {
	input := `{"role":"server","wss_listen":"127.0.0.1:9001","listen":"127.0.0.1:9002","tls_cert":"c","tls_key":"k","token":"x","unexpected":true}`
	if _, err := Decode(strings.NewReader(input)); err == nil {
		t.Fatal("expected unknown field error")
	}
}
func TestDecodeRejectsSameAddress(t *testing.T) {
	input := `{"role":"server","wss_listen":"127.0.0.1:9001","listen":"127.0.0.1:9001","tls_cert":"c","tls_key":"k","token":"x"}`
	if _, err := Decode(strings.NewReader(input)); err == nil {
		t.Fatal("expected address conflict error")
	}
}
func TestDecodeAgentPoolBounds(t *testing.T) {
	input := `{"role":"agent","server_url":"wss://example.com/tunnel","target":"127.0.0.1:443","token":"x","min_pool":2,"max_pool":4}`
	cfg, err := Decode(strings.NewReader(input))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.MaxPool != 4 {
		t.Fatalf("unexpected pool: %+v", cfg)
	}
}

func TestExampleConfigsDecode(t *testing.T) {
	for _, path := range []string{"../../configs/server.example.json", "../../configs/agent.example.json"} {
		if _, err := Load(path); err != nil {
			t.Errorf("example config %s: %v", path, err)
		}
	}
}
