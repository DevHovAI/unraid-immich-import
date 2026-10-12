#!/bin/bash
# Holt die neueste camera-import-install.sh von GitHub, ersetzt damit das
# Installationsskript in User Scripts und führt es aus. Die Einstellungen
# übernimmt das Installationsskript selbst aus der bisherigen Konfiguration.
# Gestartet vom Bot mit /update.

# Alles steht in main(): bash liest ein Skript während der Ausführung nach, und
# das Installationsskript überschreibt auch diese Datei.
main() {
  # shellcheck source=lib.sh
  source "$(dirname "$0")/lib.sh"
  load_config || exit 1

  fail() { log "Update fehlgeschlagen: $*"; notify "❌ Update fehlgeschlagen: $*"; exit 1; }

  own=$(installer_file) || fail "Installationsskript in User Scripts nicht gefunden."
  tmp=$(mktemp)
  trap 'rm -f "$tmp"' EXIT
  curl -fsSL -m 120 "https://github.com/$REPO/releases/latest/download/camera-import-install.sh" -o "$tmp" \
    || fail "Download von GitHub fehlgeschlagen."
  if ! grep -qx '# camera-import-installer' "$tmp" || ! bash -n "$tmp"; then
    fail "Die heruntergeladene Datei ist kein gültiges Installationsskript."
  fi

  cp "$tmp" "$own"   # /boot ist FAT: cp statt mv
  log "Update: $own ersetzt, wird ausgeführt"
  ilog="/tmp/user.scripts/tmpScripts/$(basename "$(dirname "$own")")/log.txt"
  mkdir -p "$(dirname "$ilog")"
  # Das Installationsskript beendet und startet den Bot neu und meldet sich
  # selbst mit ✅ und der neuen Version.
  env -u CAMERA_IMPORT_KOPIE bash "$own" >> "$ilog" 2>&1 || fail "Installationsskript meldet einen Fehler, siehe Log von camera-import-install."
}
main "$@"; exit $?
