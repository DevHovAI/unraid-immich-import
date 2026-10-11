#!/bin/bash
# Baut camera-import-install.sh: Einstellungen (installer/kopf.sh),
# Installationslogik (installer/rumpf.sh) und alle Programmdateien in einem
# Skript, das man in User Scripts einfügt.
set -euo pipefail
cd "$(dirname "$0")"

OUT="camera-import-install.sh"
MARK="__CAMERA_IMPORT_EOF__"
# Ziel (relativ zu den Variablen im Installer) und Quelle
FILES=(
  '$BASE_DIR/lib.sh'          boot/camera-import/lib.sh
  '$BASE_DIR/bot.sh'          boot/camera-import/bot.sh
  '$BASE_DIR/stack.sh'        boot/camera-import/stack.sh
  '$BASE_DIR/stack.php'       boot/camera-import/stack.php
  '$IMPORT_DIR/script'        user.scripts/camera-import/script
)

{
  cat installer/kopf.sh
  echo
  # Vorgabewerte der Vorlage, um Updates von eigenen Änderungen zu unterscheiden
  printf 'declare -A VORLAGE_WERTE=(\n'
  sed -n '/^# =* EINSTELLUNGEN/,/^# =* ENDE DER EINSTELLUNGEN/p' installer/kopf.sh \
    | grep -E '^[A-Z_]+="' | grep -v '^VORLAGE=' \
    | sed -E 's/^([A-Z_]+)=(".*")$/  [\1]=\2/'
  printf ')\n'
  cat installer/rumpf.sh
  echo
  echo '# ======================= Programmdateien ====================================='
  echo 'write_files() {'
  for ((i = 0; i < ${#FILES[@]}; i += 2)); do
    src="${FILES[i+1]}"
    if grep -q "$MARK" "$src"; then echo "$src enthält $MARK" >&2; exit 1; fi
    printf 'put "%s" <<'"'"'%s'"'"'\n' "${FILES[i]}" "$MARK"
    cat "$src"
    [[ -z "$(tail -c1 "$src")" ]] || echo
    echo "$MARK"
  done
  echo '}'
  echo
  echo 'main "$@"'
} > "$OUT"
chmod +x "$OUT"
echo "$OUT gebaut ($(wc -l < "$OUT") Zeilen)"
