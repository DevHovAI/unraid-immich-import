#!/bin/bash
# Gemeinsame Funktionen für das Import-Skript und den Telegram-Bot.
# Wird mit "source" eingebunden, nicht direkt ausgeführt.

# shellcheck disable=SC2034  # Variablen werden in den einbindenden Skripten genutzt

BASE_DIR="/boot/config/camera-import"
CONF_FILE="$BASE_DIR/camera-import.conf"
STATE_DIR="/tmp/camera-import"            # Laufzeitstatus, weg nach Neustart
LAST_FILE="$BASE_DIR/letzter-import.txt"  # übersteht einen Neustart
UPDATE_STAMP="$BASE_DIR/.immich-go-gemeldet"
SCRIPT_LOG="/tmp/user.scripts/tmpScripts/camera-import/log.txt"
IMMICH_GO="/usr/local/bin/immich-go"

# Status des laufenden Imports (von bot.sh gelesen)
CURRENT_FILE="$STATE_DIR/aktuell.env"
CANCEL_FILE="$STATE_DIR/abbrechen"
ALBUM_REQUEST="$STATE_DIR/album-frage"
ALBUM_ANSWER="$STATE_DIR/album-antwort"

load_config() {
  IMMICH_URL="" IMMICH_API_KEY="" TG_BOT_TOKEN="" TG_CHAT_ID=""
  ALBUM_MODE="none" IMMICH_ALBUM="" ALBUM_ASK_TIMEOUT=120
  RAW_JPEG="StackCoverJPG" DRY_RUN=0 AUTO_UNMOUNT=1
  PROGRESS_STEPS="25 50 75" CHECK_UPDATES=1
  STACK_PAIRS=1 STACK_COVER="display" STACK_TOLERANCE=2
  if [[ ! -r "$CONF_FILE" ]]; then
    log "Konfiguration fehlt: $CONF_FILE – Skript camera-import-install ausführen"
    return 1
  fi
  # shellcheck source=/dev/null
  source "$CONF_FILE"
  IMMICH_URL="${IMMICH_URL%/}"
  mkdir -p "$STATE_DIR"
}

log() { echo "$(date '+%F %T') $*"; }

fmt_dur() { echo "$(($1 / 60))m $(($1 % 60))s"; }

# Telegram-Nachricht senden (max. 4096 Zeichen erlaubt)
notify() {
  local resp
  [[ -n "$TG_BOT_TOKEN" && -n "$TG_CHAT_ID" ]] || return 0
  resp=$(curl -sS -m 15 "https://api.telegram.org/bot${TG_BOT_TOKEN}/sendMessage" \
    --data-urlencode "chat_id=${TG_CHAT_ID}" \
    --data-urlencode "text=${1:0:4000}" 2>&1)
  [[ "$resp" == *'"ok":true'* ]] || log "Telegram-Nachricht fehlgeschlagen: $resp"
}

# Telegram-Updates als Zeilen "update_id<TAB>chat_id<TAB>text" ausgeben.
# Rückgabe 1 bei Netzwerk- oder API-Fehler. php ist auf Unraid immer da, jq nicht.
# shellcheck disable=SC2016  # PHP-Code in einfachen Anführungszeichen
tg_updates() {
  curl -sS -m $(( ${2:-0} + 15 )) "https://api.telegram.org/bot${TG_BOT_TOKEN}/getUpdates" \
    -d offset="$1" -d timeout="${2:-0}" -d 'allowed_updates=["message"]' 2>/dev/null \
  | php -r '$d = json_decode(stream_get_contents(STDIN), true);
      if (empty($d["ok"])) exit(1);
      foreach ($d["result"] as $u) {
        $m = $u["message"] ?? [];
        echo $u["update_id"], "\t", $m["chat"]["id"] ?? "", "\t",
             str_replace(["\t", "\n"], " ", $m["text"] ?? ""), "\n";
      }'
}

# Läuft gerade ein Import? Lädt dann PID, OUT, GOLOG, LABEL, TOTAL, START, MODE.
import_running() {
  [[ -r "$CURRENT_FILE" ]] || return 1
  # shellcheck disable=SC1090
  source "$CURRENT_FILE"
  kill -0 "${PID:-0}" 2>/dev/null
}

# Fortschritt des laufenden Imports; setzt PROG_DONE und PROG_PCT
progress() {
  local up dup
  up=$(tail -c 500 "$OUT" 2>/dev/null | tr '\r' '\n' | grep '^Immich read' | tail -n1 \
       | sed -nE 's/.*Uploaded ([0-9]+).*/\1/p')
  dup=$(grep -c ' INF server has duplicate' "$GOLOG" 2>/dev/null)
  PROG_UP=${up:-0}
  PROG_DUP=${dup:-0}
  PROG_ERR=$(grep -c ' ERR ' "$GOLOG" 2>/dev/null)
  PROG_DONE=$(( PROG_UP + PROG_DUP ))
  PROG_PCT=$(( TOTAL > 0 ? 100 * PROG_DONE / TOTAL : 0 ))
}

status_text() {
  progress
  echo "📊 Import läuft${MODE} ($LABEL)
Erledigt: $PROG_DONE von $TOTAL Dateien ($PROG_PCT %)
  davon neu hochgeladen: $PROG_UP
  davon schon vorhanden: $PROG_DUP
Fehler: ${PROG_ERR:-0}
Laufzeit: $(fmt_dur $(( $(date +%s) - START )))"
}

# Meldet einmal pro Version, wenn es ein neueres immich-go gibt
check_immich_go_update() {
  (( CHECK_UPDATES )) || return 0
  local installed latest
  installed=$("$IMMICH_GO" --version 2>/dev/null | grep -oE '[0-9]+\.[0-9]+\.[0-9]+' | head -n1)
  latest=$(curl -sS -m 15 https://api.github.com/repos/simulot/immich-go/releases/latest 2>/dev/null \
           | grep -oE '"tag_name": *"v?[0-9]+\.[0-9]+\.[0-9]+"' | grep -oE '[0-9]+\.[0-9]+\.[0-9]+')
  [[ -n "$installed" && -n "$latest" && "$installed" != "$latest" ]] || return 0
  # nur melden, wenn wirklich neuer und noch nicht gemeldet
  [[ "$(printf '%s\n%s\n' "$installed" "$latest" | sort -V | tail -n1)" == "$latest" ]] || return 0
  [[ "$(cat "$UPDATE_STAMP" 2>/dev/null)" == "$latest" ]] && return 0
  notify "ℹ️ immich-go v$latest ist erschienen (installiert: v$installed).
Aktualisieren: im Skript camera-import-install IMMICH_GO_VERSION=\"$latest\" setzen und Run Script klicken (vorher Release-Notes lesen).
https://github.com/simulot/immich-go/releases/tag/v$latest"
  echo "$latest" > "$UPDATE_STAMP"
}
