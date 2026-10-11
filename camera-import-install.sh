#!/bin/bash
#description=Kamera/SD-Karte → Immich mit Telegram-Bot. Einstellungen oben im Skript ändern, dann „Run Script“. Zeitplan: At Startup of Array
# shellcheck disable=SC2034,SC1111,SC2016  # Einstellungen werden über ${!k} gelesen; deutsche Anführungszeichen; PHP-Code
# camera-import-installer
#
# camera-import – Installation und Einstellungen in einem Skript
#
# Einrichten:  in User Scripts „Add New Script“, Inhalt hier hinein kopieren,
#              unten die Einstellungen ausfüllen, „Run Script“ klicken und den
#              Zeitplan auf „At Startup of Array“ stellen.
# Ändern:      Einstellung anpassen (z.B. API-Key), speichern, „Run Script“.
# Neue Version: Inhalt komplett ersetzen und „Run Script“ klicken – deine
#              bisherigen Einstellungen werden dabei automatisch übernommen.
#
# Das Skript legt alles Nötige selbst an, auch das User Script „camera-import“
# für den eigentlichen Import. Das musst du nicht anfassen.

# ============================ EINSTELLUNGEN ==================================

# --- Immich ---
# Adresse von Immich, z.B. http://<server-ip>:2283
IMMICH_URL=""
# Immich → Kontoeinstellungen → API-Schlüssel. Rechte siehe README, oder einfach „Alle“.
IMMICH_API_KEY=""

# --- Telegram ---
# Token von @BotFather
TG_BOT_TOKEN=""
# Deine Chat-ID: dem Bot schreiben, dann
# https://api.telegram.org/bot<TOKEN>/getUpdates öffnen → "chat":{"id":…}
TG_CHAT_ID=""

# --- Album ---
# ask = Bot fragt nach dem Namen | date = „Import JJJJ-MM-TT“ | fixed = IMMICH_ALBUM | none = keins
ALBUM_MODE="ask"
IMMICH_ALBUM=""
# Sekunden, die der Bot auf den Albumnamen wartet
ALBUM_ASK_TIMEOUT="120"

# --- Import ---
# 1 = Probelauf, nichts hochladen
DRY_RUN="0"
# 1 = von Unassigned Devices eingehängte Karten danach aushängen
AUTO_UNMOUNT="1"
# Zwischenstände in Prozent, leer = keine
PROGRESS_STEPS="25 50 75"
# RAW+JPG bei immich-go: NoStack, KeepRaw, KeepJPG, StackCoverRaw, StackCoverJPG
RAW_JPEG="StackCoverJPG"

# --- Stapeln (RAW + HIF/JPG) ---
# 1 = nach jedem Import Paare stapeln
STACK_PAIRS="1"
# display = HIF/JPG liegt vorne, raw = RAW liegt vorne
STACK_COVER="display"
# höchster Abstand der Aufnahmezeiten in Sekunden
STACK_TOLERANCE="2"

# --- immich-go ---
# Version, die installiert wird. Zum Aktualisieren hier ändern und „Run Script“.
IMMICH_GO_VERSION="0.32.0"
# 1 = per Telegram melden, wenn eine neue Version erscheint
CHECK_UPDATES="1"

# Intern, nicht ändern: 1 = frisch eingefügt, bisherige Einstellungen übernehmen
VORLAGE="1"

# ========================= ENDE DER EINSTELLUNGEN ============================

declare -A VORLAGE_WERTE=(
  [IMMICH_URL]=""
  [IMMICH_API_KEY]=""
  [TG_BOT_TOKEN]=""
  [TG_CHAT_ID]=""
  [ALBUM_MODE]="ask"
  [IMMICH_ALBUM]=""
  [ALBUM_ASK_TIMEOUT]="120"
  [DRY_RUN]="0"
  [AUTO_UNMOUNT]="1"
  [PROGRESS_STEPS]="25 50 75"
  [RAW_JPEG]="StackCoverJPG"
  [STACK_PAIRS]="1"
  [STACK_COVER]="display"
  [STACK_TOLERANCE]="2"
  [IMMICH_GO_VERSION]="0.32.0"
  [CHECK_UPDATES]="1"
)

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
  # Reste der alten Einrichtung mit drei Teilen
  rm -rf "$US_DIR/camera-import-setup" "$BASE_DIR/camera-import.conf.example" "$BASE_DIR/bin/README.md"
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

# ======================= Programmdateien =====================================
write_files() {
put "$BASE_DIR/lib.sh" <<'__CAMERA_IMPORT_EOF__'
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
__CAMERA_IMPORT_EOF__
put "$BASE_DIR/bot.sh" <<'__CAMERA_IMPORT_EOF__'
#!/bin/bash
# Telegram-Bot für camera-import: beantwortet Befehle und nimmt den
# Albumnamen entgegen. Läuft dauerhaft, gestartet von camera-import-install.

# shellcheck source=lib.sh
source "$(dirname "$0")/lib.sh"

HELP="Befehle:
/status – Fortschritt des laufenden Imports
/abbrechen – laufenden Import stoppen
/letzter – Zusammenfassung des letzten Imports
/log – letzte Zeilen des Protokolls
/stapeln – RAW+HIF/JPG-Paare der ganzen Bibliothek stapeln
/stapeln JJJJ-MM-TT – nur seit diesem Tag Hochgeladenes
/kein – beim Albumnamen: kein Album"

handle() {
  local text="$1" cmd
  cmd="${text%% *}"
  cmd="${cmd%%@*}"   # "/status@MeinBot" in Gruppen

  # Wartet ein Import auf den Albumnamen, ist jede Nicht-Befehl-Antwort der Name
  if [[ -e "$ALBUM_REQUEST" && "$cmd" != /* ]]; then
    printf '%s\n' "$text" > "$ALBUM_ANSWER"
    notify "📁 Album: $text"
    return
  fi

  case "$cmd" in
    /status)
      if import_running; then
        notify "$(status_text)"
      elif [[ -e "$ALBUM_REQUEST" ]]; then
        notify "⏳ Import wartet auf den Albumnamen."
      else
        notify "💤 Kein Import aktiv."
      fi
      ;;
    /abbrechen|/cancel)
      if import_running; then
        touch "$CANCEL_FILE"
        kill "$PID" 2>/dev/null
        notify "⏹️ Import wird abgebrochen …"
      else
        notify "💤 Kein Import aktiv."
      fi
      ;;
    /letzter)
      if [[ -r "$LAST_FILE" ]]; then
        notify "$(cat "$LAST_FILE")"
      else
        notify "Noch kein Import abgeschlossen."
      fi
      ;;
    /log)
      local file="$SCRIPT_LOG"
      import_running && file="$GOLOG"
      if [[ -r "$file" ]]; then
        notify "📄 $(basename "$file"):
$(tr '\r' '\n' < "$file" | grep -v '^Immich read' | tail -n 15 | cut -c1-250)"
      else
        notify "Kein Protokoll vorhanden."
      fi
      ;;
    /stapeln)
      local arg=""
      [[ "$text" == *" "* ]] && arg="${text#* }"
      if [[ -n "$arg" ]] && ! date -d "$arg" >/dev/null 2>&1; then
        notify "Unbekanntes Datum: $arg (Format JJJJ-MM-TT)"
        return
      fi
      notify "🗂️ Stapeln gestartet (${arg:-ganze Bibliothek}) …"
      setsid bash "$BASE_DIR/stack.sh" "$arg" 0 >> "$SCRIPT_LOG" 2>&1 < /dev/null &
      ;;
    /kein)
      if [[ -e "$ALBUM_REQUEST" ]]; then
        echo "-" > "$ALBUM_ANSWER"
        notify "📁 Kein Album."
      fi
      ;;
    /start|/hilfe|/help)
      notify "$HELP"
      ;;
  esac
}

main() {
  local offset uid chat text updates
  until load_config && [[ -n "$TG_BOT_TOKEN" ]]; do sleep 60; done

  curl -sS -m 15 "https://api.telegram.org/bot${TG_BOT_TOKEN}/setMyCommands" \
    --data-urlencode 'commands=[
      {"command":"status","description":"Fortschritt des laufenden Imports"},
      {"command":"abbrechen","description":"Laufenden Import stoppen"},
      {"command":"letzter","description":"Zusammenfassung des letzten Imports"},
      {"command":"log","description":"Letzte Zeilen des Protokolls"},
      {"command":"stapeln","description":"RAW+HIF/JPG-Paare stapeln (optional: ab Datum)"},
      {"command":"hilfe","description":"Alle Befehle"}]' >/dev/null 2>&1

  # Nachrichten von vor dem Start überspringen
  uid=$(tg_updates -1 | tail -n1 | cut -f1)
  offset=$(( ${uid:-0} + 1 ))
  log "Bot gestartet"

  while :; do
    load_config || { sleep 60; continue; }
    if ! updates=$(tg_updates "$offset" 50); then
      sleep 10   # Netzwerk- oder API-Fehler: nicht im Kreis drehen
      continue
    fi
    while IFS=$'\t' read -r uid chat text; do
      [[ -n "$uid" ]] || continue
      offset=$(( uid + 1 ))
      [[ "$chat" == "$TG_CHAT_ID" ]] && handle "$text"
    done <<< "$updates"
  done
}

main
__CAMERA_IMPORT_EOF__
put "$BASE_DIR/stack.sh" <<'__CAMERA_IMPORT_EOF__'
#!/bin/bash
# Stapelt RAW+Bild-Paare in Immich (stack.php) und meldet das Ergebnis.
#
#   stack.sh                  ganze Bibliothek
#   stack.sh 2026-10-11       alles, was seit diesem Tag hochgeladen wurde
#   stack.sh @1791738534 3    seit Zeitpunkt (Unix-Zeit), bis zu 3 neue Versuche
#
# Fehlt Immich bei manchen Bildern noch das Aufnahmedatum (Metadaten-Job läuft
# noch), wird der Lauf nach 30 Minuten wiederholt, solange Versuche übrig sind.

# shellcheck source=lib.sh
source "$(dirname "$0")/lib.sh"
load_config || exit 1

since="${1:-}"
retries="${2:-0}"

exec 7>"$STATE_DIR/stapeln.lock"
if ! flock -n 7; then
  notify "🗂️ Stapeln läuft bereits."
  exit 0
fi

scope="ganze Bibliothek"
if [[ "$since" == @* ]]; then
  scope="seit $(date -d "$since" '+%F %H:%M')"
elif [[ -n "$since" ]]; then
  scope="seit $since"
fi

args=()
[[ -n "$since" ]] && args+=(--since="$since")
(( DRY_RUN )) && args+=(--dry-run)

log "Stapeln ($scope) gestartet"
out=$(php "$BASE_DIR/stack.php" "${args[@]}" 2>&1)
while IFS= read -r line; do echo "  stapeln | $line"; done <<< "$out"

result=$(grep '^ERGEBNIS' <<< "$out" | tail -n1)
val() { sed -nE "s/.*$1=([0-9]+).*/\1/p" <<< "$result"; }
pairs=$(val paare) created=$(val angelegt) ambiguous=$(val mehrdeutig)
nodate=$(val ohnedatum) errors=$(val fehler)

if [[ -z "$result" ]]; then
  notify "❌ Stapeln ($scope) fehlgeschlagen:
$(tail -n 5 <<< "$out" | cut -c1-300)"
  exit 1
fi

msg="🗂️ Stapeln ($scope)${MODE:-}: $created von $pairs Paaren gestapelt"
(( DRY_RUN )) && msg="🗂️ Stapeln ($scope, Probelauf): $pairs Paare gefunden"
(( ambiguous )) && msg+=$'\n'"$ambiguous mehrdeutig, übersprungen (z.B. HIF+JPG+ARW mit gleichem Namen)"
if (( errors )); then
  msg+=$'\n'"❌ $errors Fehler:"$'\n'"$(grep -v '^ERGEBNIS' <<< "$out" | grep -iE 'fehlgeschlagen|HTTP' | tail -n 3 | cut -c1-300)"
fi
if (( nodate )); then
  if (( retries > 0 )) && command -v at >/dev/null 2>&1; then
    echo "bash $BASE_DIR/stack.sh '$since' $((retries - 1)) >> $SCRIPT_LOG 2>&1" | at now + 30 minutes 2>/dev/null
    msg+=$'\n'"$nodate Dateinamen noch ohne Aufnahmedatum (Immich liest Metadaten noch) – neuer Versuch in 30 Min."
  else
    msg+=$'\n'"$nodate Dateinamen ohne Aufnahmedatum – später mit /stapeln erneut versuchen."
  fi
fi
log "${msg%%$'\n'*}"
notify "$msg"
__CAMERA_IMPORT_EOF__
put "$BASE_DIR/stack.php" <<'__CAMERA_IMPORT_EOF__'
<?php
// Stapelt zusammengehörige RAW- und Bild-Dateien in Immich (z.B. DSC01234.ARW
// + DSC01234.HIF), auch für früher hochgeladene Bilder.
//
// Ein Paar entsteht nur, wenn
//   - der Dateiname ohne Endung gleich ist,
//   - die Aufnahmezeiten höchstens STACK_TOLERANCE Sekunden auseinanderliegen,
//   - das Kameramodell gleich ist (wenn beide eins haben),
//   - die Zuordnung eindeutig ist (genau ein Partner auf beiden Seiten),
//   - keine der beiden Dateien schon in einem Stapel liegt.
//
// Aufruf:
//   php stack.php [--since=JJJJ-MM-TT|ISO-Zeit] [--dry-run] [--conf=PFAD]
// Ohne --since wird die ganze Bibliothek durchsucht.
// Letzte Ausgabezeile: "ERGEBNIS paare=N angelegt=N mehrdeutig=N ohnedatum=N fehler=N"

const RAW_EXT = ['arw', 'cr2', 'cr3', 'nef', 'nrw', 'raf', 'dng', 'orf', 'rw2',
                 'pef', 'srw', '3fr', 'iiq', 'erf', 'srf', 'sr2'];
const DISPLAY_EXT = ['hif', 'heif', 'heic', 'jpg', 'jpeg'];

$opt = getopt('', ['since::', 'dry-run', 'conf::']);
$conf = read_conf($opt['conf'] ?? '/boot/config/camera-import/camera-import.conf');
$url = rtrim($conf['IMMICH_URL'] ?? '', '/');
$key = $conf['IMMICH_API_KEY'] ?? '';
$tolerance = (int)($conf['STACK_TOLERANCE'] ?? 2);
$cover = $conf['STACK_COVER'] ?? 'display';
$dry = isset($opt['dry-run']);
if ($url === '' || $key === '') {
    fwrite(STDERR, "IMMICH_URL oder IMMICH_API_KEY fehlt\n");
    exit(2);
}

// Einstellungen im Shell-Format (KEY="wert") lesen
function read_conf(string $path): array {
    $out = [];
    foreach (@file($path) ?: [] as $line) {
        if (preg_match('/^\s*([A-Z_]+)=(["\']?)(.*)\2\s*(#.*)?$/', $line, $m)) {
            $out[$m[1]] = $m[3];
        }
    }
    return $out;
}

function api(string $method, string $path, array $body): array {
    global $url, $key;
    $ctx = stream_context_create(['http' => [
        'method' => $method,
        'header' => "x-api-key: $key\r\nContent-Type: application/json\r\nAccept: application/json\r\n",
        'content' => json_encode($body),
        'timeout' => 120,
        'ignore_errors' => true,
    ]]);
    $raw = @file_get_contents($url . '/api' . $path, false, $ctx);
    $status = 0;
    foreach ($http_response_header ?? [] as $h) {
        if (preg_match('#^HTTP/\S+\s+(\d+)#', $h, $m)) $status = (int)$m[1];
    }
    return [$status, json_decode($raw ?: 'null', true)];
}

function ext_of(string $name): string {
    return strtolower(pathinfo($name, PATHINFO_EXTENSION));
}

// Nur das von Immich selbst ausgelesene Aufnahmedatum: das Dateidatum weicht bei
// manchen Formaten ab, und vor dem Metadaten-Job von Immich fehlt es noch.
function taken_at(array $a): ?int {
    $t = $a['exifInfo']['dateTimeOriginal'] ?? null;
    return $t ? strtotime($t) : null;
}

function camera(array $a): string {
    return trim(($a['exifInfo']['make'] ?? '') . ' ' . ($a['exifInfo']['model'] ?? ''));
}

// --- Bilder aus Immich holen ------------------------------------------------
$query = ['size' => 1000, 'withExif' => true, 'withStacked' => true];
if (!empty($opt['since'])) {
    $ts = strtotime($opt['since']);
    if ($ts === false) { fwrite(STDERR, "Ungültiges Datum: {$opt['since']}\n"); exit(2); }
    $query['createdAfter'] = gmdate('Y-m-d\TH:i:s.000\Z', $ts);
}

$groups = [];
$seen = 0;
$page = 1;
while (true) {
    [$status, $res] = api('POST', '/search/metadata', $query);
    if ($status !== 200 || !isset($res['assets']['items'])) {
        fwrite(STDERR, "Suche fehlgeschlagen (HTTP $status): " . json_encode($res) . "\n");
        echo "ERGEBNIS paare=0 angelegt=0 mehrdeutig=0 ohnedatum=0 fehler=1\n";
        exit(1);
    }
    foreach ($res['assets']['items'] as $a) {
        $seen++;
        if (($a['type'] ?? '') !== 'IMAGE' || !empty($a['isTrashed'])) continue;
        $ext = ext_of($a['originalFileName'] ?? '');
        if (!in_array($ext, RAW_EXT, true) && !in_array($ext, DISPLAY_EXT, true)) continue;
        $base = strtolower(pathinfo($a['originalFileName'], PATHINFO_FILENAME));
        $groups[$base][] = [
            'id' => $a['id'],
            'name' => $a['originalFileName'],
            'raw' => in_array($ext, RAW_EXT, true),
            't' => taken_at($a),
            'cam' => camera($a),
            'stacked' => !empty($a['stack']),
        ];
    }
    // Ab Immich 3.2 per Cursor, davor per Seitenzahl
    if (!empty($res['assets']['nextCursor'])) {
        $query['cursor'] = $res['assets']['nextCursor'];
    } elseif (!empty($res['assets']['nextPage'])) {
        $query['page'] = (int)$res['assets']['nextPage'];
    } else {
        break;
    }
    if (++$page % 10 === 0) echo "… $seen Dateien gelesen\n";
}
echo "$seen Dateien gelesen, " . count($groups) . " Dateinamen\n";

// --- Paare bilden -----------------------------------------------------------
function matches(array $raw, array $img, int $tol): bool {
    if ($raw['t'] === null || $img['t'] === null) return false;
    if (abs($raw['t'] - $img['t']) > $tol) return false;
    if ($raw['cam'] !== '' && $img['cam'] !== '' && $raw['cam'] !== $img['cam']) return false;
    return true;
}

$pairs = [];
$ambiguous = 0;
$nodate = 0;
foreach ($groups as $files) {
    $raws = array_values(array_filter($files, fn($f) => $f['raw']));
    $imgs = array_values(array_filter($files, fn($f) => !$f['raw']));
    if (!$raws || !$imgs) continue;
    // Ohne Aufnahmedatum lässt sich nicht sicher paaren; später erneut versuchen
    $undated = array_filter($files, fn($f) => $f['t'] === null && !$f['stacked']);
    if ($undated) { $nodate++; continue; }
    foreach ($raws as $r) {
        $cand = array_values(array_filter($imgs, fn($i) => matches($r, $i, $tolerance)));
        if (count($cand) !== 1) {
            if (count($cand) > 1) $ambiguous++;
            continue;
        }
        $i = $cand[0];
        // Gegenprobe: das Bild darf nur zu diesem einen RAW passen
        $back = array_filter($raws, fn($x) => matches($x, $i, $tolerance));
        if (count($back) !== 1) { $ambiguous++; continue; }
        if ($r['stacked'] || $i['stacked']) continue;
        $pairs[] = $cover === 'raw' ? [$r, $i] : [$i, $r];
    }
}

// --- Stapel anlegen ---------------------------------------------------------
$created = 0;
$errors = 0;
foreach ($pairs as [$primary, $other]) {
    if ($dry) {
        echo "würde stapeln: {$primary['name']} + {$other['name']}\n";
        continue;
    }
    [$status, $res] = api('POST', '/stacks', ['assetIds' => [$primary['id'], $other['id']]]);
    if ($status >= 200 && $status < 300) {
        $created++;
    } else {
        $errors++;
        if ($errors <= 5) {
            fwrite(STDERR, "Stapel {$primary['name']} fehlgeschlagen (HTTP $status): " . json_encode($res) . "\n");
        }
    }
}

printf("ERGEBNIS paare=%d angelegt=%d mehrdeutig=%d ohnedatum=%d fehler=%d\n",
       count($pairs), $created, $ambiguous, $nodate, $errors);
exit($errors ? 1 : 0);
__CAMERA_IMPORT_EOF__
put "$IMPORT_DIR/script" <<'__CAMERA_IMPORT_EOF__'
#!/bin/bash
# Importiert Fotos/Videos von SD-Karte oder Kamera (USB-Massenspeicher) nach Immich
# und meldet das Ergebnis per Telegram.
# Wird von udev mit dem Partitionsnamen (z.B. sdb1) aufgerufen; ohne Argument
# werden alle angeschlossenen USB-Partitionen mit DCIM-Ordner importiert.
#   script stapeln [JJJJ-MM-TT]   nur RAW+HIF/JPG-Paare stapeln, kein Import
# Wird von camera-import-install angelegt und bei jedem Lauf überschrieben.
# Einstellungen: oben im Skript camera-import-install

# shellcheck source=../../boot/camera-import/lib.sh
source /boot/config/camera-import/lib.sh

MOUNT_BASE="/mnt/camera-import"
LOCK_FILE="/var/run/camera-import.lock"
IMMICH_GO_LOG_DIR="/tmp/user.scripts/tmpScripts/camera-import"

is_usb_partition() {
  udevadm info --query=property --name="/dev/$1" 2>/dev/null | grep -q '^ID_BUS=usb$'
}

# Wert aus dem immich-go-Bericht lesen, z.B. "  uploaded successfully   :      12  (3.1 GB)"
report_value() {
  local v
  v=$(grep -E "^\s*$1\s*:" "$2" | tail -n1 | sed -E 's/.*:\s*([0-9]+).*/\1/')
  echo "${v:-0}"
}

immich_reachable() {
  curl -fsS -m 10 "$IMMICH_URL/api/server/ping" 2>/dev/null | grep -q pong
}

# Albumnamen nach ALBUM_MODE bestimmen; im Modus "ask" per Telegram fragen
choose_album() {
  local label="$1" count="$2" default answer waited=0
  default="Import $(date +%F)"
  case "$ALBUM_MODE" in
    fixed) echo "$IMMICH_ALBUM" ;;
    date)  echo "$default" ;;
    ask)
      rm -f "$ALBUM_ANSWER"
      touch "$ALBUM_REQUEST"
      notify "📷 Karte erkannt: $count Dateien von $label${MODE}
In welches Album? Antworte mit dem Namen oder /kein.
Ohne Antwort in $((ALBUM_ASK_TIMEOUT / 60)) Min: \"$default\"" >&2  # stdout ist der Albumname
      while (( waited < ALBUM_ASK_TIMEOUT )) && [[ ! -s "$ALBUM_ANSWER" ]]; do
        sleep 2; (( waited += 2 ))
      done
      answer=$(head -n1 "$ALBUM_ANSWER" 2>/dev/null)
      rm -f "$ALBUM_REQUEST" "$ALBUM_ANSWER"
      case "$answer" in
        "") echo "$default" ;;
        -)  echo "" ;;
        *)  echo "$answer" ;;
      esac
      ;;
    *) echo "" ;;
  esac
}

# Meldet Zwischenstände bei den Prozentmarken aus PROGRESS_STEPS.
# Überspringt der Fortschritt mehrere Marken auf einmal, kommt nur die höchste.
progress_watch() {
  local steps=() hit
  read -r -a steps <<< "$PROGRESS_STEPS"
  while kill -0 "$PID" 2>/dev/null && (( ${#steps[@]} )); do
    sleep 30
    progress
    hit=""
    while (( ${#steps[@]} )) && (( PROG_PCT >= steps[0] )); do
      hit=${steps[0]}
      steps=("${steps[@]:1}")
    done
    [[ -n "$hit" ]] && notify "⏳ $hit % geschafft ($LABEL): $PROG_DONE von $TOTAL Dateien, Laufzeit $(fmt_dur $(( $(date +%s) - START )))"
  done
}

import_partition() {
  local kname="$1" dev="/dev/$1" mnt mounted_by_us=0 label
  [[ -b "$dev" ]] || { log "$dev existiert nicht"; return 0; }

  mnt=$(findmnt -nro TARGET --source "$dev" | head -n1)
  if [[ -z "$mnt" ]]; then
    mnt="$MOUNT_BASE/$kname"
    mkdir -p "$mnt"
    if ! mount -o ro "$dev" "$mnt"; then
      log "$dev konnte nicht eingehängt werden"
      notify "❌ Karte/Kamera an $dev konnte nicht eingehängt werden. Beschädigt oder unbekanntes Dateisystem?"
      rmdir "$mnt" 2>/dev/null
      return 0
    fi
    mounted_by_us=1
  fi

  # Aushängen: selbst eingehängt → umount; von Unassigned Devices → über UD
  release_card() {
    if (( mounted_by_us )); then
      umount "$mnt" && rmdir "$mnt" 2>/dev/null
      return 0
    elif (( AUTO_UNMOUNT )) && [[ -x /usr/local/sbin/rc.unassigned ]]; then
      /usr/local/sbin/rc.unassigned umount "$dev" >/dev/null 2>&1 && return 0
    fi
    return 1
  }

  if [[ ! -d "$mnt/DCIM" ]]; then
    log "$dev: kein DCIM-Ordner, wird ignoriert"
    (( mounted_by_us )) && release_card
    return 0
  fi

  label=$(lsblk -no LABEL "$dev" 2>/dev/null)
  label="${label:-$kname}"

  local paths=("$mnt/DCIM")
  [[ -d "$mnt/PRIVATE/M4ROOT/CLIP" ]] && paths+=("$mnt/PRIVATE/M4ROOT/CLIP")

  local count
  count=$(find "${paths[@]}" -type f \( -iname '*.jpg' -o -iname '*.arw' -o -iname '*.hif' \
    -o -iname '*.heif' -o -iname '*.mp4' -o -iname '*.mts' \) | wc -l)
  log "$dev ($label): $count Dateien gefunden in ${paths[*]}"

  if ! immich_reachable; then
    log "Immich unter $IMMICH_URL nicht erreichbar"
    notify "❌ Immich nicht erreichbar ($IMMICH_URL). Import von $label nicht gestartet."
    (( mounted_by_us )) && release_card
    return 0
  fi

  local album
  album=$(choose_album "$label" "$count")
  log "Album: ${album:-keins}"

  local args=(upload from-folder
    --server="$IMMICH_URL" --api-key="$IMMICH_API_KEY"
    --no-ui --pause-immich-jobs=false --on-errors=continue
    --manage-raw-jpeg="$RAW_JPEG")
  [[ -n "$album" ]] && args+=(--into-album="$album")
  (( DRY_RUN )) && args+=(--dry-run)

  local out golog start rc pid
  mkdir -p "$IMMICH_GO_LOG_DIR"
  golog="$IMMICH_GO_LOG_DIR/immich-go_$(date +%F_%H-%M-%S).log"
  args+=(--log-file="$golog")
  out=$(mktemp)
  start=$(date +%s)
  rm -f "$CANCEL_FILE"

  notify "📷 Import gestartet${MODE}: $count Dateien von $label${album:+ → Album \"$album\"}
Befehle: /status /abbrechen /log"

  # immich-go im Hintergrund starten und sein Log live ins Skript-Log spiegeln;
  # tail endet von selbst, sobald immich-go fertig ist.
  "$IMMICH_GO" "${args[@]}" "${paths[@]}" > "$out" 2>&1 &
  pid=$!
  # Status für bot.sh und progress_watch
  PID=$pid OUT=$out GOLOG=$golog LABEL=$label TOTAL=$count START=$start
  printf 'PID=%q\nOUT=%q\nGOLOG=%q\nLABEL=%q\nTOTAL=%q\nSTART=%q\nMODE=%q\n' \
    "$PID" "$OUT" "$GOLOG" "$LABEL" "$TOTAL" "$START" "$MODE" > "$CURRENT_FILE"
  progress_watch &
  local wpid=$!

  tail -n +1 -F --pid="$pid" "$golog" 2>/dev/null | sed -u 's/^/  immich-go | /'
  wait "$pid"
  rc=$?
  kill "$wpid" 2>/dev/null; wait "$wpid" 2>/dev/null
  rm -f "$CURRENT_FILE"

  # Zusammenfassung (Standardausgabe) anhängen; die mit \r überschriebene
  # Fortschrittsanzeige auf ihren letzten Stand kürzen
  tr '\r' '\n' < "$out" > "$out.clean"
  mv "$out.clean" "$out"
  { grep -v '^Immich read' "$out"; grep '^Immich read' "$out" | tail -n1; } \
    | sed 's/^/  immich-go | /'
  local dur_txt
  dur_txt=$(fmt_dur $(( $(date +%s) - start )))

  local up dup err pend
  up=$(report_value "uploaded successfully" "$out")
  dup=$(report_value "server has duplicate" "$out")
  err=$(( $(report_value "upload failed" "$out") + $(report_value "server error" "$out") \
        + $(report_value "file access error" "$out") ))
  pend=$(report_value "Pending" "$out")
  (( DRY_RUN )) && pend=0   # im Probelauf bleiben gestapelte Dateien immer "pending"

  local removable=""
  release_card && removable=$'\nKarte ausgehängt, kann entfernt werden.'

  local msg
  if [[ -e "$CANCEL_FILE" ]]; then
    msg="⏹️ Import abgebrochen ($label): $up neu, $dup schon vorhanden, nach $dur_txt"
    rm -f "$CANCEL_FILE"
  elif (( rc == 0 && err == 0 && pend == 0 )); then
    msg="✅ Import fertig${MODE} ($label): $up neu, $dup schon vorhanden, Dauer $dur_txt"
  elif (( rc == 0 && err == 0 )); then
    msg="⚠️ Import unvollständig ($label): $up neu, $dup schon vorhanden, $pend nicht abgeschlossen – Karte bitte erneut einstecken"
  else
    msg="❌ Import fehlgeschlagen ($label): Exit $rc, $up neu, $dup vorhanden, $err Fehler
$(grep -E ' (ERR|WRN) ' "$golog" 2>/dev/null | tail -n 5 | cut -c1-300)"
  fi
  [[ -n "$album" ]] && msg+=$'\n'"Album: $album"
  log "${msg%%$'\n'*}"
  notify "$msg$removable"
  printf '%s\n%s\n' "$(date '+%F %T')" "$msg" > "$LAST_FILE"
  rm -f "$out"

  # RAW+HIF-Paare stapeln, die immich-go nicht stapelt; bei fehlenden
  # Metadaten bis zu dreimal nach je 30 Minuten erneut
  if (( STACK_PAIRS && ! DRY_RUN && up > 0 )); then
    bash "$BASE_DIR/stack.sh" "@$start" 3
  fi
}

# ===== Hauptprogramm =========================================================
load_config || exit 1

if [[ "${1:-}" == "stapeln" ]]; then
  exec bash "$BASE_DIR/stack.sh" "${2:-}" 0
fi

MODE=""
(( DRY_RUN )) && MODE=" (Probelauf)"

if [[ ! -x "$IMMICH_GO" ]]; then
  log "immich-go fehlt – erst camera-import-install ausführen"
  notify "❌ Karte erkannt, aber immich-go fehlt. Bitte camera-import-install ausführen."
  exit 1
fi

# Pro Gerät nur ein Aufruf: weitere Auslöser für dieselbe Partition entfallen
exec 8>"${LOCK_FILE%.lock}-${1:-all}.lock"
if ! flock -n 8; then
  log "Import für ${1:-alle} läuft bereits oder wartet schon – übersprungen"
  exit 0
fi
# Verschiedene Karten (z.B. beide Kamera-Schächte) nacheinander importieren
exec 9>"$LOCK_FILE"
flock 9

udevadm settle --timeout=30
sleep 2

if [[ -n "${1:-}" ]]; then
  is_usb_partition "$1" && import_partition "$1"
else
  # Manueller Start: alle USB-Partitionen durchsuchen
  while read -r kname type; do
    [[ "$type" == "part" ]] || continue
    is_usb_partition "$kname" || continue
    [[ "$(lsblk -no LABEL "/dev/$kname")" == "UNRAID" ]] && continue
    import_partition "$kname"
  done < <(lsblk -nr -o KNAME,TYPE)
fi

check_immich_go_update
__CAMERA_IMPORT_EOF__
}

main "$@"
