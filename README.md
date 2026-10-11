# unraid-immich-import

Steckst du eine SD-Karte (über einen USB-Kartenleser) oder eine Kamera an den Unraid-Server, werden die Fotos und Videos automatisch mit [immich-go](https://github.com/simulot/immich-go) nach [Immich](https://immich.app) hochgeladen. Ein Telegram-Bot meldet den Fortschritt und das Ergebnis.

- Ein einziges Skript für das Unraid-Plugin **User Scripts**: einfügen, Einstellungen ausfüllen, ausführen. Ausgelöst wird der Import beim Einstecken über eine udev-Regel.
- RAW+HIF und RAW+JPG werden gestapelt, auch für früher hochgeladene Bilder (`/stapeln`). Was schon in Immich liegt, wird übersprungen.
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
| `/status` | Fortschritt von Import und Stapeln |
| `/abbrechen` | Laufenden Import stoppen |
| `/letzter` | Zusammenfassung des letzten Imports |
| `/log` | Letzte Zeilen des Protokolls |
| `/stapeln` | RAW+HIF/JPG-Paare der ganzen Bibliothek stapeln |
| `/stapeln JJJJ-MM-TT` | nur, was seit diesem Tag hochgeladen wurde |
| 🗂️ Stapeln … | Ergebnis des Stapel-Laufs nach jedem Import |
| `/update` | Neue Version installieren. Der Bot meldet einmal am Tag, wenn es eine gibt. |
| `/hilfe` | Alle Befehle |

Der Bot reagiert nur auf Nachrichten aus dem eingetragenen Chat (`TG_CHAT_ID`).

## Stapeln

immich-go stapelt nur RAW+JPG. Sonys HIF+ARW-Paare stapelt es nicht. Deshalb folgt nach jedem Import ein eigener Schritt (`stack.php`), der direkt mit der Schnittstelle von Immich arbeitet. Zwei Dateien kommen nur dann in einen Stapel, wenn alles davon zutrifft:

- Der Dateiname ohne Endung ist gleich, zum Beispiel `DSC01234.ARW` und `DSC01234.HIF`.
- Die Aufnahmezeiten liegen höchstens `STACK_TOLERANCE` Sekunden auseinander (Standard 2).
- Das Kameramodell ist gleich, sofern beide Dateien eines haben.
- Die Zuordnung ist eindeutig. Gibt es zum Beispiel HIF, JPG und ARW mit demselben Namen, wird nichts gestapelt.
- Keine der beiden Dateien liegt schon in einem Stapel.

Vorne liegt standardmäßig das HIF bzw. JPG (`STACK_COVER="display"`). Mit `raw` liegt das RAW vorne.

Das Aufnahmedatum liest Immich erst im Metadaten-Job aus, also kurz nach dem Upload. Fehlt es noch, versucht das Skript es nach 30 Minuten erneut, höchstens dreimal. Danach geht es von Hand mit `/stapeln`.

Von Hand in der Konsole (Ergebnis kommt zusätzlich per Telegram):
```bash
S=/boot/config/plugins/user.scripts/scripts/camera-import/script
bash $S stapeln              # ganze Bibliothek
bash $S stapeln 2026-10-11   # nur seit diesem Tag Hochgeladenes
php /boot/config/camera-import/stack.php --dry-run   # nur anzeigen, nichts ändern
```

## Einrichtung

Voraussetzung ist das Plugin **User Scripts** (Apps → „User Scripts“). Alles Weitere passiert in der Unraid-Oberfläche:

1. Settings → User Scripts → **Add New Script**, Name `camera-import-install`.
2. Zahnrad → **Edit Script**. Den kompletten Inhalt von `camera-import-install.sh` aus dem [neuesten Release](https://github.com/DevHovAI/unraid-immich-import/releases/latest) hineinkopieren.
3. Oben im Skript die Einstellungen ausfüllen: Immich-Adresse, API-Key, Telegram-Token und Chat-ID. **Save Changes**.
4. Den Zeitplan auf **At Startup of Array** stellen und auf **Run Script** klicken.

Das Skript lädt immich-go herunter, legt das User Script `camera-import` für den eigentlichen Import an, richtet den Auslöser beim Einstecken ein und startet den Telegram-Bot. Am Ende prüft es Immich und den API-Key und schickt eine Testnachricht. Was nicht stimmt, steht im Log mit ⚠.

**Einstellung ändern**, z.B. einen neuen API-Key: oben im Skript ändern, speichern, **Run Script**.

**Neue Version einspielen**: In Telegram `/update` schicken. Das Skript lädt die neueste Version, ersetzt sich selbst und übernimmt deine Einstellungen. Ohne Telegram geht es auch von Hand: den Inhalt durch die neue `camera-import-install.sh` ersetzen, speichern, **Run Script**.

**immich-go aktualisieren**: `IMMICH_GO_VERSION` oben im Skript ändern, **Run Script**.

### Was du für die Einstellungen brauchst

- **API-Key**: Immich → Kontoeinstellungen → API-Schlüssel → Neuer Schlüssel, mit dem Konto, in dessen Bibliothek die Bilder landen sollen. Am einfachsten mit allen Rechten. Mindestens nötig sind:
  `asset.read`, `asset.statistics`, `asset.update`, `asset.upload`, `asset.copy`, `asset.delete`, `asset.download`, `album.create`, `album.read`, `albumAsset.create`, `server.about`, `stack.create`, `tag.asset`, `tag.create`, `user.read`.
- **Telegram-Bot**: bei `@BotFather` mit `/newbot` anlegen, Token notieren. Den Bot über `t.me/<benutzername>` öffnen (die Suche findet neue Bots oft nicht), **Start** tippen und eine Nachricht schicken. Dann `https://api.telegram.org/bot<TOKEN>/getUpdates` im Browser öffnen: Die Zahl bei `"chat":{"id":…}` ist `TG_CHAT_ID`. Nicht die Zahl vor dem Doppelpunkt im Token, das wäre der Bot selbst.

Für einen ersten Test `DRY_RUN="1"` setzen, dann wird nichts hochgeladen.

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
- Nur stapeln: `bash /boot/config/plugins/user.scripts/scripts/camera-import/script stapeln`

`camera-import-install` wartet nicht auf einen laufenden Import, sondern bricht ab und sagt Bescheid. Dann nach dem Import noch einmal **Run Script**.

## Entwicklung

Die Programmteile liegen einzeln in `boot/camera-import/` und `user.scripts/camera-import/`, die Einstellungen und die Installationslogik in `installer/`. `build.sh` setzt daraus `camera-import-install.sh` zusammen. Die Datei ist im Repo, damit man sie direkt kopieren kann; die CI prüft, dass sie aktuell ist.

Release: `VERSION` und `CHANGELOG.md` anpassen, `just build`, mergen, dann Tag `vX.Y.Z` auf main pushen. Der Workflow hängt `camera-import-install.sh` an das Release; von dort holt `/update` sie.

```bash
just check     # bauen, bash -n und shellcheck für alle Skripte
```

## Lizenz

MIT, siehe [LICENSE](LICENSE).
