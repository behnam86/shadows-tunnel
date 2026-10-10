package tcp

import (
	"context"
	"io"
	"testing"
)

func TestListenAndDial(t *testing.T) {
	tr := Transport{}
	ln, err := tr.Listen(context.Background(), "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	done := make(chan error, 1)
	go func() {
		c, err := ln.Accept()
		if err != nil {
			done <- err
			return
		}
		defer c.Close()
		b := make([]byte, 4)
		_, err = io.ReadFull(c, b)
		if err == nil && string(b) != "test" {
			err = io.ErrUnexpectedEOF
		}
		done <- err
	}()
	c, err := tr.Dial(context.Background(), ln.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c.Write([]byte("test")); err != nil {
		t.Fatal(err)
	}
	_ = c.Close()
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}
