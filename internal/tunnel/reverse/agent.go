package reverse

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"net/url"
	"sync"
	"sync/atomic"
	"time"

	"github.com/behnam86/shadows-tunnel/internal/transport/tcpmux"
	"github.com/behnam86/shadows-tunnel/internal/transport/wss"
)

// Agent maintains a bounded pool of outbound WSS multiplex sessions from the source node.
type Agent struct {
	ServerURL, TargetAddr, Token, CAFile string
	ReconnectMin, ReconnectMax           time.Duration
	MinPool, MaxPool, MaxStreams         int
	Logger                               *slog.Logger
	workers                              atomic.Int32
	activeStreams                        atomic.Int64
}

// Run maintains the configured number of outbound sessions until cancellation.
func (a *Agent) Run(ctx context.Context) error {
	if a.Logger == nil {
		a.Logger = slog.Default()
	}
	if a.MinPool <= 0 {
		a.MinPool = 1
	}
	if a.MaxPool < a.MinPool {
		a.MaxPool = a.MinPool
	}
	if a.MaxPool > 64 {
		return fmt.Errorf("max_pool must not exceed 64")
	}
	if a.MaxStreams <= 0 {
		a.MaxStreams = 256
	}
	var wg sync.WaitGroup
	launch := func() {
		a.workers.Add(1)
		wg.Add(1)
		go func(worker int) { defer wg.Done(); defer a.workers.Add(-1); a.worker(ctx, worker) }(int(a.workers.Load()))
	}
	for i := 0; i < a.MinPool; i++ {
		launch()
	}
	// Scale up when most configured stream capacity is in use. Workers are retained
	// after scale-up so transient traffic does not cause connection churn.
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			wg.Wait()
			return nil
		case <-ticker.C:
			workers := int(a.workers.Load())
			capacity := int64(workers * a.MaxStreams)
			if workers < a.MaxPool && capacity > 0 && a.activeStreams.Load()*4 >= capacity*3 {
				launch()
			}
		}
	}
}

func (a *Agent) worker(ctx context.Context, worker int) {
	min := a.ReconnectMin
	if min <= 0 {
		min = time.Second
	}
	max := a.ReconnectMax
	if max < min {
		max = 30 * time.Second
	}
	backoff := min
	for ctx.Err() == nil {
		u, err := url.Parse(a.ServerURL)
		if err == nil {
			var conn net.Conn
			conn, err = wss.DialWithCA(u, a.Token, 10*time.Second, a.CAFile)
			if err == nil {
				backoff = min
				a.Logger.Info("WSS pool connection established", "worker", worker, "server", a.ServerURL)
				session := tcpmux.New(conn, a.MaxStreams)
				a.serveSession(ctx, session, worker)
				session.Close()
				err = fmt.Errorf("WSS session ended")
			}
		}
		if ctx.Err() != nil {
			return
		}
		a.Logger.Warn("WSS connection unavailable", "worker", worker, "error", err, "retry_in", backoff.String())
		t := time.NewTimer(backoff)
		select {
		case <-ctx.Done():
			t.Stop()
			return
		case <-t.C:
		}
		backoff *= 2
		if backoff > max {
			backoff = max
		}
	}
}
func (a *Agent) serveSession(ctx context.Context, s *tcpmux.Session, worker int) {
	stopClose := context.AfterFunc(ctx, func() { _ = s.Close() })
	defer stopClose()
	var wg sync.WaitGroup
	defer wg.Wait()
	for {
		stream, err := s.AcceptStream(ctx)
		if err != nil {
			return
		}
		wg.Add(1)
		a.activeStreams.Add(1)
		go func(st net.Conn) {
			defer wg.Done()
			defer a.activeStreams.Add(-1)
			defer st.Close()
			target, err := net.DialTimeout("tcp", a.TargetAddr, 8*time.Second)
			if err != nil {
				a.Logger.Warn("target dial failed", "target", a.TargetAddr, "error", err)
				return
			}
			defer target.Close()
			CopyBidirectional(st, target)
		}(stream)
		_ = worker
	}
}
