# itop-collector - nimmt Agent-Meldungen entgegen und schreibt sie ueber
# iTop Synchro Data Sources in die CMDB.
#
# Bauen (aus dem Repo-Wurzelverzeichnis):
#   docker build -t itop-collector:0.1.0 -f deploy/collector/Dockerfile .

FROM golang:1.23-alpine AS build
WORKDIR /src

# Erst die Modul-Dateien, dann der Rest: solange sich go.mod/go.sum nicht
# aendern, bleibt der Abhaengigkeits-Layer im Cache.
COPY go.mod ./
RUN go mod download

COPY . .

# CGO_ENABLED=0 -> statisches Binary, laeuft in einem scratch-Image ohne libc.
# -trimpath entfernt lokale Pfade aus dem Binary (reproduzierbarer, weniger
# Information nach aussen), -s -w sparen die Debug-Symbole.
RUN CGO_ENABLED=0 GOOS=linux go build \
      -trimpath -ldflags="-s -w" \
      -o /out/itop-collector ./cmd/collector

# Zwischenstufe nur fuer die Dateien, die scratch nicht mitbringt.
FROM alpine:3.20 AS certs
RUN apk add --no-cache ca-certificates
# Das Registry-Verzeichnis MUSS im Image existieren und dem Laufzeitbenutzer
# gehoeren. Docker uebernimmt die Ownership eines neuen Named Volumes vom
# Mountpunkt im Image - fehlt der Pfad, legt Docker ihn als root an, und der
# Prozess (UID 65532) bekommt beim ersten Schreiben "permission denied".
# Dieselbe Falle wie bei iTops data/backups.
RUN mkdir -p /seed/var/lib/itop-collector \
 && chown -R 65532:65532 /seed/var/lib/itop-collector

FROM scratch

# Wurzelzertifikate: der Collector spricht iTop ueber HTTPS an. Ohne CA-Bundle
# schlaegt jede Verbindung fehl - und zwar auch dann, wenn das Zertifikat gueltig
# ist. (Fuer Testinstanzen mit selbstsigniertem Zertifikat gibt es zusaetzlich
# ITOP_SKIP_TLS_VERIFY, das ist aber kein Ersatz.)
COPY --from=certs /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/
COPY --from=certs --chown=65532:65532 /seed/var/lib/itop-collector /var/lib/itop-collector
COPY --from=build /out/itop-collector /itop-collector

# Unprivilegiert. scratch hat keine /etc/passwd, deshalb die reine UID.
USER 65532:65532

# Registry-Ablage. Muss als Volume eingehaengt werden, sonst ist nach jedem
# Neustart jedes Geraet unbekannt und muesste sich neu registrieren.
VOLUME ["/var/lib/itop-collector"]

EXPOSE 8080
ENTRYPOINT ["/itop-collector"]
