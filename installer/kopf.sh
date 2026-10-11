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
