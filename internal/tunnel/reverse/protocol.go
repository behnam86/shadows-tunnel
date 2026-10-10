// Package reverse implements the Phase 1 reverse TCP tunnel protocol.
package reverse

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"sync"
)

const maxControlLine = 16 * 1024

// Message is the bounded JSON-line control/data handshake format.
type Message struct {
	Type  string `json:"type"`
	Token string `json:"token,omitempty"`
	ID    string `json:"id,omitempty"`
}

// ReadMessage reads one newline-terminated JSON message with a strict size limit.
func ReadMessage(r *bufio.Reader) (Message, error) {
	var m Message
	var line []byte
	for {
		part, err := r.ReadSlice('\n')
		if len(line)+len(part) > maxControlLine {
			return m, errors.New("control message exceeds size limit")
		}
		line = append(line, part...)
		if err == nil {
			break
		}
		if !errors.Is(err, bufio.ErrBufferFull) {
			return m, err
		}
	}
	if err := json.Unmarshal(line, &m); err != nil {
		return m, fmt.Errorf("decode control message: %w", err)
	}
	return m, nil
}

// WriteMessage writes a JSON-line message.
func WriteMessage(w io.Writer, m Message) error {
	b, err := json.Marshal(m)
	if err != nil {
		return err
	}
	b = append(b, '\n')
	_, err = w.Write(b)
	return err
}

// CopyBidirectional relays bytes until either direction ends, then closes both streams.
func CopyBidirectional(a, b net.Conn) {
	var once sync.Once
	closeBoth := func() { once.Do(func() { _ = a.Close(); _ = b.Close() }) }
	done := make(chan struct{}, 2)
	go func() { _, _ = io.Copy(a, b); done <- struct{}{} }()
	go func() { _, _ = io.Copy(b, a); done <- struct{}{} }()
	<-done
	closeBoth()
	<-done
}
