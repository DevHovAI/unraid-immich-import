# unraid-immich-import — lokale Prüfungen

set shell := ["bash", "-euo", "pipefail", "-c"]

# Go läuft in podman, weil das System kein Go mitbringt
go := "podman run --rm -v " + justfile_directory() + ":/src:z -w /src -e GOFLAGS=-buildvcs=false -e GOCACHE=/src/.cache/go-build -e GOMODCACHE=/src/.cache/go-mod docker.io/library/golang:1.24"
legacy_scripts := "legacy/user.scripts/camera-import/script legacy/boot/camera-import/lib.sh legacy/boot/camera-import/bot.sh legacy/boot/camera-import/stack.sh legacy/boot/camera-import/update.sh legacy/camera-import-install.sh"

# Rezepte auflisten
default:
    @just --list --unsorted

# Alles, was auch die CI prüft. Vor jedem Push.
check: fmt vet test legacy
    @echo
    @echo "alles grün"

# Formatierung prüfen
fmt:
    out=$({{go}} gofmt -l .); if [[ -n "$out" ]]; then echo "nicht formatiert: $out"; exit 1; fi

vet:
    {{go}} go vet ./...

# Tests mit Race-Detector
test:
    {{go}} go test -race ./...

# Container-Image bauen
image version="dev":
    podman build --build-arg VERSION={{version}} -t camera-import:{{version}} .

# Nachgebautes Immich auf Port 2283 für Rauchtests (API-Key KEY)
fake:
    podman run --rm -p 2283:2283 -v {{justfile_directory()}}:/src:z -w /src -e GOFLAGS=-buildvcs=false docker.io/library/golang:1.24 go run ./internal/fakeimmich/cmd/fakeimmich :2283

# Skript-Version (v1): Installer aktuell, Syntax, shellcheck
legacy:
    cd legacy && bash build.sh >/dev/null
    git diff --exit-code legacy/camera-import-install.sh >/dev/null || { echo "legacy/camera-import-install.sh veraltet"; exit 1; }
    for f in {{legacy_scripts}}; do bash -n "$f"; done
    shellcheck -x -P SCRIPTDIR {{legacy_scripts}}

# Datenschutz-Prüfung — muss vor jedem Push grün sein
privacy *ARGS:
    bash ~/.claude/skills/repo-flow/scripts/privacy-scan.sh {{ARGS}}
