// Package config reads the settings from environment variables. The names
// are the same as in the script version (camera-import-install).
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
	TGChat  int64 // 0 = Telegram off

	AlbumMode       string // ask, date, fixed, none
	Album           string
	AlbumAskTimeout time.Duration

	DryRun        bool
	ProgressSteps []int // percentages for progress messages

	StackPairs     bool
	AutoUnmount    bool   // ask the helper script to unmount the card afterwards
	StackCover     string // display or raw
	StackTolerance time.Duration

	DisksDir     string // where Unassigned Devices mounts the cards
	ConfigDir    string // memory, log, last import
	BackupDir    string // target of /backup on
	PollInterval time.Duration
	Parallel     int // concurrent uploads
}

// Load reads the settings via getenv (os.Getenv, a map in tests).
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
			errs = append(errs, fmt.Errorf("%s=%q is not a number ≥ %d", k, s, min))
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
			errs = append(errs, fmt.Errorf("%s=%q: expected 0 or 1", k, s))
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
		AutoUnmount:     flag("AUTO_UNMOUNT", "1"),
		StackCover:      get("STACK_COVER", "display"),
		StackTolerance:  time.Duration(num("STACK_TOLERANCE", 2, 0)) * time.Second,
		DisksDir:        get("DISKS_DIR", "/mnt/disks"),
		ConfigDir:       get("CONFIG_DIR", "/config"),
		BackupDir:       get("BACKUP_DIR", "/backup"),
		PollInterval:    time.Duration(num("POLL_INTERVAL", 5, 1)) * time.Second,
		Parallel:        num("UPLOAD_PARALLEL", 2, 1),
	}

	if c.ImmichURL == "" {
		errs = append(errs, errors.New("IMMICH_URL is missing (e.g. http://<server-ip>:2283)"))
	} else if u, err := url.Parse(c.ImmichURL); err != nil || u.Scheme == "" || u.Host == "" {
		errs = append(errs, fmt.Errorf("IMMICH_URL=%q is not an address like http://<server-ip>:2283", c.ImmichURL))
	}
	if c.APIKey == "" {
		errs = append(errs, errors.New("IMMICH_API_KEY is missing"))
	}
	if chat := get("TG_CHAT_ID", ""); chat != "" {
		n, err := strconv.ParseInt(chat, 10, 64)
		if err != nil {
			errs = append(errs, fmt.Errorf("TG_CHAT_ID=%q is not a number", chat))
		}
		c.TGChat = n
	}
	if (c.TGToken == "") != (c.TGChat == 0) {
		errs = append(errs, errors.New("for Telegram, set both TG_BOT_TOKEN and TG_CHAT_ID (or leave both empty)"))
	}
	switch c.AlbumMode {
	case "ask", "date", "fixed", "none":
	default:
		errs = append(errs, fmt.Errorf("ALBUM_MODE=%q: ask, date, fixed or none", c.AlbumMode))
	}
	if c.AlbumMode == "fixed" && c.Album == "" {
		errs = append(errs, errors.New("ALBUM_MODE=fixed needs IMMICH_ALBUM"))
	}
	if c.AlbumMode == "ask" && c.TGToken == "" {
		c.AlbumMode = "date" // without a bot nobody can answer
	}
	if c.StackCover != "display" && c.StackCover != "raw" {
		errs = append(errs, fmt.Errorf("STACK_COVER=%q: display or raw", c.StackCover))
	}
	for _, f := range strings.Fields(get("PROGRESS_STEPS", "25 50 75")) {
		n, err := strconv.Atoi(f)
		if err != nil || n <= 0 || n >= 100 {
			errs = append(errs, fmt.Errorf("PROGRESS_STEPS: %q is not a number between 1 and 99", f))
			continue
		}
		c.ProgressSteps = append(c.ProgressSteps, n)
	}
	sort.Ints(c.ProgressSteps)
	return c, errors.Join(errs...)
}
