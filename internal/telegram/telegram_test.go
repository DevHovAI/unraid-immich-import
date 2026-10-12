package telegram

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"
)

func TestPollAndSend(t *testing.T) {
	var mu sync.Mutex
	var sent []string
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		r.ParseForm()
		mu.Lock()
		defer mu.Unlock()
		switch r.URL.Path {
		case "/botT/sendMessage":
			sent = append(sent, r.Form.Get("text"))
			w.Write([]byte(`{"ok":true,"result":{}}`))
		case "/botT/getUpdates":
			calls++
			var res any = []any{}
			switch calls {
			case 1: // alte Nachricht vor dem Start
				res = []map[string]any{{"update_id": 5, "message": map[string]any{"chat": map[string]any{"id": 1}, "text": "/alt"}}}
			case 2:
				if r.Form.Get("offset") != "6" {
					t.Errorf("offset = %s", r.Form.Get("offset"))
				}
				res = []map[string]any{
					{"update_id": 6, "message": map[string]any{"chat": map[string]any{"id": 99}, "text": "/fremd"}},
					{"update_id": 7, "message": map[string]any{"chat": map[string]any{"id": 1}, "text": " /status "}},
				}
			default:
				time.Sleep(20 * time.Millisecond)
			}
			json.NewEncoder(w).Encode(map[string]any{"ok": true, "result": res})
		}
	}))
	defer srv.Close()

	b := New("T", 1, t.Logf)
	b.API = srv.URL
	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()
	var got []string
	b.Poll(ctx, func(s string) { got = append(got, s); b.Send("echo " + s) })
	if len(got) != 1 || got[0] != "/status" {
		t.Fatalf("empfangen: %v", got)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(sent) != 1 || sent[0] != "echo /status" {
		t.Fatalf("gesendet: %v", sent)
	}
}
