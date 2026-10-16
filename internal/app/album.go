package app

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/DevHovAI/unraid-immich-import/internal/telegram"
)

// Album buttons: the most recently changed albums, plus the date album and
// "no album". Button data is "<seq>:<index>", so a press on an old message
// cannot pick from a newer list.
const pickerAlbums = 8

type picker struct {
	seq   int
	msgID int64
	names []string // index → album name; "" = no album
}

const noAlbum = "🚫 No album"

func dateAlbum() string { return "Import " + time.Now().Format("2006-01-02") }

// sendPicker sends text with album buttons. Without Immich albums, or if
// they cannot be read, only the date album and "no album" are offered.
func (a *App) sendPicker(ctx context.Context, text string) {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	albums, err := a.Immich.Albums(ctx)
	if err != nil {
		a.Log.Printf("read albums: %v", err)
	}
	def := dateAlbum()
	names := []string{def, ""}
	for _, n := range albums {
		if len(names) == pickerAlbums+2 {
			break
		}
		if n != def && n != "" {
			names = append(names, n)
		}
	}

	a.mu.Lock()
	a.picker.seq++
	seq := a.picker.seq
	a.mu.Unlock()

	var rows [][]telegram.Button
	var row []telegram.Button
	for i, n := range names[2:] {
		row = append(row, telegram.Button{Text: short(n), Data: fmt.Sprintf("%d:%d", seq, i+2)})
		if len(row) == 2 {
			rows, row = append(rows, row), nil
		}
	}
	if row != nil {
		rows = append(rows, row)
	}
	rows = append(rows, []telegram.Button{
		{Text: "📅 " + def, Data: fmt.Sprintf("%d:0", seq)},
		{Text: noAlbum, Data: fmt.Sprintf("%d:1", seq)},
	})
	a.Log.Printf("→ %s", strings.SplitN(text, "\n", 2)[0])
	id := a.Bot.SendButtons(text, rows)

	a.mu.Lock()
	if a.picker.seq == seq {
		a.picker.msgID, a.picker.names = id, names
	}
	a.mu.Unlock()
}

// short keeps buttons readable; the full name is used anyway
func short(s string) string {
	if r := []rune(s); len(r) > 28 {
		return string(r[:27]) + "…"
	}
	return s
}

// onButton handles a press on an album button.
func (a *App) onButton(data string, msgID int64) {
	seqS, idxS, _ := strings.Cut(data, ":")
	seq, err1 := strconv.Atoi(seqS)
	idx, err2 := strconv.Atoi(idxS)
	a.mu.Lock()
	p := a.picker
	a.mu.Unlock()
	if err1 != nil || err2 != nil || seq != p.seq || idx < 0 || idx >= len(p.names) {
		a.Bot.Edit(msgID, "This selection is outdated. /switch_album shows the albums again.")
		return
	}
	a.Bot.Edit(msgID, a.pickAlbum(p.names[idx]))
}

// pickAlbum answers an open album question, otherwise switches the album of
// the running import. Returns the reply.
func (a *App) pickAlbum(name string) string {
	label := "📁 Album: " + name
	if name == "" {
		label = "📁 No album."
	}
	if a.answerAlbum(name) {
		return label
	}
	p := a.Imp.Current()
	if p == nil {
		return "💤 No import running – nothing to switch."
	}
	if !p.SetAlbum(name) {
		return "⏳ Too late: the files are already being added to “" + p.Album() + "”."
	}
	a.Log.Printf("album switched to %q", name)
	if name == "" {
		return "📁 Switched: no album."
	}
	return "📁 Switched to album: " + name
}

// chooseAlbum picks the album name according to ALBUM_MODE; asks via
// Telegram for "ask".
func (a *App) chooseAlbum(ctx context.Context, card string, n int) string {
	def := dateAlbum()
	switch a.Cfg.AlbumMode {
	case "fixed":
		return a.Cfg.Album
	case "date":
		return def
	case "ask":
	default:
		return ""
	}
	ch := make(chan string, 1)
	a.mu.Lock()
	a.answer = ch
	a.mu.Unlock()
	defer func() {
		a.mu.Lock()
		a.answer = nil
		a.mu.Unlock()
	}()
	a.sendPicker(ctx, fmt.Sprintf("📷 Card detected: %d new files on %s%s\nWhich album? Tap one or reply with a new name.\nNo reply within %d min: “%s”. /switch_album changes it during the import.",
		n, card, a.mode(), int(a.Cfg.AlbumAskTimeout.Minutes()), def))
	select {
	case ans := <-ch:
		return ans
	case <-time.After(a.Cfg.AlbumAskTimeout):
		a.closePicker("📁 No reply – album: " + def)
		return def
	case <-ctx.Done():
		return ""
	}
}

// closePicker removes the buttons of the last album message.
func (a *App) closePicker(text string) {
	a.mu.Lock()
	id := a.picker.msgID
	a.picker.seq++ // old buttons are outdated now
	a.mu.Unlock()
	a.Bot.Edit(id, text)
}

// answerAlbum passes the answer on to a pending album question.
func (a *App) answerAlbum(name string) bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.answer == nil {
		return false
	}
	select {
	case a.answer <- name:
	default:
	}
	a.answer = nil
	return true
}

// switchAlbum handles /switch_album [name].
func (a *App) switchAlbum(arg string) {
	p := a.Imp.Current()
	if p == nil {
		a.Bot.Send("💤 No import running. /switch_album works while a card is being imported.")
		return
	}
	if arg != "" {
		a.Bot.Send(a.pickAlbum(arg))
		return
	}
	cur := p.Album()
	if cur == "" {
		cur = "none"
	}
	a.sendPicker(a.ctx, "📁 Album of the running import: "+cur+"\nWhich one instead? Tap one or send /switch_album <new name>.")
}
