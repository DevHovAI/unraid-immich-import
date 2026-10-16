package telegram

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"slices"
	"sync"
	"testing"
	"time"
)

func TestPollAndSend(t *testing.T) {
	var mu sync.Mutex
	var sent, answered []string
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		r.ParseForm()
		mu.Lock()
		defer mu.Unlock()
		switch r.URL.Path {
		case "/botT/sendMessage":
			sent = append(sent, r.Form.Get("text"))
			w.Write([]byte(`{"ok":true,"result":{"message_id":42}}`))
		case "/botT/answerCallbackQuery":
			answered = append(answered, r.Form.Get("callback_query_id"))
			w.Write([]byte(`{"ok":true,"result":true}`))
		case "/botT/getUpdates":
			calls++
			var res any = []any{}
			switch calls {
			case 1: // old message from before the start
				res = []map[string]any{{"update_id": 5, "message": map[string]any{"chat": map[string]any{"id": 1}, "text": "/old"}}}
			case 2:
				if r.Form.Get("offset") != "6" {
					t.Errorf("offset = %s", r.Form.Get("offset"))
				}
				res = []map[string]any{
					{"update_id": 6, "message": map[string]any{"chat": map[string]any{"id": 99}, "text": "/foreign"}},
					{"update_id": 7, "message": map[string]any{"chat": map[string]any{"id": 1}, "text": " /status "}},
					{"update_id": 8, "callback_query": map[string]any{"id": "cb1", "data": "3:2",
						"message": map[string]any{"message_id": 42, "chat": map[string]any{"id": 1}}}},
					{"update_id": 9, "callback_query": map[string]any{"id": "cb2", "data": "3:3",
						"message": map[string]any{"message_id": 43, "chat": map[string]any{"id": 99}}}},
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
	b.Poll(ctx, func(s string) { got = append(got, s); b.Send("echo " + s) },
		func(data string, id int64) { got = append(got, fmt.Sprintf("button %s on %d", data, id)) })
	if len(got) != 2 || got[0] != "/status" || got[1] != "button 3:2 on 42" {
		t.Fatalf("received: %v", got)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(answered) != 1 || answered[0] != "cb1" {
		t.Fatalf("answered: %v", answered)
	}
	if len(sent) != 1 || sent[0] != "echo /status" {
		t.Fatalf("sent: %v", sent)
	}
}

// The bot must speak HTTP/1.1 even when the server offers HTTP/2. On the
// server, the cloned default transport still offered "h2" via ALPN.
func TestHTTP1Only(t *testing.T) {
	var proto int
	srv := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		proto = r.ProtoMajor
		w.Write([]byte(`{"ok":true,"result":{}}`))
	}))
	srv.EnableHTTP2 = true
	srv.StartTLS()
	defer srv.Close()

	b := New("T", 1, t.Logf)
	b.API = srv.URL
	tr := b.HTTP.Transport.(*http.Transport)
	if !slices.Equal(tr.TLSClientConfig.NextProtos, []string{"http/1.1"}) {
		t.Fatalf("ALPN %q", tr.TLSClientConfig.NextProtos)
	}
	tr.TLSClientConfig.RootCAs = srv.Client().Transport.(*http.Transport).TLSClientConfig.RootCAs
	if err := b.call(context.Background(), "getMe", nil, nil); err != nil {
		t.Fatal(err)
	}
	if proto != 1 {
		t.Fatalf("bot used HTTP/%d", proto)
	}
}

func TestKeyboard(t *testing.T) {
	got := keyboard([][]Button{{{Text: "Holiday", Data: "1:2"}}})
	if got != `{"inline_keyboard":[[{"text":"Holiday","callback_data":"1:2"}]]}` {
		t.Fatal(got)
	}
}
