package reverse

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"sync"
	"sync/atomic"
	"time"

	"github.com/behnam86/shadows-tunnel/internal/transport/tcpmux"
	"github.com/behnam86/shadows-tunnel/internal/transport/wss"
)

// Server accepts public TCP connections and forwards them through authenticated WSS multiplex sessions.
type Server struct {
	WSSListen, PublicListen, Token, TLSCert, TLSKey string
	MaxConnections, MaxStreams                      int
	Logger                                          *slog.Logger
	mu                                              sync.RWMutex
	sessions                                        map[*tcpmux.Session]struct{}
	rr                                              atomic.Uint64
	active                                          atomic.Int64
}

// NewServer creates a WSS reverse-tunnel server.
func NewServer(wssListen, publicListen, token, cert, key string, maxConnections, maxStreams int, logger *slog.Logger) *Server {
	if maxConnections <= 0 {
		maxConnections = 1000
	}
	if maxStreams <= 0 {
		maxStreams = 256
	}
	if logger == nil {
		logger = slog.Default()
	}
	return &Server{WSSListen: wssListen, PublicListen: publicListen, Token: token, TLSCert: cert, TLSKey: key, MaxConnections: maxConnections, MaxStreams: maxStreams, Logger: logger, sessions: make(map[*tcpmux.Session]struct{})}
}

// Run serves WSS agent sessions and public TCP clients until cancellation.
func (s *Server) Run(ctx context.Context) error {
	if s.TLSCert == "" || s.TLSKey == "" {
		return errors.New("tls_cert and tls_key are required for WSS server")
	}
	publicLn, err := net.Listen("tcp", s.PublicListen)
	if err != nil {
		return fmt.Errorf("listen public TCP: %w", err)
	}
	defer publicLn.Close()
	mux := http.NewServeMux()
	mux.Handle("/", wss.Handler(s.Token, func(conn net.Conn) {
		session := tcpmux.New(conn, s.MaxStreams)
		s.mu.Lock()
		s.sessions[session] = struct{}{}
		s.mu.Unlock()
		s.Logger.Info("WSS agent session connected", "remote", conn.RemoteAddr().String())
		defer func() {
			session.Close()
			s.mu.Lock()
			delete(s.sessions, session)
			s.mu.Unlock()
			s.Logger.Warn("WSS agent session disconnected", "remote", conn.RemoteAddr().String())
		}()
		select {
		case <-ctx.Done():
			return
		case <-session.Done():
			return
		}
	}))
	// Session.AcceptStream is for streams opened by the peer; server is the opener. Keep session alive by waiting on ctx or transport closure.
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/tunnel" {
			http.NotFound(w, r)
			return
		}
		mux.ServeHTTP(w, r)
	})
	srv := &http.Server{Addr: s.WSSListen, Handler: handler, TLSConfig: &tls.Config{MinVersion: tls.VersionTLS12}, ReadHeaderTimeout: 10 * time.Second, IdleTimeout: 90 * time.Second, MaxHeaderBytes: 16 << 10}
	errCh := make(chan error, 1)
	go func() { errCh <- srv.ListenAndServeTLS(s.TLSCert, s.TLSKey) }()
	go func() { <-ctx.Done(); _ = srv.Shutdown(context.Background()); _ = publicLn.Close() }()
	s.Logger.Info("Shadows Tunnel destination server started", "wss", s.WSSListen, "public", s.PublicListen)
	go func() {
		for {
			conn, err := publicLn.Accept()
			if err != nil {
				if ctx.Err() != nil {
					return
				}
				s.Logger.Error("accept public connection", "error", err)
				continue
			}
			if s.active.Add(1) > int64(s.MaxConnections) {
				s.active.Add(-1)
				_ = conn.Close()
				continue
			}
			go s.forward(conn)
		}
	}()
	select {
	case <-ctx.Done():
		return nil
	case err := <-errCh:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return fmt.Errorf("WSS server: %w", err)
	}
}
func (s *Server) forward(client net.Conn) {
	defer s.active.Add(-1)
	defer client.Close()
	_ = client.SetDeadline(time.Now().Add(20 * time.Second))
	var stream net.Conn
	var err error
	for i := 0; i < 3; i++ {
		session := s.pickSession()
		if session == nil {
			err = errors.New("no connected agent sessions")
			time.Sleep(100 * time.Millisecond)
			continue
		}
		stream, err = session.OpenStream()
		if err == nil {
			break
		}
	}
	if err != nil {
		s.Logger.Warn("cannot open tunnel stream", "error", err)
		return
	}
	_ = client.SetDeadline(time.Time{})
	CopyBidirectional(client, stream)
}
func (s *Server) pickSession() *tcpmux.Session {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var best *tcpmux.Session
	bestLoad := int(^uint(0) >> 1)
	// Prefer the least-loaded session with available stream capacity.
	for session := range s.sessions {
		load := session.ActiveStreams()
		if load < session.MaxStreams() && load < bestLoad {
			best, bestLoad = session, load
		}
	}
	return best
}
