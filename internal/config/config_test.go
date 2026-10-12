package config

import (
	"strings"
	"testing"
	"time"
)

func env(m map[string]string) func(string) string {
	return func(k string) string { return m[k] }
}

func TestDefaults(t *testing.T) {
	c, err := Load(env(map[string]string{
		"IMMICH_URL": "http://immich:2283/", "IMMICH_API_KEY": "k",
		"TG_BOT_TOKEN": "t", "TG_CHAT_ID": "42",
	}))
	if err != nil {
		t.Fatal(err)
	}
	if c.ImmichURL != "http://immich:2283" || c.TGChat != 42 || c.AlbumMode != "ask" ||
		c.AlbumAskTimeout != 2*time.Minute || c.StackTolerance != 2*time.Second ||
		c.DryRun || !c.StackPairs || c.Parallel != 2 || len(c.ProgressSteps) != 3 {
		t.Fatalf("unerwartet: %+v", c)
	}
}

func TestErrors(t *testing.T) {
	_, err := Load(env(map[string]string{
		"IMMICH_URL": "immich", "TG_CHAT_ID": "x", "ALBUM_MODE": "wild",
		"DRY_RUN": "vielleicht", "PROGRESS_STEPS": "50 150",
	}))
	if err == nil {
		t.Fatal("Fehler erwartet")
	}
	for _, want := range []string{"IMMICH_URL", "IMMICH_API_KEY", "TG_CHAT_ID", "ALBUM_MODE", "DRY_RUN", "150"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("Meldung zu %s fehlt in:\n%v", want, err)
		}
	}
}

func TestAskWithoutTelegramFallsBackToDate(t *testing.T) {
	c, err := Load(env(map[string]string{"IMMICH_URL": "http://i:1", "IMMICH_API_KEY": "k"}))
	if err != nil {
		t.Fatal(err)
	}
	if c.AlbumMode != "date" {
		t.Fatalf("AlbumMode = %q", c.AlbumMode)
	}
}
