#!/bin/sh
set -e

systemctl daemon-reload >/dev/null 2>&1 || true

# BEWUSST nur enable, nicht start: der Agent braucht vor dem ersten Lauf ein
# Device-Token. Ein sofortiger Start wuerde nur fehlschlagen und das Log
# fuellen. Der Ablauf ist:
#
#   itop-agent -enroll <einmal-token>
#   systemctl start itop-agent
systemctl enable itop-agent.service >/dev/null 2>&1 || true

cat <<'HINWEIS'

itop-agent installiert.

Noch zu tun:
  1. /etc/itop-agent.conf anpassen (ITOP_COLLECTOR_URL)
  2. itop-agent -enroll <einmal-token>
  3. systemctl start itop-agent

Zum Pruefen ohne Collector:  itop-agent -collect

HINWEIS
