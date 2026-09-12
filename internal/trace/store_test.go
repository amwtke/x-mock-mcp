package trace

import (
	"encoding/json"
	"testing"
)

func TestTraceCursorAndBound(t *testing.T) {
	s, err := Open(t.TempDir(), 300)
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		if _, err = s.Append("run", Event{Kind: "query", Payload: json.RawMessage(`{"id":1}`)}); err != nil {
			t.Fatal(err)
		}
	}
	events, err := s.Read("run", 1, 8)
	if err != nil || len(events) != 1 || events[0].Seq != 2 {
		t.Fatal(events, err)
	}
	if _, err = s.Append("run", Event{Kind: string(make([]byte, 300))}); err == nil {
		t.Fatal("trace limit ignored")
	}
}
