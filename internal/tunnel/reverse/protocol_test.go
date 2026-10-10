package reverse

import (
	"bufio"
	"strings"
	"testing"
)

func TestMessageRoundTrip(t *testing.T) {
	var b strings.Builder
	want := Message{Type: "open", ID: "abc"}
	if err := WriteMessage(&b, want); err != nil {
		t.Fatal(err)
	}
	got, err := ReadMessage(bufio.NewReader(strings.NewReader(b.String())))
	if err != nil {
		t.Fatal(err)
	}
	if got.Type != want.Type || got.ID != want.ID {
		t.Fatalf("got %+v, want %+v", got, want)
	}
}

func TestMessageRejectsInvalidJSON(t *testing.T) {
	if _, err := ReadMessage(bufio.NewReader(strings.NewReader("not-json\n"))); err == nil {
		t.Fatal("expected parse error")
	}
}
