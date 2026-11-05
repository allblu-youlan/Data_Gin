#!/bin/bash
# Keep existing processes alive and remove the resource caps.
set -euo pipefail
systemctl disable --now vscode-resource-guard.service
python3 /usr/local/sbin/vscode-resource-guard.py --release
