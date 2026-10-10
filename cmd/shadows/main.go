// Command shadows runs the Shadows Tunnel CLI, server, or agent.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/behnam86/shadows-tunnel/internal/config"
	"github.com/behnam86/shadows-tunnel/internal/doctor"
	"github.com/behnam86/shadows-tunnel/internal/logging"
	"github.com/behnam86/shadows-tunnel/internal/tunnel/reverse"
)

const version = "0.2.0-phase2"

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	if len(args) == 0 {
		printUsage()
		return errors.New("command required")
	}
	switch args[0] {
	case "version":
		fmt.Printf("Shadows Tunnel %s\n", version)
		return nil
	case "doctor":
		doctor.Run()
		return nil
	case "server":
		return runServer(args[1:])
	case "agent":
		return runAgent(args[1:])
	case "tunnel":
		return runTunnel(args[1:])
	case "help", "-h", "--help":
		printUsage()
		return nil
	default:
		printUsage()
		return fmt.Errorf("unknown command %q", args[0])
	}
}

func runServer(args []string) error {
	fs := flag.NewFlagSet("server", flag.ContinueOnError)
	path := fs.String("config", "", "path to server JSON config")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *path == "" {
		return errors.New("-config is required")
	}
	cfg, err := config.Load(*path)
	if err != nil {
		return err
	}
	if cfg.Role != "server" {
		return errors.New("config role must be 'server'")
	}
	logger := logging.New(slog.LevelInfo)
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	s := reverse.NewServer(cfg.WSSListen, cfg.Listen, cfg.Token, cfg.TLSCert, cfg.TLSKey, cfg.MaxConnections, cfg.MaxStreams, logger)
	return s.Run(ctx)
}

func runAgent(args []string) error {
	fs := flag.NewFlagSet("agent", flag.ContinueOnError)
	path := fs.String("config", "", "path to agent JSON config")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *path == "" {
		return errors.New("-config is required")
	}
	cfg, err := config.Load(*path)
	if err != nil {
		return err
	}
	if cfg.Role != "agent" {
		return errors.New("config role must be 'agent'")
	}
	logger := logging.New(slog.LevelInfo)
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	a := &reverse.Agent{ServerURL: cfg.ServerURL, TargetAddr: cfg.Target, Token: cfg.Token, CAFile: cfg.CAFile, ReconnectMin: cfg.ReconnectMin, ReconnectMax: cfg.ReconnectMax, MinPool: cfg.MinPool, MaxPool: cfg.MaxPool, MaxStreams: cfg.MaxStreams, Logger: logger}
	return a.Run(ctx)
}

func runTunnel(args []string) error {
	if len(args) == 0 {
		return errors.New("usage: shadows tunnel test -address host:port")
	}
	switch args[0] {
	case "test":
		fs := flag.NewFlagSet("tunnel test", flag.ContinueOnError)
		address := fs.String("address", "", "TCP address to test")
		timeout := fs.Duration("timeout", 3*time.Second, "connection timeout")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		if *address == "" {
			return errors.New("-address is required")
		}
		conn, err := net.DialTimeout("tcp", *address, *timeout)
		if err != nil {
			return fmt.Errorf("TCP probe %s failed: %w", *address, err)
		}
		_ = conn.Close()
		fmt.Printf("TCP probe OK: %s\n", *address)
		return nil
	default:
		return fmt.Errorf("unknown tunnel command %q", args[0])
	}
}

func printUsage() {
	fmt.Println(`Shadows Tunnel Phase 2

Usage:
  shadows version
  shadows doctor
  shadows server -config /path/to/server.json
  shadows agent -config /path/to/agent.json
  shadows tunnel test -address host:port [-timeout 3s]`)
}
