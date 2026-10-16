#!/bin/bash
#description=camera-import: unmounts the camera after the import and cleans up dead mounts of unplugged cards. Schedule: At Startup of Array, then Run in Background once.
#backgroundOnly=true
#arrayStarted=true

# The container sees the card read-only and cannot unmount it. It asks this
# helper by creating <appdata>/unmount/<card name>. The helper also cleans
# up a card that was unplugged while mounted: Unassigned Devices leaves such
# a mount behind, and does not mount the card again when it comes back.

CONTAINER="camera-import"
UD=/usr/local/emhttp/plugins/unassigned.devices/scripts/rc.unassigned
PIDFILE=/tmp/camera-import-helper.pid

if [[ -e $PIDFILE ]] && kill -0 "$(cat "$PIDFILE")" 2>/dev/null; then
  echo "camera-import-helper is already running (PID $(cat "$PIDFILE"))"
  exit 0
fi
echo $$ >"$PIDFILE"

log() {
  echo "$(date '+%F %T') $*"
  logger -t camera-import-helper -- "$*"
}

# Host folder of the container's /config, so the helper follows the template
config_dir() {
  docker inspect -f '{{range .Mounts}}{{if eq .Destination "/config"}}{{.Source}}{{end}}{{end}}' \
    "$CONTAINER" 2>/dev/null
}

# SCSI host of a block device in sysfs: the same for every partition and LUN
# of one camera (a Sony shows the card and a small PMHOME drive)
scsi_host() {
  local p
  p=$(readlink -f "/sys/class/block/$1" 2>/dev/null) || return 1
  [[ $p == */target* ]] || return 1
  echo "${p%%/target*}"
}

# Card mounts under /mnt/disks as "device mountpoint" lines (FAT/exFAT only:
# never touch hard disks mounted there)
card_mounts() {
  findmnt -rn -t vfat,exfat -o SOURCE,TARGET | awk '$2 ~ "^/mnt/disks/"'
}

# Unmount the card behind a mount point and everything else of the same
# camera, through Unassigned Devices so its view stays right
unmount_card() {
  local mp=$1 src host dev target
  src=$(findmnt -n -o SOURCE --mountpoint "$mp") || { log "$mp is not mounted"; return 0; }
  host=$(scsi_host "$(basename "$src")")
  while read -r dev target; do
    if [[ $dev == "$src" ]] || { [[ -n $host ]] && [[ $(scsi_host "$(basename "$dev")") == "$host" ]]; }; then
      "$UD" umount "$dev" >/dev/null 2>&1
      if findmnt -n --mountpoint "$target" >/dev/null; then
        log "unmounting $target ($dev) failed, see Unassigned Devices log"
      else
        log "unmounted $target ($dev)"
      fi
    fi
  done < <(card_mounts)
}

# A card that was unplugged while mounted: device gone or unreadable
cleanup_dead() {
  local dev target
  while read -r dev target; do
    if [[ -e /sys/class/block/$(basename "$dev") ]] && ls -A "$target" >/dev/null 2>&1; then
      continue
    fi
    umount -l "$target" 2>/dev/null && log "removed dead mount $target ($dev)"
    rmdir "$target" 2>/dev/null
    # Plugged in again already: let Unassigned Devices mount it as usual
    if [[ -b $dev ]]; then
      sleep 1
      "$UD" mount "$dev" >/dev/null 2>&1 && log "mounted $dev again"
    fi
  done < <(card_mounts)
}

log "started (container $CONTAINER)"
n=0
cfg=""
while true; do
  # docker inspect only once a minute
  if ((n % 30 == 0)); then
    cfg=$(config_dir)
    [[ -n $cfg ]] && mkdir -p "$cfg/unmount" && chown 99:100 "$cfg/unmount" 2>/dev/null
  fi
  n=$((n + 1))

  if [[ -n $cfg ]]; then
    for req in "$cfg"/unmount/*; do
      [[ -f $req ]] || continue
      name=$(basename "$req")
      rm -f "$req"
      [[ $name == */* || $name == .* ]] && continue
      unmount_card "/mnt/disks/$name"
    done
  fi
  cleanup_dead
  sleep 2
done
