# Changelog

## [2.0.0-beta.1] – 2026-10-12

camera-import ist jetzt ein Docker-Container. Umstieg siehe README, Abschnitt „Umstieg auf v2“.

### Hinzugefügt
- Gedächtnis: schon importierte Dateien werden beim erneuten Einstecken übersprungen, ohne sie zu lesen
- `/vergessen` löscht das Gedächtnis
- `/abbrechen` stoppt auch das Stapeln, wenn kein Import läuft
- Albumfrage nur noch, wenn es auf der Karte Neues gibt

### Geändert
- Eigener Upload statt immich-go; Duplikate erkennt Immich per Prüfsumme, bevor hochgeladen wird
- Einstellungen in der Container-Vorlage statt im Skript, Updates über Docker statt `/update`
- Die Karte hängt Unassigned Devices nur lesend ein; der Container braucht keine Sonderrechte
- Die Skript-Version liegt in `legacy/`

### Entfernt
- `/update`, `RAW_JPEG`, `IMMICH_GO_VERSION`, `CHECK_UPDATES`, `AUTO_UNMOUNT`

## [1.1.0] – 2026-10-12

### Hinzugefügt
- Einstellung `DEINSTALLIEREN="1"`: entfernt camera-import mit „Run Script“ vollständig vom Server (Vorbereitung für den Umstieg auf den Docker-Container v2)

### Geändert
- Eine neue Hauptversion (v2) bietet der Bot nicht per /update an, sondern weist einmal auf den Umstieg hin

## [1.0.3] – 2026-10-12

### Behoben
- Ins Album kamen auch Bilder, die schon vorher in Immich lagen (z.B. beim erneuten Einstecken derselben Karte). Jetzt landen nur die bei diesem Import neu hochgeladenen Dateien darin; die Abschlussmeldung nennt, wie viele.

## [1.0.2] – 2026-10-12

### Behoben
- /update brach mit „syntax error“ ab: das neue Installationsskript lief direkt aus der Datei, die es gerade umschrieb

## [1.0.1] – 2026-10-12

### Geändert
- Nach Updates wird nur noch beim Array-Start und bei Nachrichten an den Bot geschaut, nicht mehr täglich im Hintergrund

## [1.0.0] – 2026-10-12

Erste veröffentlichte Version.

### Hinzugefügt
- Updates über Telegram: der Bot meldet neue Versionen, /update installiert sie
- /status zeigt auch den Fortschritt beim Stapeln, mit geschätzter Restzeit
- Ein einziges Installationsskript `camera-import-install` mit den Einstellungen oben im Skript: lädt immich-go, legt den Import an, startet den Bot, prüft Immich und Telegram. Ersetzt `camera-import-setup`, die conf-Vorlage und die Handarbeit mit `scp`
- Automatischer Import von SD-Karte oder Kamera nach Immich per udev und User Scripts
- RAW+JPG-Stapel, Duplikaterkennung über immich-go
- Telegram-Meldungen zu Start, Zwischenständen (25/50/75 %) und Ergebnis
- `camera-import/script stapeln [JJJJ-MM-TT]` stapelt von Hand aus der Konsole
- Telegram-Bot mit /status, /abbrechen, /letzter, /log, /hilfe
- Albumwahl: keins, fest, nach Datum oder per Telegram-Rückfrage
- Vorabprüfung, ob Immich erreichbar ist
- Meldung, wenn die Karte nicht eingehängt werden kann oder immich-go fehlt
- Automatisches Aushängen von Karten, die Unassigned Devices eingehängt hat
- Hinweis auf neue immich-go-Versionen
- Eigener Stapel-Schritt für RAW+HIF/JPG-Paare (Name, Aufnahmezeit, Kamera), auch für ältere Bilder per /stapeln

### Behoben
- Beim Beenden des Bots blieben curl/php der laufenden Telegram-Abfrage übrig und hielten die Sperre noch bis zu einer Minute
- Der vom Installationsskript gestartete Bot erbte die Import-Sperre: neue Importe warteten endlos, das Installationsskript meldete „Gerade läuft ein Import“
- Das Installationsskript löschte sich selbst, wenn es in das alte Skript camera-import-setup eingefügt wurde
- /stapeln startete nach einem Neustart nicht, solange noch kein Import gelaufen war (Log-Ordner fehlte)
- Stapeln bricht ab, statt endlos zu laufen, wenn Immich beim Blättern dieselben Bilder erneut liefert
- /status und Fehlermeldungen lesen das Protokollformat von immich-go richtig (INF/ERR statt level=…)
