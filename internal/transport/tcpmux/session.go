// Package tcpmux provides bounded logical TCP streams over one net.Conn.
package tcpmux

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net"
	"sync"
	"sync/atomic"
	"time"
)

const (
	frameOpen  byte = 1
	frameData  byte = 2
	frameClose byte = 3
	headerSize      = 9
	maxPayload      = 32 << 10
)

var errSessionClosed = errors.New("multiplex session closed")

// Session multiplexes logical streams over a single ordered transport connection.
type Session struct {
	conn     net.Conn
	max      int
	writeMu  sync.Mutex
	mu       sync.Mutex
	streams  map[uint32]*Stream
	next     atomic.Uint32
	incoming chan *Stream
	done     chan struct{}
	once     sync.Once
	err      error
}

// New creates a session and starts its bounded frame reader.
func New(conn net.Conn, maxStreams int) *Session {
	if maxStreams <= 0 {
		maxStreams = 256
	}
	s := &Session{conn: conn, max: maxStreams, streams: make(map[uint32]*Stream), incoming: make(chan *Stream, maxStreams), done: make(chan struct{})}
	go s.readLoop()
	return s
}

// OpenStream opens a logical stream on the remote peer.
func (s *Session) OpenStream() (net.Conn, error) {
	s.mu.Lock()
	if s.closed() {
		s.mu.Unlock()
		return nil, errSessionClosed
	}
	if len(s.streams) >= s.max {
		s.mu.Unlock()
		return nil, errors.New("multiplex stream limit reached")
	}
	var id uint32
	for {
		id = s.next.Add(1)
		if id == 0 {
			continue
		}
		if _, ok := s.streams[id]; !ok {
			break
		}
	}
	st := newStream(s, id)
	s.streams[id] = st
	s.mu.Unlock()
	if err := s.writeFrame(frameOpen, id, nil); err != nil {
		s.remove(id)
		return nil, err
	}
	return st, nil
}

// ActiveStreams reports the number of currently registered logical streams.
func (s *Session) ActiveStreams() int { s.mu.Lock(); defer s.mu.Unlock(); return len(s.streams) }

// MaxStreams reports the configured logical-stream limit for this session.
func (s *Session) MaxStreams() int { return s.max }

// Done is closed when the underlying transport or session has terminated.
func (s *Session) Done() <-chan struct{} { return s.done }

// AcceptStream waits for the next stream opened by the remote peer.
func (s *Session) AcceptStream(ctx context.Context) (net.Conn, error) {
	select {
	case <-s.done:
		return nil, errSessionClosed
	default:
	}
	select {
	case st := <-s.incoming:
		if st == nil {
			return nil, errSessionClosed
		}
		return st, nil
	case <-s.done:
		return nil, errSessionClosed
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

// Close terminates the session and all streams.
func (s *Session) Close() error {
	s.once.Do(func() {
		close(s.done)
		_ = s.conn.Close()
		s.mu.Lock()
		for id, st := range s.streams {
			st.terminate()
			delete(s.streams, id)
		}
		s.mu.Unlock()
	})
	return nil
}
func (s *Session) closed() bool {
	select {
	case <-s.done:
		return true
	default:
		return false
	}
}
func (s *Session) remove(id uint32) { s.mu.Lock(); delete(s.streams, id); s.mu.Unlock() }
func (s *Session) writeFrame(typ byte, id uint32, p []byte) error {
	if len(p) > maxPayload {
		return errors.New("multiplex payload too large")
	}
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	if s.closed() {
		return errSessionClosed
	}
	var h [headerSize]byte
	h[0] = typ
	binary.BigEndian.PutUint32(h[1:5], id)
	binary.BigEndian.PutUint32(h[5:9], uint32(len(p)))
	if err := writeFull(s.conn, h[:]); err != nil {
		s.Close()
		return err
	}
	if len(p) > 0 {
		if err := writeFull(s.conn, p); err != nil {
			s.Close()
			return err
		}
	}
	return nil
}
func writeFull(w io.Writer, p []byte) error {
	for len(p) > 0 {
		n, e := w.Write(p)
		if e != nil {
			return e
		}
		if n == 0 {
			return io.ErrShortWrite
		}
		p = p[n:]
	}
	return nil
}
func (s *Session) readLoop() {
	defer s.Close()
	for {
		var h [headerSize]byte
		if _, err := io.ReadFull(s.conn, h[:]); err != nil {
			s.setErr(err)
			return
		}
		typ := h[0]
		id := binary.BigEndian.Uint32(h[1:5])
		n := binary.BigEndian.Uint32(h[5:9])
		if n > maxPayload {
			s.setErr(fmt.Errorf("frame payload %d exceeds limit", n))
			return
		}
		if (typ == frameOpen || typ == frameClose) && n != 0 {
			s.setErr(errors.New("OPEN/CLOSE frames must not contain payloads"))
			return
		}
		if typ != frameOpen && typ != frameData && typ != frameClose {
			s.setErr(fmt.Errorf("unknown frame type %d", typ))
			return
		}
		p := make([]byte, int(n))
		if _, err := io.ReadFull(s.conn, p); err != nil {
			s.setErr(err)
			return
		}
		s.mu.Lock()
		st, ok := s.streams[id]
		if typ == frameOpen {
			if ok || id == 0 || len(s.streams) >= s.max {
				s.mu.Unlock()
				s.setErr(errors.New("invalid or excessive stream open"))
				return
			}
			st = newStream(s, id)
			s.streams[id] = st
			select {
			case s.incoming <- st:
				s.mu.Unlock()
				continue
			default:
				s.mu.Unlock()
				s.setErr(errors.New("incoming stream queue full"))
				return
			}
		}
		s.mu.Unlock()
		if !ok {
			continue
		}
		switch typ {
		case frameData:
			if err := st.deliver(p); err != nil {
				s.setErr(err)
				return
			}
		case frameClose:
			st.terminate()
			s.remove(id)
		default:
			s.setErr(fmt.Errorf("unknown frame type %d", typ))
			return
		}
	}
}
func (s *Session) setErr(err error) {
	s.mu.Lock()
	if s.err == nil {
		s.err = err
	}
	s.mu.Unlock()
}

// Stream is one logical TCP connection inside a multiplex session.
type Stream struct {
	session       *Session
	id            uint32
	incoming      chan []byte
	mu            sync.Mutex
	current       []byte
	remoteClosed  chan struct{}
	closeOnce     sync.Once
	remoteOnce    sync.Once
	localClosed   atomic.Bool
	deadlineMu    sync.Mutex
	readDeadline  time.Time
	writeDeadline time.Time
}

func newStream(s *Session, id uint32) *Stream {
	return &Stream{session: s, id: id, incoming: make(chan []byte, 16), remoteClosed: make(chan struct{})}
}
func (st *Stream) Read(p []byte) (int, error) {
	if len(p) == 0 {
		return 0, nil
	}
	st.mu.Lock()
	if len(st.current) > 0 {
		n := copy(p, st.current)
		st.current = st.current[n:]
		st.mu.Unlock()
		return n, nil
	}
	st.mu.Unlock()
	st.deadlineMu.Lock()
	d := st.readDeadline
	st.deadlineMu.Unlock()
	var timer <-chan time.Time
	if !d.IsZero() {
		dur := time.Until(d)
		if dur <= 0 {
			return 0, timeoutError{}
		}
		t := time.NewTimer(dur)
		defer t.Stop()
		timer = t.C
	}
	// Drain already-buffered data before observing a close, preserving stream ordering.
	select {
	case b := <-st.incoming:
		return st.copyChunk(p, b), nil
	default:
	}
	select {
	case b := <-st.incoming:
		return st.copyChunk(p, b), nil
	case <-st.remoteClosed:
		return 0, io.EOF
	case <-st.session.done:
		return 0, io.EOF
	case <-timer:
		return 0, timeoutError{}
	}
}
func (st *Stream) copyChunk(p, b []byte) int {
	n := copy(p, b)
	if n < len(b) {
		st.mu.Lock()
		st.current = append(st.current[:0], b[n:]...)
		st.mu.Unlock()
	}
	return n
}

func (st *Stream) Write(p []byte) (int, error) {
	if st.localClosed.Load() {
		return 0, net.ErrClosed
	}
	total := 0
	for len(p) > 0 {
		n := len(p)
		if n > maxPayload {
			n = maxPayload
		}
		if err := st.session.writeFrame(frameData, st.id, p[:n]); err != nil {
			return total, err
		}
		total += n
		p = p[n:]
	}
	return total, nil
}
func (st *Stream) Close() error {
	st.closeOnce.Do(func() {
		st.localClosed.Store(true)
		_ = st.session.writeFrame(frameClose, st.id, nil)
		st.terminate()
		st.session.remove(st.id)
	})
	return nil
}
func (st *Stream) terminate() {
	st.localClosed.Store(true)
	st.remoteOnce.Do(func() { close(st.remoteClosed) })
}
func (st *Stream) deliver(p []byte) error {
	if st.localClosed.Load() {
		return nil
	}
	cp := append([]byte(nil), p...)
	select {
	case st.incoming <- cp:
		return nil
	case <-st.remoteClosed:
		return nil
	case <-st.session.done:
		return errSessionClosed
	}
}
func (st *Stream) LocalAddr() net.Addr  { return st.session.conn.LocalAddr() }
func (st *Stream) RemoteAddr() net.Addr { return st.session.conn.RemoteAddr() }
func (st *Stream) SetDeadline(t time.Time) error {
	st.deadlineMu.Lock()
	st.readDeadline = t
	st.writeDeadline = t
	st.deadlineMu.Unlock()
	return nil
}
func (st *Stream) SetReadDeadline(t time.Time) error {
	st.deadlineMu.Lock()
	st.readDeadline = t
	st.deadlineMu.Unlock()
	return nil
}
func (st *Stream) SetWriteDeadline(t time.Time) error {
	st.deadlineMu.Lock()
	st.writeDeadline = t
	st.deadlineMu.Unlock()
	return nil
}

type timeoutError struct{}

func (timeoutError) Error() string   { return "i/o timeout" }
func (timeoutError) Timeout() bool   { return true }
func (timeoutError) Temporary() bool { return true }
