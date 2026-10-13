# unraid-immich-import

Plug a camera card (via a USB card reader) or the camera itself into your Unraid server, and the new photos and videos land in [Immich](https://immich.app) automatically. A Telegram bot asks for the album and reports progress and results.

- **One Docker container**, set up through the Unraid web UI. 7 MB in size, about 3 MB of RAM when idle.
- **Remembers what was already imported.** Insert the card again and only new files are touched. Known files are recognised by name, size and date, without reading them.
- Anything Immich already has is not uploaded again (checksum). Only the newly uploaded files go into the album.
- RAW+HIF and RAW+JPG are stacked, also for photos uploaded earlier (`/stack`).
- The card is mounted **read-only**. The files stay on it, and you can simply pull it after ✅.
- Tested with a Sony camera. Any card with a `DCIM` folder works. On Sony, the videos from `PRIVATE/M4ROOT/CLIP` are included as well.

## Setup

You need the **Unassigned Devices** plugin (Apps → "Unassigned Devices"). It mounts the card; the container only watches the folder `/mnt/disks`.

1. **Get the template:** open the terminal in Unraid (`>_`, top right), paste this line and press Enter:
   ```bash
   wget -qO /boot/config/plugins/dockerMan/templates-user/my-camera-import.xml https://raw.githubusercontent.com/DevHovAI/unraid-immich-import/main/unraid/camera-import.xml
   ```
   Then Docker → **Add Container** → under **Template**, pick `camera-import` from "User templates".
2. **Fill in the settings:** Immich URL, API key, Telegram token and chat ID (see below). For the first test set **Dry run = 1**. **Apply**.
3. **Insert the card once**, then in Main → Unassigned Devices click the gear icon next to the device. Turn on **Automount** and **Read Only**. Do this once for the card reader and once for the camera; Unassigned Devices remembers it per device.
4. Pull the card and insert it again. Telegram asks for the album, then the import runs. If everything works, set Dry run back to 0.

**Change a setting**, e.g. a new API key: Docker → `camera-import` → **Edit** → change → **Apply**.

**Updates** are shown by Unraid like for any container ("update ready").

### What you need for the settings

- **API key**: Immich → Account Settings → API Keys → New API Key, created with the account whose library the photos should go into. Required permissions:
  `asset.upload`, `asset.read`, `album.read`, `album.create`, `albumAsset.create`, `stack.create`, `server.about`.
- **Telegram bot**: create the bot with `@BotFather` using `/newbot` and note the token. Open the bot via `t.me/<username>`, because search often doesn't find new bots. Tap **Start** and send it a message. Then open `https://api.telegram.org/bot<TOKEN>/getUpdates` in your browser. The number at `"chat":{"id":…}` is the chat ID. Don't use the number before the colon in the token, that is the bot itself.

The import works without Telegram too. The album is then called "Import YYYY-MM-DD" or follows the **Album** setting.

## Telegram

| Message / command | Meaning |
|---|---|
| 📷 Card detected … Which album? | Reply with a name or `/none`. Without a reply, "Import YYYY-MM-DD" is created after 2 minutes. Only shown when there is something new. |
| 📷 Import started | Number and size of the new files |
| ⏳ 25 / 50 / 75 % done | Progress (setting **Progress messages**) |
| ✅ / ⚠️ / ⏹️ | done / with errors / cancelled. Failed files are retried the next time the card is inserted. |
| 📷 … nothing new | Everything on the card has already been imported. |
| 🗂️ Stacking … | Result of stacking after the import |
| `/status` | Progress of import and stacking |
| `/cancel` | Stop the running import (otherwise stacking) |
| `/last` | Summary of the last import |
| `/log` | Last lines of the log |
| `/stack` | Stack RAW+HIF/JPG pairs across the whole library |
| `/stack YYYY-MM-DD` | Only what was uploaded since that day |
| `/forget` | Clear the memory, e.g. to fetch photos deleted in Immich from the card again. Anything Immich still has is not uploaded twice. |
| `/help` | All commands |

The bot only reacts to messages from the configured chat. The German commands of v1 (`/abbrechen`, `/letzter`, `/stapeln`, `/kein`, `/vergessen`, `/hilfe`) still work.

## Stacking

Two files only end up in a stack if all of this is true:

- The file name without extension is the same, for example `DSC01234.ARW` and `DSC01234.HIF`.
- The capture times are at most 2 seconds apart (configurable).
- The camera model is the same, if both files have one.
- The match is unique. If there are HIF, JPG and ARW with the same name, for example, nothing is stacked.
- Neither file is already in a stack.

The HIF or JPG is on top. With **Stack cover = raw** the RAW is on top.

Immich reads the capture date only shortly after the upload. If it's still missing, the container tries again after 30 minutes, at most three times.

## Connecting the camera directly (Sony)

In the camera menu, set the USB connection to **Mass Storage (MSC)**. In MTP mode the camera doesn't show up as a drive.

## Troubleshooting

- Log: Docker → `camera-import` → **Logs**, or `/log` in the bot. The file is `appdata/camera-import/camera-import.log`.
- Nothing happens when you insert the card: does Unassigned Devices mount it? Under Main → Unassigned Devices it must show as "mounted", and there must be files under `/mnt/disks/<name>/DCIM`. Otherwise **Automount** is not turned on.
- "not allowed to read the card": in Unassigned Devices, turn **Read Only** off and on again for the card so it is mounted again. If that doesn't help, set **PUID = 0** in the container template (Advanced View).
- The memory is `appdata/camera-import/imported.txt`, one line per file. Files from 2.0.0-beta.1 (`importiert.txt`, `letzter.txt`) are renamed on the first start.

## Moving to v2

Up to version 1.x, camera-import was a script for **User Scripts**. To switch:

1. **Remove the old version**: in the script `camera-import-install` set `DEINSTALLIEREN="1"` and click **Run Script**. From v1.1.0 on, this removes the bot, the trigger, both user scripts and `/boot/config/camera-import`. With an older version, send `/update` to the bot first.
   This has to happen before the container starts. A Telegram bot can only be polled from one place.
2. **Setup** as above. You type the settings into the template once; the names are the same as in the script.
3. On the first insert the container checks every file against Immich by checksum. Anything already there is not uploaded again, only remembered. From then on, every insert takes seconds.

The old script version lives in [`legacy/`](legacy/) and is no longer developed.

## Development

Go without third-party dependencies.

| Where | What |
|---|---|
| `cmd/camera-import` | start and switch to PUID:PGID |
| `internal/importer` | find cards, check, upload |
| `internal/memory` | memory |
| `internal/stack` | pairs and stacks |
| `internal/telegram` | bot |
| `internal/app` | ties the parts together, bot commands |
| `internal/fakeimmich` | fake Immich for tests |
| `unraid/` | container template and icon |

Go runs in podman locally, no installation needed.

```bash
just check            # gofmt, go vet, tests with race detector, checks of the script version
just image 2.0.0-dev  # build the image
just fake             # fake Immich on :2283 (API key KEY) for smoke tests
```

Release: update `VERSION` and `CHANGELOG.md`, merge, then push the tag `vX.Y.Z` on main. The workflow builds the image and pushes it to `ghcr.io/devhovai/unraid-immich-import` (`:X.Y.Z` and `:latest`). Pre-releases like `v2.0.0-beta.1` go to `:beta`.

## License

MIT, see [LICENSE](LICENSE).
