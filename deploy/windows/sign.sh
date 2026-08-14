#!/usr/bin/env bash
#
# Signiert die Windows-Artefakte (EXE und MSI) mit einem Code-Signing-Zertifikat.
#
# Aufruf:
#   SIGN_PFX=/pfad/zum/cert.pfx SIGN_PASS='...' deploy/windows/sign.sh dist/*.exe dist/*.msi
#
# Das Passwort kommt aus der Umgebung und NICHT als Argument: Argumente stehen in
# der Prozessliste und landen in der Shell-Historie.
#
# Signiert wird mit osslsigncode in einem Container - kein Windows noetig, und es
# muss nichts lokal installiert werden.

set -euo pipefail

: "${SIGN_PFX:?SIGN_PFX fehlt - Pfad zur PFX-Datei}"
: "${SIGN_PASS:?SIGN_PASS fehlt - Passwort der PFX-Datei}"

# ZEITSTEMPEL IST PFLICHT, nicht optional.
#
# Ohne Zeitstempel wird jede Signatur in dem Moment ungueltig, in dem das
# Zertifikat ablaeuft - auch bei Dateien, die lange vorher gebaut wurden. Mit
# Zeitstempel bestaetigt eine dritte Stelle, dass zum Signierzeitpunkt ein
# gueltiges Zertifikat vorlag; die Signatur bleibt danach gueltig.
SIGN_TS="${SIGN_TS:-http://time.certum.pl}"

SIGN_NAME="${SIGN_NAME:-iTop Inventory Agent}"
SIGN_URL="${SIGN_URL:-https://github.com/JxxKal/itop-collector}"
SIGNER_IMAGE="${SIGNER_IMAGE:-itop-signer}"

if [ "$#" -eq 0 ]; then
    echo "Keine Dateien angegeben." >&2
    exit 1
fi

# Signier-Image bei Bedarf bauen.
if ! docker image inspect "$SIGNER_IMAGE" >/dev/null 2>&1; then
    echo "Signier-Image wird gebaut ..."
    docker build -q -t "$SIGNER_IMAGE" - >/dev/null <<'DOCKERFILE'
FROM debian:bookworm-slim
RUN apt-get update -qq \
 && apt-get install -y --no-install-recommends osslsigncode ca-certificates \
 && rm -rf /var/lib/apt/lists/*
DOCKERFILE
fi

# Die PFX in ein eigenes Verzeichnis legen, damit nur sie in den Container
# gereicht wird - nicht das ganze Home-Verzeichnis.
workdir="$(mktemp -d)"
trap 'rm -rf "$workdir"' EXIT
cp "$SIGN_PFX" "$workdir/cert.pfx"
chmod 600 "$workdir/cert.pfx"

for file in "$@"; do
    [ -f "$file" ] || { echo "uebersprungen (nicht vorhanden): $file"; continue; }

    dir="$(cd "$(dirname "$file")" && pwd)"
    base="$(basename "$file")"

    echo "signiere $base ..."
    docker run --rm \
        -v "$workdir":/sign:ro \
        -v "$dir":/work \
        -e PASS="$SIGN_PASS" \
        -e NAME="$SIGN_NAME" -e URL="$SIGN_URL" -e TS="$SIGN_TS" \
        "$SIGNER_IMAGE" sh -c '
            set -e
            osslsigncode sign \
                -pkcs12 /sign/cert.pfx -pass "$PASS" \
                -n "$NAME" -i "$URL" \
                -h sha256 \
                -ts "$TS" \
                -in "/work/'"$base"'" \
                -out "/work/'"$base"'.signed" >/dev/null
            mv "/work/'"$base"'.signed" "/work/'"$base"'"
        '

    # Gegenpruefen. Die Kettenpruefung schlaegt fehl, wenn die ausstellende CA
    # dem Container unbekannt ist - das ist kein Fehler der Signatur. Geprueft
    # wird deshalb nur, ob Signatur und Zeitstempel vorhanden sind.
    #
    # Die Ausgabe wird ERST eingesammelt und DANN durchsucht: "verify" endet bei
    # unbekannter CA mit Exitcode ungleich 0, und mit "set -o pipefail" wuerde
    # das die ganze Pipeline scheitern lassen - selbst wenn grep faendig wird.
    verify_output="$(docker run --rm -v "$dir":/work "$SIGNER_IMAGE" \
        osslsigncode verify -in "/work/$base" 2>&1 || true)"

    if grep -q "timestamped" <<<"$verify_output"; then
        stamp="$(grep -m1 "timestamped" <<<"$verify_output" | sed 's/.*timestamped: //')"
        echo "  signiert, Zeitstempel: $stamp"
    else
        echo "  FEHLER: kein Zeitstempel in $base" >&2
        echo "$verify_output" >&2
        exit 1
    fi
done

echo "fertig."
