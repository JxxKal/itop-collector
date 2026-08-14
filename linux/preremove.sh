#!/bin/sh
set -e

if [ -d /run/systemd/system ]; then
    systemctl stop itop-agent.service >/dev/null 2>&1 || true
    systemctl disable itop-agent.service >/dev/null 2>&1 || true
fi

# /var/lib/itop-agent bleibt bewusst stehen: darin liegen GUID und
# Device-Token. Wird das Paket nur aktualisiert oder kurzzeitig entfernt, muss
# sich die Maschine sonst neu registrieren - und bekaeme eine neue GUID, was in
# iTop ein zweites CI erzeugt.
exit 0
