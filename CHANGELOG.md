# Changelog

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
