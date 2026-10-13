# unraid-immich-import — local checks

set shell := ["bash", "-euo", "pipefail", "-c"]

# Go runs in podman because the system has no Go
go := "podman run --rm -v " + justfile_directory() + ":/src:z -w /src -e GOFLAGS=-buildvcs=false -e GOCACHE=/src/.cache/go-build -e GOMODCACHE=/src/.cache/go-mod docker.io/library/golang:1.24"
legacy_scripts := "legacy/user.scripts/camera-import/script legacy/boot/camera-import/lib.sh legacy/boot/camera-import/bot.sh legacy/boot/camera-import/stack.sh legacy/boot/camera-import/update.sh legacy/camera-import-install.sh"

# List recipes
default:
    @just --list --unsorted

# Everything CI checks too. Before every push.
check: fmt vet test legacy
    @echo
    @echo "all green"

# Check formatting
fmt:
    out=$({{go}} gofmt -l .); if [[ -n "$out" ]]; then echo "not formatted: $out"; exit 1; fi

vet:
    {{go}} go vet ./...

# Tests with race detector
test:
    {{go}} go test -race ./...

# Build container image
image version="dev":
    podman build --build-arg VERSION={{version}} -t camera-import:{{version}} .

# Fake Immich on port 2283 for smoke tests (API key KEY)
fake:
    podman run --rm -p 2283:2283 -v {{justfile_directory()}}:/src:z -w /src -e GOFLAGS=-buildvcs=false docker.io/library/golang:1.24 go run ./internal/fakeimmich/cmd/fakeimmich :2283

# Script version (v1): installer up to date, syntax, shellcheck
legacy:
    cd legacy && bash build.sh >/dev/null
    git diff --exit-code legacy/camera-import-install.sh >/dev/null || { echo "legacy/camera-import-install.sh is out of date"; exit 1; }
    for f in {{legacy_scripts}}; do bash -n "$f"; done
    shellcheck -x -P SCRIPTDIR {{legacy_scripts}}

# Privacy check — must be green before every push
privacy *ARGS:
    bash ~/.claude/skills/repo-flow/scripts/privacy-scan.sh {{ARGS}}
