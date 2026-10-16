// Package telegram is a minimal bot client: send, and receive via long
// polling. Long polling keeps one connection open and costs practically no
// CPU.
package telegram

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

type Bot struct {
	Token string
	Chat  int64
	API   string // https://api.telegram.org, a mock in tests
	HTTP  *http.Client
	Log   func(format string, args ...any)
}

// pollSeconds is the long-polling timeout. Short enough that a connection
// that silently died (NAT, router) is noticed after pollSeconds+15 s.
const pollSeconds = 25

func New(token string, chat int64, logf func(string, ...any)) *Bot {
	// HTTP/1.1 only: with HTTP/2 every request shares one connection, and when
	// that connection dies silently, each following request hangs until the
	// timeout too (seen on Unraid: answers after 4 minutes). HTTP/1.1 drops a
	// timed-out connection, so the next request dials a fresh one.
	tr := http.DefaultTransport.(*http.Transport).Clone()
	tr.Protocols = new(http.Protocols)
	tr.Protocols.SetHTTP1(true)
	// The clone may carry "h2" in ALPN once the default transport was used
	tr.TLSClientConfig = &tls.Config{NextProtos: []string{"http/1.1"}}
	tr.IdleConnTimeout = 60 * time.Second
	return &Bot{Token: token, Chat: chat, API: "https://api.telegram.org",
		HTTP: &http.Client{Timeout: (pollSeconds + 15) * time.Second, Transport: tr}, Log: logf}
}

func (b *Bot) Enabled() bool { return b != nil && b.Token != "" && b.Chat != 0 }

func (b *Bot) call(ctx context.Context, method string, form url.Values, out any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		b.API+"/bot"+b.Token+"/"+method, strings.NewReader(form.Encode()))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := b.HTTP.Do(req)
	if err != nil {
		// The URL contains the token: keep it out of the log
		if ue, ok := err.(*url.Error); ok {
			err = ue.Err
		}
		return err
	}
	defer resp.Body.Close()
	var r struct {
		OK          bool            `json:"ok"`
		Description string          `json:"description"`
		Result      json.RawMessage `json:"result"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&r); err != nil {
		return fmt.Errorf("%s: HTTP %d", method, resp.StatusCode)
	}
	if !r.OK {
		return fmt.Errorf("%s: %s", method, r.Description)
	}
	if out != nil {
		return json.Unmarshal(r.Result, out)
	}
	return nil
}

// Send sends a message; errors only go to the log.
func (b *Bot) Send(text string) { b.SendButtons(text, nil) }

// Button is an inline button below a message. Data comes back to the
// button handler of Poll (at most 64 bytes).
type Button struct{ Text, Data string }

// SendButtons sends a message with rows of buttons and returns its ID
// (0 on error).
func (b *Bot) SendButtons(text string, rows [][]Button) int64 {
	if !b.Enabled() {
		return 0
	}
	if r := []rune(text); len(r) > 4000 {
		text = string(r[:4000]) + "…"
	}
	form := url.Values{"chat_id": {strconv.FormatInt(b.Chat, 10)}, "text": {text}}
	if len(rows) > 0 {
		form.Set("reply_markup", keyboard(rows))
	}
	var msg struct {
		MessageID int64 `json:"message_id"`
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	if err := b.call(ctx, "sendMessage", form, &msg); err != nil {
		if b.Log != nil {
			b.Log("Telegram message failed: %v", err)
		}
		return 0
	}
	return msg.MessageID
}

// Edit replaces the text of a message and removes its buttons.
func (b *Bot) Edit(id int64, text string) {
	if !b.Enabled() || id == 0 {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	err := b.call(ctx, "editMessageText", url.Values{
		"chat_id": {strconv.FormatInt(b.Chat, 10)}, "message_id": {strconv.FormatInt(id, 10)},
		"text": {text},
	}, nil)
	if err != nil && b.Log != nil {
		b.Log("Telegram edit failed: %v", err)
	}
}

func keyboard(rows [][]Button) string {
	type btn struct {
		Text string `json:"text"`
		Data string `json:"callback_data"`
	}
	var kb [][]btn
	for _, r := range rows {
		var row []btn
		for _, x := range r {
			row = append(row, btn{x.Text, x.Data})
		}
		kb = append(kb, row)
	}
	j, _ := json.Marshal(map[string]any{"inline_keyboard": kb})
	return string(j)
}

type Command struct{ Name, Description string }

func (b *Bot) SetCommands(ctx context.Context, cmds []Command) error {
	type c struct {
		Command     string `json:"command"`
		Description string `json:"description"`
	}
	var list []c
	for _, x := range cmds {
		list = append(list, c{x.Name, x.Description})
	}
	j, _ := json.Marshal(list)
	return b.call(ctx, "setMyCommands", url.Values{"commands": {string(j)}}, nil)
}

type message struct {
	MessageID int64 `json:"message_id"`
	Chat      struct {
		ID int64 `json:"id"`
	} `json:"chat"`
	Text string `json:"text"`
}

// Poll calls handle for every text message and onButton for every button
// press from our own chat until ctx ends. Messages from before the start are
// skipped.
func (b *Bot) Poll(ctx context.Context, handle func(text string), onButton func(data string, msgID int64)) {
	type update struct {
		UpdateID int64    `json:"update_id"`
		Message  *message `json:"message"`
		Callback *struct {
			ID      string   `json:"id"`
			Data    string   `json:"data"`
			Message *message `json:"message"`
		} `json:"callback_query"`
	}
	offset := int64(-1) // -1: fetch only the last old message and drop it
	skipOld := true
	backoff := time.Second
	for ctx.Err() == nil {
		var ups []update
		err := b.call(ctx, "getUpdates", url.Values{
			"offset": {strconv.FormatInt(offset, 10)}, "timeout": {strconv.Itoa(pollSeconds)},
			"allowed_updates": {`["message","callback_query"]`},
		}, &ups)
		if err != nil {
			if ctx.Err() != nil {
				return
			}
			if b.Log != nil {
				b.Log("Telegram poll: %v", err)
			}
			b.HTTP.CloseIdleConnections()
			select {
			case <-ctx.Done():
				return
			case <-time.After(backoff):
			}
			backoff = min(backoff*2, time.Minute)
			continue
		}
		backoff = time.Second
		for _, u := range ups {
			offset = u.UpdateID + 1
			if skipOld {
				continue
			}
			if c := u.Callback; c != nil && c.Message != nil && c.Message.Chat.ID == b.Chat {
				// stop the spinner on the button
				b.call(ctx, "answerCallbackQuery", url.Values{"callback_query_id": {c.ID}}, nil)
				if onButton != nil {
					onButton(c.Data, c.Message.MessageID)
				}
				continue
			}
			if u.Message == nil || u.Message.Chat.ID != b.Chat {
				continue
			}
			if t := strings.TrimSpace(u.Message.Text); t != "" {
				handle(t)
			}
		}
		if skipOld {
			skipOld = false
			if len(ups) == 0 {
				offset = 0
			}
		}
	}
}
