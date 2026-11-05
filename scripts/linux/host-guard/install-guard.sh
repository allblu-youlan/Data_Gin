#!/bin/bash
set -euo pipefail
cd -- "$(dirname -- "$0")"
python3 -c 'import ast; ast.parse(open("vscode-resource-guard.py").read())'
python3 vscode-resource-guard.py --config vscode-resource-guard.json --dry-run
backup_dir="/mnt/host-monitor/guard-backup-$(date +%Y%m%d-%H%M%S)"
install -d -m 0700 "$backup_dir"
for file in /usr/local/sbin/vscode-resource-guard.py /etc/vscode-resource-guard.json /etc/systemd/system/vscode-resource-guard.service; do
    if [ -f "$file" ]; then cp -a "$file" "$backup_dir/"; fi
done
install -m 0755 vscode-resource-guard.py /usr/local/sbin/vscode-resource-guard.py
install -m 0600 vscode-resource-guard.json /etc/vscode-resource-guard.json
install -m 0644 vscode-resource-guard.service /etc/systemd/system/vscode-resource-guard.service
systemd-analyze verify /etc/systemd/system/vscode-resource-guard.service
systemctl daemon-reload
systemctl enable --now vscode-resource-guard.service
