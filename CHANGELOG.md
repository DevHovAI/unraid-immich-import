# Changelog

## [Unveröffentlicht]

### Hinzugefügt
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
- /status und Fehlermeldungen lesen das Protokollformat von immich-go richtig (INF/ERR statt level=…)
