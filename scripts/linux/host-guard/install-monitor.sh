#!/bin/bash
set -euo pipefail
cd -- "$(dirname -- "$0")"
mountpoint -q /mnt
command -v pidstat iostat logrotate >/dev/null
install -d -m 0700 /mnt/host-monitor
backup_dir="/mnt/host-monitor/backup-$(date +%Y%m%d-%H%M%S)"
install -d -m 0700 "$backup_dir"
for unit in host-pidstat.service host-iostat.service host-monitor-rotate.service host-monitor-rotate.timer; do
    if [ -f "/etc/systemd/system/$unit" ]; then
        cp -a "/etc/systemd/system/$unit" "$backup_dir/"
    fi
    install -m 0644 "$unit" "/etc/systemd/system/$unit"
done
if [ -f /etc/host-monitor.logrotate ]; then
    cp -a /etc/host-monitor.logrotate "$backup_dir/"
fi
install -m 0600 host-monitor.logrotate /etc/host-monitor.logrotate
systemd-analyze verify /etc/systemd/system/host-{pidstat,iostat,monitor-rotate}.service /etc/systemd/system/host-monitor-rotate.timer
logrotate -d /etc/host-monitor.logrotate
systemctl daemon-reload
systemctl enable --now host-pidstat.service host-iostat.service host-monitor-rotate.timer
systemctl start host-monitor-rotate.service
