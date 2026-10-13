// camera-import: uploads photos from inserted camera cards to Immich and
// reports via Telegram. Runs as a Docker container on Unraid.
package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"syscall"
	_ "time/tzdata" // time zones without zoneinfo in the image (Unraid sets TZ)

	"github.com/DevHovAI/unraid-immich-import/internal/app"
	"github.com/DevHovAI/unraid-immich-import/internal/config"
	"github.com/DevHovAI/unraid-immich-import/internal/memory"
)

var version = "dev" // set at build time via -ldflags

func main() {
	if len(os.Args) > 1 && os.Args[1] == "--version" {
		fmt.Println(version)
		return
	}
	cfg, err := config.Load(os.Getenv)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Invalid settings – fix them in the container template:\n%v\n", err)
		os.Exit(2)
	}
	if err := dropPrivileges(cfg.ConfigDir); err != nil {
		fmt.Fprintf(os.Stderr, "%v\n", err)
		os.Exit(1)
	}

	log := app.NewLogger(filepath.Join(cfg.ConfigDir, "camera-import.log"), os.Stdout)
	app.MigrateFileNames(cfg.ConfigDir, log)
	mem, err := memory.Open(filepath.Join(cfg.ConfigDir, app.MemoryFile))
	if err != nil {
		log.Printf("open memory: %v", err)
		os.Exit(1)
	}
	defer mem.Close()

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	log.Printf("camera-import %s starting, watching %s", version, cfg.DisksDir)
	app.New(cfg, version, log, mem).Run(ctx)
	log.Printf("stopped")
}

// Started as root (Docker default): hand /config to the Unraid user, then
// continue as PUID:PGID (default 99:100 = nobody:users).
func dropPrivileges(configDir string) error {
	if err := os.MkdirAll(configDir, 0o755); err != nil {
		return fmt.Errorf("create %s: %w", configDir, err)
	}
	if os.Getuid() != 0 {
		return nil
	}
	uid, gid := envInt("PUID", 99), envInt("PGID", 100)
	if uid == 0 {
		return nil
	}
	err := filepath.WalkDir(configDir, func(p string, _ os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		return os.Lchown(p, uid, gid)
	})
	if err != nil {
		return fmt.Errorf("hand %s to %d:%d: %w", configDir, uid, gid, err)
	}
	if err := syscall.Setgroups(nil); err != nil {
		return err
	}
	if err := syscall.Setgid(gid); err != nil {
		return err
	}
	return syscall.Setuid(uid)
}

func envInt(k string, def int) int {
	if n, err := strconv.Atoi(os.Getenv(k)); err == nil && n >= 0 {
		return n
	}
	return def
}
