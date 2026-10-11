# unraid-immich-import — lokale Prüfungen

set shell := ["bash", "-euo", "pipefail", "-c"]

scripts := "user.scripts/camera-import/script user.scripts/camera-import-setup/script boot/camera-import/lib.sh boot/camera-import/bot.sh boot/camera-import/stack.sh"

# Rezepte auflisten
default:
    @just --list --unsorted

# Alles, was auch die CI prüft. Vor jedem Push.
check: syntax lint
    @echo
    @echo "alles grün"

# Syntax aller Skripte
syntax:
    for f in {{scripts}}; do bash -n "$f"; done

# shellcheck mit Auflösung der source-Pfade
lint:
    shellcheck -x -P SCRIPTDIR {{scripts}}

# Datenschutz-Prüfung — muss vor jedem Push grün sein
privacy *ARGS:
    bash ~/.claude/skills/repo-flow/scripts/privacy-scan.sh {{ARGS}}
