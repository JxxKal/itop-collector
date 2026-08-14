#!/bin/sh
# Richtet die Verzeichnisrechte ein, bevor Apache startet.
#
# Warum noetig: conf/, data/ und env-production/ haengen an Docker-Volumes.
# Ein frisch angelegtes Volume, ein wiederhergestelltes Backup oder ein
# Bind-Mount vom Host bringt eine Ownership mit, die nicht zu www-data im
# Container passt. Ergebnis ist immer dasselbe Fehlerbild:
#
#   Warning: file_put_contents(.../data/cache-production/...): Permission denied
#   Fatal error: Unable to create the cache directory (.../data/cache-production/twig/...)
#
# Deshalb bei jedem Start korrigieren statt einmalig im Image.

set -e

APPROOT=/var/www/html
WEBUSER=www-data

for d in conf data env-production extensions log; do
    [ -d "$APPROOT/$d" ] || mkdir -p "$APPROOT/$d"
done

# data/backups ist ein eigenes Volume und kann gross sein - beim rekursiven
# Durchlauf ueberspringen, sonst dauert jeder Containerstart unnoetig lange.
#
# ABER: das Verzeichnis SELBST muss www-data gehoeren. Ein frisch angelegtes
# Named Volume erbt die Ownership des Mountpunkts im Image, und ein leeres
# Volume auf einem im Image nicht existierenden Pfad legt Docker als root an.
# Der -prune oben hat genau diesen Fall bisher mit uebersprungen. Folge beim
# ersten Setup-Lauf einer Neuinstallation:
#
#   Warning: mkdir(): Permission denied in setup/setuputils.class.inc.php:802
#   Fatal error: Directory "/var/www/html/data/backups/manual" was not created
#
# Das ist ein chown auf genau ein Verzeichnis, kein Durchlauf - der Grund fuer
# den prune bleibt also gewahrt.
[ -d "$APPROOT/data/backups" ] || mkdir -p "$APPROOT/data/backups"
chown "$WEBUSER:$WEBUSER" "$APPROOT/data/backups" 2>/dev/null || true

find "$APPROOT/data" -path "$APPROOT/data/backups" -prune -o \
     ! -user "$WEBUSER" -exec chown "$WEBUSER:$WEBUSER" {} + 2>/dev/null || true

for d in conf env-production; do
    find "$APPROOT/$d" ! -user "$WEBUSER" -exec chown "$WEBUSER:$WEBUSER" {} + 2>/dev/null || true
done

# Der Model-Cache wird beim Setup ohnehin neu erzeugt. Liegt er von einer
# anderen iTop-Version herum, verursacht er nur Folgefehler.
if [ -n "${ITOP_CLEAR_CACHE_ON_START:-}" ]; then
    rm -rf "$APPROOT/data/cache-production" 2>/dev/null || true
    echo "[entrypoint] cache-production geleert"
fi

echo "[entrypoint] Rechte geprueft, starte: $*"

# An den Entrypoint des offiziellen PHP-Images weiterreichen, sonst gehen
# dessen Sonderbehandlungen verloren.
exec docker-php-entrypoint "$@"
