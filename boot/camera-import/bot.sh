#!/bin/bash
# Telegram-Bot für camera-import: beantwortet Befehle und nimmt den
# Albumnamen entgegen. Läuft dauerhaft, gestartet von camera-import-setup.

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
