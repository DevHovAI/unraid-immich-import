# unraid-immich-import

Steckst du eine SD-Karte (über einen USB-Kartenleser) oder eine Kamera an den Unraid-Server, werden die Fotos und Videos automatisch mit [immich-go](https://github.com/simulot/immich-go) nach [Immich](https://immich.app) hochgeladen. Ein Telegram-Bot meldet den Fortschritt und das Ergebnis.

- Läuft über das Unraid-Plugin **User Scripts**. Ausgelöst wird es beim Einstecken über eine udev-Regel.
- RAW+JPG werden gestapelt. Was schon in Immich liegt, wird übersprungen.
- Die Dateien bleiben auf der Karte. Die Karte wird nur lesend eingehängt.
- Getestet mit einer Sony-Kamera. Die Karte muss einen `DCIM`-Ordner haben, dann geht jede Kamera. Bei Sony werden zusätzlich die Videos aus `PRIVATE/M4ROOT/CLIP` importiert.

## Telegram

| Nachricht / Befehl | Bedeutung |
|---|---|
| 📷 Karte erkannt … In welches Album? | Antworte mit einem Namen oder `/kein`. Ohne Antwort wird nach 2 Minuten ein Album „Import JJJJ-MM-TT“ angelegt. Das Verhalten stellst du mit `ALBUM_MODE` ein. |
| 📷 Import gestartet | Anzahl der Dateien und Album |
| ⏳ 25 / 50 / 75 % geschafft | Zwischenstände (`PROGRESS_STEPS`) |
| ✅ / ⚠️ / ❌ / ⏹️ | fertig / unvollständig / Fehler / abgebrochen. Hat das Skript die Karte selbst ausgehängt, kommt „kann entfernt werden“ dazu. |
| ❌ Immich nicht erreichbar | Immich war vor dem Start nicht erreichbar. Der Import wird gar nicht erst begonnen. |
| ❌ … konnte nicht eingehängt werden | Die Karte ist beschädigt oder hat ein unbekanntes Dateisystem. |
| ℹ️ immich-go vX ist erschienen | Meldet eine neue Version einmal. Aktualisiert wird nur von Hand. |
| `/status` | Fortschritt des laufenden Imports |
| `/abbrechen` | Laufenden Import stoppen |
| `/letzter` | Zusammenfassung des letzten Imports |
| `/log` | Letzte Zeilen des Protokolls |
| `/hilfe` | Alle Befehle |

Der Bot reagiert nur auf Nachrichten aus dem eingetragenen Chat (`TG_CHAT_ID`).

## Aufbau

```
boot/camera-import/          →  /boot/config/camera-import/
  camera-import.conf.example     Vorlage für die Einstellungen
  lib.sh                         gemeinsame Funktionen
  bot.sh                         Telegram-Bot (läuft dauerhaft)
  bin/immich-go                  Binary, nicht im Repo (siehe bin/README.md)
user.scripts/                →  /boot/config/plugins/user.scripts/scripts/
  camera-import/                 der Import ("Schedule Disabled")
  camera-import-setup/           udev-Regel, immich-go, Bot ("At Startup of Array")
```

Ablauf: Einstecken → udev → `camera-import-trigger` → `at now` → `camera-import/script sdX1` → einhängen → `DCIM`-Ordner vorhanden? → Immich erreichbar? → nach dem Album fragen → immich-go → Telegram → aushängen.

Das Root-Dateisystem von Unraid liegt im RAM. `camera-import-setup` legt deshalb bei jedem Array-Start die udev-Regel, den Trigger-Wrapper und immich-go neu an und startet den Bot.

## Einrichtung

1. Das Plugin **User Scripts** installieren (Apps → „User Scripts“).
2. Die Dateien kopieren:
   ```bash
   scp -r boot/camera-import root@tower:/boot/config/
   scp -r user.scripts/* root@tower:/boot/config/plugins/user.scripts/scripts/
   ```
3. immich-go nach `/boot/config/camera-import/bin/` legen, siehe [bin/README.md](boot/camera-import/bin/README.md).
4. Die Einstellungen anlegen:
   ```bash
   cd /boot/config/camera-import
   cp camera-import.conf.example camera-import.conf
   nano camera-import.conf
   ```
5. **Immich-API-Key**: Immich → Kontoeinstellungen → API-Schlüssel → Neuer Schlüssel. Leg ihn mit dem Konto an, in dessen Bibliothek die Bilder landen sollen. Er braucht diese Rechte:
   `asset.read`, `asset.statistics`, `asset.update`, `asset.upload`, `asset.copy`, `asset.delete`, `asset.download`, `album.create`, `album.read`, `albumAsset.create`, `server.about`, `stack.create`, `tag.asset`, `tag.create`, `user.read`.
   `job.*` und einen Admin-Schlüssel braucht es nicht, weil das Skript `--pause-immich-jobs=false` setzt.
6. **Telegram-Bot**:
   - Bei `@BotFather` mit `/newbot` einen Bot anlegen und den Token notieren.
   - Den Bot über den Link `t.me/<benutzername>` öffnen. Neue Bots findet die Suche oft nicht.
   - **Start** tippen und eine Nachricht schicken.
   - `curl -s "https://api.telegram.org/bot<TOKEN>/getUpdates"` aufrufen. Die Zahl bei `"chat":{"id":…}` ist `TG_CHAT_ID`. Das ist nicht die Zahl vor dem Doppelpunkt im Token, das wäre die ID des Bots selbst.
7. Die Zeitpläne in User Scripts setzen: `camera-import-setup` auf **At Startup of Array** und einmal **Run Script** klicken. `camera-import` auf **Schedule Disabled**.
8. Für den ersten Test `DRY_RUN=1` setzen. Dann wird nichts hochgeladen.

## Kamera direkt anschließen (Sony)

Im Kameramenü die USB-Verbindung auf **Massenspeicher (MSC)** stellen. Steht dort „Wählen bei Verbindung“, wählst du beim Anstecken „Massenspeicher“. Im MTP-Modus erscheint die Kamera nicht als Laufwerk.

## Logs und Fehlersuche

- Import-Log: Settings → User Scripts → `camera-import` → **Show Log**. Die Ausgabe von immich-go steht dort mit dem Präfix `immich-go |`.
- Ausführliches Protokoll von immich-go: `/tmp/user.scripts/tmpScripts/camera-import/immich-go_*.log`
- Bot-Log: `/tmp/camera-import/bot.log`
- Letzter Import: `/boot/config/camera-import/letzter-import.txt`
- Ist die udev-Regel aktiv? `cat /etc/udev/rules.d/99-camera-import.rules`
- Was meldet udev zur Karte? `udevadm info --query=property --name=/dev/sdX1 | grep -E 'ID_BUS|ID_FS'`
- Import von Hand für eine Partition: `bash /boot/config/plugins/user.scripts/scripts/camera-import/script sdX1`

Ändere die Skripte nicht, solange ein Import läuft. Bash liest ein Skript während der Ausführung nach, und ein laufender Import kann dabei durcheinanderkommen.

## Entwicklung

```bash
just check     # bash -n und shellcheck für alle Skripte
```

## Lizenz

MIT, siehe [LICENSE](LICENSE).
