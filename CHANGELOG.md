# Changelog

## [2.0.0-beta.5] – 2026-10-17

### Added
- The card is unmounted after every import and after `/cancel`, so it can be unplugged safely. This needs the new User Script `camera-import-helper` (see README); `AUTO_UNMOUNT=0` turns it off.
- The helper also removes the dead mount of a card that was unplugged while mounted, and lets Unassigned Devices mount it again. Before, such a card was not imported until it was unmounted by hand.

### Changed
- The bot no longer says "The card can be removed" without unmounting.

## [2.0.0-beta.4] – 2026-10-16

### Added
- Backup: `/backup on` copies every inserted card into a backup folder (new template field), one folder per day, checked by checksum, before the import. `/backup off` turns it off, `/backup` shows the state.

## [2.0.0-beta.3] – 2026-10-16

### Fixed
- Telegram did not work at all in beta.2: the bot still offered HTTP/2 and could not read the answer
- A card under `/mnt/disks` that cannot be read (e.g. a stale mount after the camera was unplugged) is reported in Telegram instead of being skipped silently

## [2.0.0-beta.2] – 2026-10-16

### Changed
- camera-import now speaks English: Telegram messages, commands, log, error messages, container template and README
- Bot commands are now `/cancel`, `/last`, `/stack`, `/none`, `/forget` and `/help`. The German commands still work.
- The files in appdata are now called `imported.txt` and `last.txt`; the old ones are renamed on the first start, nothing is lost
- Sizes use a decimal point (1.5 GB)

### Fixed
- Telegram answers could take minutes: a silently dropped connection kept every following request hanging. The bot now uses a fresh connection after a failed request and polls with a shorter timeout.
- The log shows what the container sees under `/mnt/disks` and why an entry is not imported (no `DCIM`, no permission), instead of staying silent

## [2.0.0-beta.1] – 2026-10-12

camera-import is now a Docker container. How to switch: see README, section "Moving to v2".

### Added
- Memory: files that were already imported are skipped when the card is inserted again, without reading them
- `/vergessen` clears the memory
- `/abbrechen` also stops stacking when no import is running
- The album question only comes when there is something new on the card

### Changed
- Own upload instead of immich-go; Immich recognises duplicates by checksum before anything is uploaded
- Settings live in the container template instead of the script, updates come through Docker instead of `/update`
- Unassigned Devices mounts the card read-only; the container needs no special privileges
- The script version lives in `legacy/`

### Removed
- `/update`, `RAW_JPEG`, `IMMICH_GO_VERSION`, `CHECK_UPDATES`, `AUTO_UNMOUNT`

## [1.1.0] – 2026-10-12

### Added
- Setting `DEINSTALLIEREN="1"`: removes camera-import completely from the server with "Run Script" (preparation for switching to the Docker container v2)

### Changed
- The bot doesn't offer a new major version (v2) via /update, it points to the switch once instead

## [1.0.3] – 2026-10-12

### Fixed
- The album also received photos that were already in Immich (e.g. when inserting the same card again). Now only the files newly uploaded by this import go into it; the final message says how many.

## [1.0.2] – 2026-10-12

### Fixed
- /update aborted with "syntax error": the new install script ran directly from the file it was rewriting

## [1.0.1] – 2026-10-12

### Changed
- Updates are only checked at array start and on messages to the bot, no longer daily in the background

## [1.0.0] – 2026-10-12

First released version.

### Added
- Updates via Telegram: the bot reports new versions, /update installs them
- /status also shows stacking progress, with an estimated time left
- A single install script `camera-import-install` with the settings at the top of the script: downloads immich-go, sets up the import, starts the bot, checks Immich and Telegram. Replaces `camera-import-setup`, the conf template and manual work with `scp`
- Automatic import from SD card or camera to Immich via udev and User Scripts
- RAW+JPG stacks, duplicate detection via immich-go
- Telegram messages on start, progress (25/50/75 %) and result
- `camera-import/script stapeln [YYYY-MM-DD]` stacks by hand from the console
- Telegram bot with /status, /abbrechen, /letzter, /log, /hilfe
- Album choice: none, fixed, by date or by asking via Telegram
- Pre-check whether Immich is reachable
- Message when the card can't be mounted or immich-go is missing
- Automatic unmounting of cards mounted by Unassigned Devices
- Notice about new immich-go versions
- Separate stacking step for RAW+HIF/JPG pairs (name, capture time, camera), also for older photos via /stapeln

### Fixed
- When the bot stopped, curl/php of the running Telegram poll were left behind and held the lock for up to a minute
- The bot started by the install script inherited the import lock: new imports waited forever, the install script reported "an import is running"
- The install script deleted itself when pasted into the old script camera-import-setup
- /stapeln didn't start after a reboot as long as no import had run yet (log folder missing)
- Stacking stops instead of running forever when Immich returns the same photos again while paging
- /status and error messages read immich-go's log format correctly (INF/ERR instead of level=…)
