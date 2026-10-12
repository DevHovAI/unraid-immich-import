# unraid-immich-import

Steckst du eine Kamera-Karte (über einen USB-Kartenleser) oder die Kamera selbst an den Unraid-Server, landen die neuen Fotos und Videos automatisch in [Immich](https://immich.app). Ein Telegram-Bot fragt nach dem Album und meldet Fortschritt und Ergebnis.

- **Ein Docker-Container**, eingerichtet über die Unraid-Oberfläche. 7 MB groß, im Leerlauf etwa 3 MB Arbeitsspeicher.
- **Merkt sich, was schon importiert ist.** Steckst du die Karte erneut ein, werden nur neue Dateien angefasst. Bekannte erkennt er am Namen, an der Größe und am Datum, ohne sie zu lesen.
- Was Immich schon hat, wird nicht noch einmal hochgeladen (Prüfsumme). Ins Album kommen nur die neu hochgeladenen Dateien.
- RAW+HIF und RAW+JPG werden gestapelt, auch für früher hochgeladene Bilder (`/stapeln`).
- Die Karte wird **nur lesend** eingehängt. Die Dateien bleiben darauf, und du kannst sie nach ✅ einfach abziehen.
- Getestet mit einer Sony-Kamera. Jede Karte mit `DCIM`-Ordner geht. Bei Sony kommen zusätzlich die Videos aus `PRIVATE/M4ROOT/CLIP` mit.

## Einrichtung

Voraussetzung ist das Plugin **Unassigned Devices** (Apps → „Unassigned Devices“). Es hängt die Karte ein, der Container beobachtet nur den Ordner `/mnt/disks`.

1. **Vorlage holen:** Oben rechts in Unraid das Terminal (`>_`) öffnen, diese Zeile einfügen und Enter drücken:
   ```bash
   wget -qO /boot/config/plugins/dockerMan/templates-user/my-camera-import.xml https://raw.githubusercontent.com/DevHovAI/unraid-immich-import/main/unraid/camera-import.xml
   ```
   Danach Docker → **Add Container** → bei **Template** unter „User templates“ `camera-import` wählen.
2. **Einstellungen** ausfüllen: Immich-Adresse, API-Key, Telegram-Token und Chat-ID (siehe unten). Für den ersten Test **Probelauf = 1**. **Apply**.
3. **Karte einmal einstecken** und in Main → Unassigned Devices beim Gerät auf das Zahnrad klicken. **Automount** und **Read Only** einschalten. Beim Kartenleser und bei der Kamera je einmal, Unassigned Devices merkt sich das pro Gerät.
4. Karte abziehen und neu einstecken. Telegram fragt nach dem Album, dann läuft der Import. Klappt alles, den Probelauf auf 0 stellen.

**Einstellung ändern**, z.B. einen neuen API-Key: Docker → `camera-import` → **Edit** → ändern → **Apply**.

**Updates** meldet Unraid wie bei jedem Container („update ready“).

### Was du für die Einstellungen brauchst

- **API-Key**: Immich → Kontoeinstellungen → API-Schlüssel → Neuer Schlüssel, mit dem Konto, in dessen Bibliothek die Bilder landen sollen. Nötig sind:
  `asset.upload`, `asset.read`, `album.read`, `album.create`, `albumAsset.create`, `stack.create`, `server.about`.
- **Telegram-Bot**: Lege den Bot bei `@BotFather` mit `/newbot` an und notiere den Token. Öffne den Bot über `t.me/<benutzername>`, denn die Suche findet neue Bots oft nicht. Tippe **Start** und schick ihm eine Nachricht. Öffne dann `https://api.telegram.org/bot<TOKEN>/getUpdates` im Browser. Die Zahl bei `"chat":{"id":…}` ist die Chat-ID. Nimm nicht die Zahl vor dem Doppelpunkt im Token, das wäre der Bot selbst.

Ohne Telegram funktioniert der Import auch. Das Album heißt dann „Import JJJJ-MM-TT“ oder folgt der Einstellung **Album**.

## Telegram

| Nachricht / Befehl | Bedeutung |
|---|---|
| 📷 Karte erkannt … In welches Album? | Antworte mit einem Namen oder `/kein`. Ohne Antwort wird nach 2 Minuten „Import JJJJ-MM-TT“ angelegt. Kommt nur, wenn es Neues gibt. |
| 📷 Import gestartet | Anzahl und Größe der neuen Dateien |
| ⏳ 25 / 50 / 75 % geschafft | Zwischenstände (Einstellung **Zwischenstände**) |
| ✅ / ⚠️ / ⏹️ | fertig / mit Fehlern / abgebrochen. Fehlgeschlagene Dateien werden beim nächsten Einstecken erneut versucht. |
| 📷 … nichts Neues | Alles auf der Karte ist schon importiert. |
| 🗂️ Stapeln … | Ergebnis des Stapelns nach dem Import |
| `/status` | Fortschritt von Import und Stapeln |
| `/abbrechen` | Laufenden Import stoppen (sonst das Stapeln) |
| `/letzter` | Zusammenfassung des letzten Imports |
| `/log` | Letzte Zeilen des Protokolls |
| `/stapeln` | RAW+HIF/JPG-Paare der ganzen Bibliothek stapeln |
| `/stapeln JJJJ-MM-TT` | nur, was seit diesem Tag hochgeladen wurde |
| `/vergessen` | Gedächtnis löschen, z.B. um in Immich gelöschte Bilder neu von der Karte zu holen. Was Immich noch hat, wird trotzdem nicht doppelt hochgeladen. |
| `/hilfe` | Alle Befehle |

Der Bot reagiert nur auf Nachrichten aus dem eingetragenen Chat.

## Stapeln

Zwei Dateien kommen nur dann in einen Stapel, wenn alles davon zutrifft:

- Der Dateiname ohne Endung ist gleich, zum Beispiel `DSC01234.ARW` und `DSC01234.HIF`.
- Die Aufnahmezeiten liegen höchstens 2 Sekunden auseinander (einstellbar).
- Das Kameramodell ist gleich, sofern beide Dateien eines haben.
- Die Zuordnung ist eindeutig. Gibt es zum Beispiel HIF, JPG und ARW mit demselben Namen, wird nichts gestapelt.
- Keine der beiden Dateien liegt schon in einem Stapel.

Vorne liegt das HIF bzw. JPG. Mit **Vorne im Stapel = raw** liegt das RAW vorne.

Das Aufnahmedatum liest Immich erst kurz nach dem Upload aus. Fehlt es noch, versucht der Container es nach 30 Minuten erneut, höchstens dreimal.

## Kamera direkt anschließen (Sony)

Im Kameramenü die USB-Verbindung auf **Massenspeicher (MSC)** stellen. Im MTP-Modus erscheint die Kamera nicht als Laufwerk.

## Fehlersuche

- Protokoll: Docker → `camera-import` → **Logs**, oder `/log` im Bot. Die Datei liegt in `appdata/camera-import/camera-import.log`.
- Passiert beim Einstecken nichts: Hängt Unassigned Devices die Karte ein? Unter Main → Unassigned Devices muss sie als „mounted“ erscheinen, und unter `/mnt/disks/<Name>/DCIM` müssen Dateien liegen. Sonst ist **Automount** nicht eingeschaltet.
- „darf die Karte nicht lesen“: Bei der Karte in Unassigned Devices **Read Only** ein- und wieder ausschalten, damit sie neu eingehängt wird. Hilft das nicht, in der Container-Vorlage (Advanced View) **PUID = 0** setzen.
- Das Gedächtnis ist `appdata/camera-import/importiert.txt`, eine Zeile pro Datei.

## Umstieg auf v2

Bis Version 1.x war camera-import ein Skript für **User Scripts**. So steigst du um:

1. **Alte Version entfernen**: Im Skript `camera-import-install` `DEINSTALLIEREN="1"` setzen und **Run Script** klicken. Ab v1.1.0 entfernt das Bot, Auslöser, beide User Scripts und `/boot/config/camera-import`. Bei einer älteren Version erst `/update` im Bot schicken.
   Das muss vor dem Start des Containers passieren. Ein Telegram-Bot kann nur von einer Stelle abgefragt werden.
2. **Einrichtung** wie oben. Die Einstellungen tippst du einmal neu in die Vorlage, die Namen sind dieselben wie im Skript.
3. Beim ersten Einstecken prüft der Container jede Datei per Prüfsumme gegen Immich. Was schon da ist, lädt er nicht noch einmal hoch, er merkt es sich nur. Ab dann geht jedes Einstecken in Sekunden.

Die alte Skript-Version liegt in [`legacy/`](legacy/) und wird nicht weiterentwickelt.

## Entwicklung

Go ohne fremde Abhängigkeiten.

| Ort | Inhalt |
|---|---|
| `cmd/camera-import` | Start und Wechsel auf PUID:PGID |
| `internal/importer` | Karten finden, prüfen, hochladen |
| `internal/memory` | Gedächtnis |
| `internal/stack` | Paare und Stapel |
| `internal/telegram` | Bot |
| `internal/app` | verbindet die Teile, Bot-Befehle |
| `internal/fakeimmich` | nachgebautes Immich für Tests |
| `unraid/` | Container-Vorlage und Icon |

Go läuft lokal in podman, eine Installation ist nicht nötig.

```bash
just check            # gofmt, go vet, Tests mit Race-Detector, Prüfungen der Skript-Version
just image 2.0.0-dev  # Image bauen
just fake             # nachgebautes Immich auf :2283 (API-Key KEY) für Rauchtests
```

Release: `VERSION` und `CHANGELOG.md` anpassen, mergen, dann den Tag `vX.Y.Z` auf main pushen. Der Workflow baut das Image und lädt es nach `ghcr.io/devhovai/unraid-immich-import` hoch (`:X.Y.Z` und `:latest`). Vorabversionen wie `v2.0.0-beta.1` landen unter `:beta`.

## Lizenz

MIT, siehe [LICENSE](LICENSE).
