// Package config loads and validates Shadows Tunnel configuration files.
package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/url"
	"os"
	"strings"
	"time"
)

// Config contains configuration for either a server or an agent.
type Config struct {
	Role            string        `json:"role"`
	WSSListen       string        `json:"wss_listen,omitempty"`
	TLSCert         string        `json:"tls_cert,omitempty"`
	TLSKey          string        `json:"tls_key,omitempty"`
	ServerURL       string        `json:"server_url,omitempty"`
	CAFile          string        `json:"ca_file,omitempty"`
	MinPool         int           `json:"min_pool,omitempty"`
	MaxPool         int           `json:"max_pool,omitempty"`
	MaxStreams      int           `json:"max_streams,omitempty"`
	Listen          string        `json:"listen,omitempty"`
	Target          string        `json:"target,omitempty"`
	Token           string        `json:"token"`
	MaxConnections  int           `json:"max_connections,omitempty"`
	ReconnectMin    time.Duration `json:"-"`
	ReconnectMax    time.Duration `json:"-"`
	ReconnectMinRaw string        `json:"reconnect_min,omitempty"`
	ReconnectMaxRaw string        `json:"reconnect_max,omitempty"`
}

// Load reads a JSON configuration file, rejects unknown fields, and validates it.
func Load(path string) (Config, error) {
	f, err := os.Open(path)
	if err != nil {
		return Config{}, fmt.Errorf("open config: %w", err)
	}
	defer f.Close()
	return Decode(f)
}

// Decode parses and validates a JSON configuration document.
func Decode(r io.Reader) (Config, error) {
	var c Config
	dec := json.NewDecoder(r)
	dec.DisallowUnknownFields()
	if err := dec.Decode(&c); err != nil {
		return Config{}, fmt.Errorf("decode config: %w", err)
	}
	var extra any
	if err := dec.Decode(&extra); !errors.Is(err, io.EOF) {
		if err == nil {
			return Config{}, errors.New("decode config: multiple JSON values are not allowed")
		}
		return Config{}, fmt.Errorf("decode config trailing data: %w", err)
	}
	if c.ReconnectMinRaw == "" {
		c.ReconnectMinRaw = "1s"
	}
	if c.ReconnectMaxRaw == "" {
		c.ReconnectMaxRaw = "30s"
	}
	var err error
	c.ReconnectMin, err = time.ParseDuration(c.ReconnectMinRaw)
	if err != nil {
		return Config{}, fmt.Errorf("invalid reconnect_min: %w", err)
	}
	c.ReconnectMax, err = time.ParseDuration(c.ReconnectMaxRaw)
	if err != nil {
		return Config{}, fmt.Errorf("invalid reconnect_max: %w", err)
	}
	if err := c.Validate(); err != nil {
		return Config{}, err
	}
	return c, nil
}

// Validate checks role-specific fields and basic resource bounds.
func (c Config) Validate() error {
	if c.Role != "server" && c.Role != "agent" {
		return errors.New("role must be either 'server' or 'agent'")
	}
	if strings.TrimSpace(c.Token) == "" || strings.ContainsAny(c.Token, "\r\n") {
		return errors.New("token must be non-empty and must not contain newlines")
	}
	if c.MaxStreams < 0 || c.MaxStreams > 100000 {
		return errors.New("max_streams must be between 1 and 100000, or 0 for default")
	}
	if c.Role == "server" {
		if err := validateTCPAddr("wss_listen", c.WSSListen); err != nil {
			return err
		}
		if err := validateTCPAddr("listen", c.Listen); err != nil {
			return err
		}
		if strings.TrimSpace(c.TLSCert) == "" || strings.TrimSpace(c.TLSKey) == "" {
			return errors.New("tls_cert and tls_key are required for WSS")
		}
		if c.WSSListen == c.Listen {
			return errors.New("wss_listen and listen must use different addresses")
		}
		if c.MaxConnections < 0 || c.MaxConnections > 100000 {
			return errors.New("max_connections must be between 1 and 100000, or 0 for default")
		}
	} else {
		u, err := url.Parse(c.ServerURL)
		if err != nil || u.Scheme != "wss" || u.Host == "" {
			return errors.New("server_url must be a valid wss:// URL")
		}
		if err := validateTCPAddr("target", c.Target); err != nil {
			return err
		}
		if c.ReconnectMin <= 0 || c.ReconnectMax < c.ReconnectMin {
			return errors.New("reconnect bounds must be positive and reconnect_max >= reconnect_min")
		}
		if c.MinPool < 1 || c.MinPool > 64 {
			return errors.New("min_pool must be between 1 and 64")
		}
		if c.MaxPool < c.MinPool || c.MaxPool > 64 {
			return errors.New("max_pool must be >= min_pool and <= 64")
		}
	}

	return nil
}

func validateTCPAddr(name, addr string) error {
	if strings.TrimSpace(addr) == "" {
		return fmt.Errorf("%s is required", name)
	}
	if _, _, err := net.SplitHostPort(addr); err != nil {
		return fmt.Errorf("%s must be host:port: %w", name, err)
	}
	return nil
}
