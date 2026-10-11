
# ============== AB HIER NICHTS ÄNDERN – Installationslogik ===================

# Von einer Kopie laufen: das Skript schreibt unten seine eigenen Einstellungen
# zurück, und bash liest eine Datei während der Ausführung nach.
if [[ "${CAMERA_IMPORT_KOPIE:-}" != 1 && -f "$0" ]]; then
  kopie=$(mktemp)
  cp "$0" "$kopie"
  CAMERA_IMPORT_KOPIE=1 exec bash "$kopie" "$@"
fi

BASE_DIR="/boot/config/camera-import"
CONF_FILE="$BASE_DIR/camera-import.conf"
US_DIR="/boot/config/plugins/user.scripts/scripts"
IMPORT_DIR="$US_DIR/camera-import"
IMPORT_LOG_DIR="/tmp/user.scripts/tmpScripts/camera-import"
LOCK_FILE="/var/run/camera-import.lock"
BOT_PID_FILE="/var/run/camera-import-bot.pid"
BOT_LOG="/tmp/camera-import/bot.log"
KEYS=(IMMICH_URL IMMICH_API_KEY TG_BOT_TOKEN TG_CHAT_ID ALBUM_MODE IMMICH_ALBUM
      ALBUM_ASK_TIMEOUT DRY_RUN AUTO_UNMOUNT PROGRESS_STEPS RAW_JPEG STACK_PAIRS
      STACK_COVER STACK_TOLERANCE IMMICH_GO_VERSION CHECK_UPDATES)

ok()   { echo "  ✔ $*"; }
warn() { echo "  ⚠ $*"; WARNINGS+=("$*"); }
die()  { echo; echo "FEHLER: $*"; exit 1; }
WARNINGS=()

# Dieses Skript auf dem USB-Stick (für das Zurückschreiben der Einstellungen)
own_file() {
  local f
  for f in "$US_DIR"/*/script; do
    [[ "$f" == "$IMPORT_DIR/script" ]] && continue
    grep -qx '# camera-import-installer' "$f" 2>/dev/null && { echo "$f"; return; }
  done
}

# Frisch eingefügte Vorlage: Werte, die noch auf dem Vorgabewert stehen, aus der
# bisherigen Konfiguration übernehmen – so überlebt ein Update die Einstellungen.
merge_previous() {
  local k old
  [[ "$VORLAGE" == 1 && -r "$CONF_FILE" ]] || return 0
  declare -A prev=()
  while IFS= read -r -d '' k && IFS= read -r -d '' old; do
    prev[$k]="$old"
  done < <(
    # shellcheck source=/dev/null
    source "$CONF_FILE" >/dev/null 2>&1
    for k in "${KEYS[@]}"; do
      [[ -n "${!k+x}" ]] && printf '%s\0%s\0' "$k" "${!k}"
    done
  )
  for k in "${KEYS[@]}"; do
    [[ -n "${prev[$k]+x}" ]] || continue
    if [[ "$k" == IMMICH_GO_VERSION ]]; then
      # die neuere von beiden
      IMMICH_GO_VERSION=$(printf '%s\n%s\n' "${prev[$k]}" "$IMMICH_GO_VERSION" | sort -V | tail -n1)
    elif [[ "${!k}" == "${VORLAGE_WERTE[$k]}" ]]; then
      printf -v "$k" '%s' "${prev[$k]}"
    fi
  done
  ok "bisherige Einstellungen übernommen"
}

check_settings() {
  local k
  for k in "${KEYS[@]}"; do
    [[ "${!k}" == *[\"\\\$\`]* ]] && die "$k enthält \", \\, \$ oder \` – das geht nicht."
  done
  IMMICH_URL="${IMMICH_URL%/}"
  [[ -n "$IMMICH_URL" ]]     || die "IMMICH_URL oben im Skript eintragen."
  [[ -n "$IMMICH_API_KEY" ]] || die "IMMICH_API_KEY oben im Skript eintragen."
  [[ "$IMMICH_URL" == http* ]] || die "IMMICH_URL muss mit http:// oder https:// beginnen."
  [[ -n "$TG_BOT_TOKEN" && -n "$TG_CHAT_ID" ]] || warn "Telegram nicht eingetragen – keine Meldungen, kein Bot."
}

write_conf() {
  local k
  mkdir -p "$BASE_DIR"
  {
    echo "# Von camera-import-install erzeugt – Änderungen bitte dort vornehmen."
    for k in "${KEYS[@]}"; do printf '%s="%s"\n' "$k" "${!k}"; done
  } > "$CONF_FILE"
  ok "Einstellungen gespeichert"
}

# Übernommene Werte oben ins Skript zurückschreiben, damit die Oberfläche
# zeigt, was wirklich gilt, und VORLAGE auf 0 setzen.
write_back() {
  local own tmp k
  [[ "$VORLAGE" == 1 ]] || return 0
  own=$(own_file)
  if [[ -z "$own" ]]; then
    warn "Konnte das Skript nicht finden, um die Einstellungen zurückzuschreiben."
    return 0
  fi
  tmp=$(mktemp)
  export VORLAGE=0
  for k in "${KEYS[@]}"; do export "${k?}"; done
  awk -v keys="${KEYS[*]} VORLAGE" '
    BEGIN { n = split(keys, a, " "); for (i = 1; i <= n; i++) want[a[i]] = 1 }
    /^# =+ ENDE DER EINSTELLUNGEN/ { done = 1 }
    !done && match($0, /^[A-Z_]+=/) {
      k = substr($0, 1, RLENGTH - 1)
      if (k in want) { print k "=\"" ENVIRON[k] "\""; next }
    }
    { print }' "$own" > "$tmp" && cp "$tmp" "$own"
  rm -f "$tmp"
  ok "Einstellungen ins Skript übernommen ($own)"
}

stop_bot() {
  local pid
  pid=$(cat "$BOT_PID_FILE" 2>/dev/null)
  if [[ -n "$pid" ]] && kill -0 "$pid" 2>/dev/null; then
    pkill -P "$pid" 2>/dev/null
    kill "$pid" 2>/dev/null
  fi
  rm -f "$BOT_PID_FILE"
}

start_bot() {
  if [[ -z "$TG_BOT_TOKEN" ]]; then return 0; fi
  mkdir -p "$(dirname "$BOT_LOG")"
  setsid nohup bash "$BASE_DIR/bot.sh" >> "$BOT_LOG" 2>&1 < /dev/null &
  echo $! > "$BOT_PID_FILE"
  ok "Telegram-Bot läuft"
}

put() {
  mkdir -p "$(dirname "$1")"
  cat > "$1"
}

install_files() {
  write_files
  printf 'camera-import\n' > "$IMPORT_DIR/name"
  printf 'SD-Karte/Kamera nach Immich importieren – startet beim Einstecken von selbst. Einstellungen: Skript camera-import-install\n' \
    > "$IMPORT_DIR/description"
  # Reste der alten Einrichtung – camera-import-setup aber nur, wenn nicht
  # dieses Skript selbst dort eingefügt wurde
  if ! grep -qx '# camera-import-installer' "$US_DIR/camera-import-setup/script" 2>/dev/null; then
    rm -rf "$US_DIR/camera-import-setup"
  fi
  rm -f "$BASE_DIR/camera-import.conf.example" "$BASE_DIR/bin/README.md"
  ok "Programmdateien geschrieben"
}

install_immich_go() {
  local bin="$BASE_DIR/bin/immich-go" stamp="$BASE_DIR/bin/immich-go.version" tmp url
  # /boot ist nicht ausführbar, deshalb merkt sich eine Datei die Version
  if [[ ! -f "$bin" || "$(cat "$stamp" 2>/dev/null)" != "$IMMICH_GO_VERSION" ]]; then
    url="https://github.com/simulot/immich-go/releases/download/v${IMMICH_GO_VERSION}/immich-go_Linux_x86_64.tar.gz"
    tmp=$(mktemp -d)
    if curl -fsSL -m 300 "$url" -o "$tmp/ig.tgz" && tar xzf "$tmp/ig.tgz" -C "$tmp" immich-go; then
      mkdir -p "$BASE_DIR/bin"
      cp "$tmp/immich-go" "$bin"   # /boot ist FAT: cp statt mv
      echo "$IMMICH_GO_VERSION" > "$stamp"
      ok "immich-go v$IMMICH_GO_VERSION heruntergeladen"
    elif [[ -f "$bin" ]]; then
      warn "immich-go v$IMMICH_GO_VERSION nicht ladbar – vorhandene Version bleibt."
    else
      rm -rf "$tmp"
      die "immich-go v$IMMICH_GO_VERSION konnte nicht geladen werden ($url)."
    fi
    rm -rf "$tmp"
  fi
  install -m 0755 "$bin" /usr/local/bin/immich-go
  ok "immich-go: $(/usr/local/bin/immich-go --version 2>&1 | head -n1)"
}

install_trigger() {
  # udev darf nur kurz laufen, deshalb wird der Import per at abgekoppelt.
  cat > /usr/local/bin/camera-import-trigger <<EOF
#!/bin/bash
kname="\$1"
mkdir -p "$IMPORT_LOG_DIR"
cmd="bash $IMPORT_DIR/script \$kname >> $IMPORT_LOG_DIR/log.txt 2>&1"
if command -v at >/dev/null 2>&1; then
  echo "\$cmd" | at now 2>/dev/null && exit 0
fi
setsid nohup bash -c "\$cmd" >/dev/null 2>&1 < /dev/null &
EOF
  chmod 0755 /usr/local/bin/camera-import-trigger
  # Jede USB-Partition außer dem Unraid-Stick; ob DCIM da ist, prüft der Import.
  cat > /etc/udev/rules.d/99-camera-import.rules <<'EOF'
ACTION=="add", SUBSYSTEM=="block", ENV{DEVTYPE}=="partition", ENV{ID_BUS}=="usb", ENV{ID_FS_LABEL}!="UNRAID", ENV{ID_FS_TYPE}=="vfat|exfat|ntfs", RUN+="/usr/local/bin/camera-import-trigger %k"
EOF
  udevadm control --reload-rules
  ok "Auslöser beim Einstecken aktiv"
}

check_immich() {
  local code
  if ! curl -fsS -m 10 "$IMMICH_URL/api/server/ping" 2>/dev/null | grep -q pong; then
    warn "Immich unter $IMMICH_URL nicht erreichbar."
    return
  fi
  code=$(curl -s -o /dev/null -w '%{http_code}' -m 10 -H "x-api-key: $IMMICH_API_KEY" "$IMMICH_URL/api/server/about")
  case "$code" in
    200) ok "Immich erreichbar, API-Key gültig" ;;
    401) warn "Immich lehnt den API-Key ab (401) – Key prüfen." ;;
    403) warn "API-Key hat zu wenige Rechte (403) – siehe README." ;;
    *)   warn "Immich antwortet mit HTTP $code auf den API-Key-Test." ;;
  esac
}

check_schedule() {
  local own="$1"
  [[ -n "$own" ]] || return 0
  php -r '$s = json_decode(@file_get_contents("/boot/config/plugins/user.scripts/schedule.json"), true) ?: [];
          exit((($s[$argv[1]]["frequency"] ?? "") === "start") ? 0 : 1);' "$own" 2>/dev/null \
    || warn "Zeitplan dieses Skripts auf „At Startup of Array“ stellen, sonst fehlt nach einem Neustart alles."
}

main() {
  local own msg

  # Einen Import nicht mittendrin umbauen
  exec 9>"$LOCK_FILE"
  if ! flock -n 9; then
    echo "Gerade läuft ein Import. Bitte danach noch einmal „Run Script“ klicken."
    exit 1
  fi

  if grep -qx '# camera-import-installer' "$IMPORT_DIR/script" 2>/dev/null; then
    die "Dieses Skript darf nicht „camera-import“ heißen – bitte z.B. „camera-import-install“ nennen."
  fi

  echo "camera-import wird eingerichtet …"
  merge_previous
  check_settings
  write_conf
  write_back
  stop_bot
  install_files
  install_immich_go
  install_trigger
  check_immich
  start_bot
  own=$(own_file)
  check_schedule "$own"

  # shellcheck source=/dev/null
  source "$BASE_DIR/lib.sh"
  load_config >/dev/null
  msg="✅ camera-import eingerichtet (immich-go v$IMMICH_GO_VERSION)."
  (( DRY_RUN )) && msg+=$'\n'"Probelauf ist an (DRY_RUN=1) – es wird nichts hochgeladen."
  (( ${#WARNINGS[@]} )) && msg+=$'\n'"$(printf '⚠️ %s\n' "${WARNINGS[@]}")"
  if [[ -n "$TG_BOT_TOKEN" ]]; then
    if curl -sS -m 15 "https://api.telegram.org/bot${TG_BOT_TOKEN}/sendMessage" \
         --data-urlencode "chat_id=${TG_CHAT_ID}" --data-urlencode "text=$msg" 2>&1 | grep -q '"ok":true'; then
      ok "Telegram-Testnachricht gesendet"
    else
      warn "Telegram-Nachricht ging nicht raus – Token und TG_CHAT_ID prüfen (Chat-ID ist deine, nicht die des Bots)."
    fi
  fi
  check_immich_go_update

  echo
  if (( ${#WARNINGS[@]} )); then
    echo "Fertig, mit ${#WARNINGS[@]} Hinweis(en) – siehe ⚠ oben."
  else
    echo "Fertig. Karte einstecken genügt."
  fi
}
