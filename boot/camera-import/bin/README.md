# immich-go-Binary

Getestete Version: **immich-go v0.32.0** (simulot/immich-go)

Auf dem Unraid-Server:

```bash
mkdir -p /boot/config/camera-import/bin
cd /tmp
wget https://github.com/simulot/immich-go/releases/download/v0.32.0/immich-go_Linux_x86_64.tar.gz
tar xzf immich-go_Linux_x86_64.tar.gz immich-go
cp immich-go /boot/config/camera-import/bin/ && rm immich-go
```

`/boot` ist FAT und nicht ausführbar. Deshalb kopiert `camera-import-setup` die Binary bei jedem Start nach `/usr/local/bin`.

## Aktualisieren

Meldet der Bot eine neue Version, prüf erst die Release-Notes auf geänderte Optionen. Danach führst du dieselben Befehle mit der neuen Versionsnummer aus und klickst bei `camera-import-setup` auf **Run Script**.
