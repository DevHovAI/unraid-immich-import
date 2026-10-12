// Package config liest die Einstellungen aus Umgebungsvariablen. Die Namen
// sind dieselben wie in der Skript-Version (camera-import-install).
package config

import (
	"errors"
	"fmt"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"
)

type Config struct {
	ImmichURL string
	APIKey    string

	TGToken string
	TGChat  int64 // 0 = Telegram aus

	AlbumMode       string // ask, date, fixed, none
	Album           string
	AlbumAskTimeout time.Duration

	DryRun        bool
	ProgressSteps []int // Prozentmarken für Zwischenstände

	StackPairs     bool
	StackCover     string // display oder raw
	StackTolerance time.Duration

	DisksDir     string // hier hängt Unassigned Devices die Karten ein
	ConfigDir    string // Gedächtnis, Log, letzter Import
	PollInterval time.Duration
	Parallel     int // gleichzeitige Uploads
}

// Load liest die Einstellungen über getenv (os.Getenv, in Tests eine Map).
func Load(getenv func(string) string) (Config, error) {
	get := func(k, def string) string {
		if v := strings.TrimSpace(getenv(k)); v != "" {
			return v
		}
		return def
	}
	var errs []error
	num := func(k string, def, min int) int {
		s := get(k, strconv.Itoa(def))
		n, err := strconv.Atoi(s)
		if err != nil || n < min {
			errs = append(errs, fmt.Errorf("%s=%q ist keine Zahl ≥ %d", k, s, min))
			return def
		}
		return n
	}
	flag := func(k, def string) bool {
		switch s := get(k, def); s {
		case "1", "true", "yes", "ja":
			return true
		case "0", "false", "no", "nein":
			return false
		default:
			errs = append(errs, fmt.Errorf("%s=%q: 0 oder 1 erwartet", k, s))
			return def == "1"
		}
	}

	c := Config{
		ImmichURL:       strings.TrimRight(get("IMMICH_URL", ""), "/"),
		APIKey:          get("IMMICH_API_KEY", ""),
		TGToken:         get("TG_BOT_TOKEN", ""),
		AlbumMode:       get("ALBUM_MODE", "ask"),
		Album:           get("IMMICH_ALBUM", ""),
		AlbumAskTimeout: time.Duration(num("ALBUM_ASK_TIMEOUT", 120, 10)) * time.Second,
		DryRun:          flag("DRY_RUN", "0"),
		StackPairs:      flag("STACK_PAIRS", "1"),
		StackCover:      get("STACK_COVER", "display"),
		StackTolerance:  time.Duration(num("STACK_TOLERANCE", 2, 0)) * time.Second,
		DisksDir:        get("DISKS_DIR", "/mnt/disks"),
		ConfigDir:       get("CONFIG_DIR", "/config"),
		PollInterval:    time.Duration(num("POLL_INTERVAL", 5, 1)) * time.Second,
		Parallel:        num("UPLOAD_PARALLEL", 2, 1),
	}

	if c.ImmichURL == "" {
		errs = append(errs, errors.New("IMMICH_URL fehlt (z.B. http://<server-ip>:2283)"))
	} else if u, err := url.Parse(c.ImmichURL); err != nil || u.Scheme == "" || u.Host == "" {
		errs = append(errs, fmt.Errorf("IMMICH_URL=%q ist keine Adresse wie http://<server-ip>:2283", c.ImmichURL))
	}
	if c.APIKey == "" {
		errs = append(errs, errors.New("IMMICH_API_KEY fehlt"))
	}
	if chat := get("TG_CHAT_ID", ""); chat != "" {
		n, err := strconv.ParseInt(chat, 10, 64)
		if err != nil {
			errs = append(errs, fmt.Errorf("TG_CHAT_ID=%q ist keine Zahl", chat))
		}
		c.TGChat = n
	}
	if (c.TGToken == "") != (c.TGChat == 0) {
		errs = append(errs, errors.New("für Telegram TG_BOT_TOKEN und TG_CHAT_ID beide setzen (oder beide leer lassen)"))
	}
	switch c.AlbumMode {
	case "ask", "date", "fixed", "none":
	default:
		errs = append(errs, fmt.Errorf("ALBUM_MODE=%q: ask, date, fixed oder none", c.AlbumMode))
	}
	if c.AlbumMode == "fixed" && c.Album == "" {
		errs = append(errs, errors.New("ALBUM_MODE=fixed braucht IMMICH_ALBUM"))
	}
	if c.AlbumMode == "ask" && c.TGToken == "" {
		c.AlbumMode = "date" // ohne Bot kann niemand antworten
	}
	if c.StackCover != "display" && c.StackCover != "raw" {
		errs = append(errs, fmt.Errorf("STACK_COVER=%q: display oder raw", c.StackCover))
	}
	for _, f := range strings.Fields(get("PROGRESS_STEPS", "25 50 75")) {
		n, err := strconv.Atoi(f)
		if err != nil || n <= 0 || n >= 100 {
			errs = append(errs, fmt.Errorf("PROGRESS_STEPS: %q ist keine Zahl zwischen 1 und 99", f))
			continue
		}
		c.ProgressSteps = append(c.ProgressSteps, n)
	}
	sort.Ints(c.ProgressSteps)
	return c, errors.Join(errs...)
}
