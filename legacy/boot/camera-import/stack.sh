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

echo $$ > "$STACK_PID"
trap 'rm -f "$STACK_PID" "$STACK_PROGRESS"' EXIT

scope="ganze Bibliothek"
if [[ "$since" == @* ]]; then
  scope="seit $(date -d "$since" '+%F %H:%M')"
elif [[ -n "$since" ]]; then
  scope="seit $since"
fi

args=(--status="$STACK_PROGRESS")
[[ -n "$since" ]] && args+=(--since="$since")
(( DRY_RUN )) && args+=(--dry-run)

log "Stapeln ($scope) gestartet"
# Ausgabe sofort ins Log, damit /log den Fortschritt zeigt
tmp=$(mktemp)
php "$BASE_DIR/stack.php" "${args[@]}" 2>&1 | tee "$tmp" \
  | while IFS= read -r line; do echo "  stapeln | $line"; done
out=$(cat "$tmp")
rm -f "$tmp"

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
