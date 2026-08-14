# itop-agent

Inventar-Agent und Collector für die iTop-CMDB. Siehe `PROJECT.md` für Ziel,
Architektur und Roadmap.

**Stand:** M0–M4 sind durch und auf echten Maschinen verifiziert — Windows 11
und Debian 13 melden über den Collector in eine iTop-3.2.3-Instanz. Offen ist
die Paketierung für Windows (MSI, Code-Signing) und das Software-Inventar auf
der iTop-Seite.

## Was hier schon funktioniert

```
internal/report/          Meldeformat v2 (Abschnitt 4 des PROJECT.md + virtualization)
internal/collect/         Sammlung: Linux (DMI//proc/dpkg) und Windows (WMI/Registry)
internal/config/          Konfiguration: Umgebung, dann Registry bzw. /etc/itop-agent.conf
internal/service/         Windows-Dienst (svc + Eventlog), Linux via systemd
internal/identity/        GUID und Device-Token: /var/lib/itop-agent bzw. HKLM\SOFTWARE\iTopAgent
internal/push/            HTTPS-Push mit CA-Pinning, Retry und Backoff
internal/sched/           Takt: 24 h + Jitter, verzögerter Erststart
internal/collectorsrv/    Collector: Routing, Reimaging, Übernahme, CSV, iTop
cmd/agent/                Agent, inkl. interaktivem Modus
cmd/collector/            Collector
deploy/collector/         Dockerfile (statisches Binary auf scratch, 6,5 MB)
deploy/linux/             systemd-Unit, nfpm-Definition für .deb/.rpm
Makefile                  Cross-Compile, Tests, Pakete — alles im Container
```

Go muss nicht lokal installiert sein: `make check`, `make all`, `make packages`
laufen alle in `golang:1.23-alpine` bzw. `goreleaser/nfpm`.

## Agent

```bash
# einmal sammeln und ansehen - braucht weder Collector noch Enrollment
./itop-agent -collect

# registrieren, danach getaktet melden
export ITOP_COLLECTOR_URL=https://collector.example.internal
./itop-agent -enroll <einmal-token>
./itop-agent                      # Dienstbetrieb
./itop-agent -once                # einmal melden
```

| Variable | Bedeutung |
|---|---|
| `ITOP_COLLECTOR_URL` | Basis-URL des Collectors (Pflicht) |
| `ITOP_CA_CERT` | PEM der internen CA — verankert **nur** diese (das Pinning aus §6) |
| `ITOP_SKIP_TLS_VERIFY=1` | Prüfung abschalten, nur für Testaufbauten; warnt bei jedem Start |
| `ITOP_AGENT_STATE_DIR` | Ablage für GUID/Token unter Linux, Vorgabe `/var/lib/itop-agent` |

Unter Windows liegt beides in `HKLM\SOFTWARE\iTopAgent`.

**Wichtig für den Dienstbetrieb:** ein Windows-Dienst erbt die Umgebungsvariablen
der aufrufenden Shell **nicht**. Wer interaktiv mit gesetztem
`ITOP_COLLECTOR_URL` testet und den Dienst dann installiert, bekommt einen
Dienst, der beim Start sofort aussteigt. Deshalb:

```powershell
.\itop-agent.exe -set-url http://collector.example.internal
.\itop-agent.exe -enroll <einmal-token>
.\itop-agent.exe -install          # registriert auch die Ereignisquelle
Start-Service itop-agent
```

Der Dienst protokolliert ins **Anwendungsprotokoll** unter der Quelle
`itop-agent` — stdout/stderr gibt es dort nicht, ohne Eventlog liefe er blind.
Unter Linux übernimmt das journald (`journalctl -u itop-agent`).

Was der Agent unter Linux liest: `/sys/class/dmi/id` (Hersteller, Modell,
Seriennummer), `/etc/os-release`, `/proc/cpuinfo`, `/proc/meminfo`,
`/proc/mounts` + `statfs`, `net.Interfaces()`, `dpkg-query` bzw. `rpm`.
Fehlt eine Quelle, bleibt das Feld leer — nie ein Abbruch.

Gefiltert wird bewusst: Loopback, Docker-Bridges und `veth`-Paare tauchen nicht
als Schnittstellen auf, Pseudo-Dateisysteme nicht als Datenträger. Sonst
wechselte der CMDB-Eintrag bei jedem Containerstart.

`product_serial` ist nur für root lesbar. Im Dienstbetrieb ist das gegeben, im
interaktiven Test als normaler Benutzer bleibt die Seriennummer leer.

Endpunkte:

| Methode | Pfad | Auth | Zweck |
|---|---|---|---|
| POST | `/enroll` | Einmal-Token | GUID gegen Device-Token tauschen |
| POST | `/report` | Device-Token | Meldung entgegennehmen und nach iTop schreiben |
| GET | `/healthz` | — | Lebenszeichen, Anzahl Geräte |
| GET | `/conflicts` | Einmal-Token | Replicas, die iTop nicht zuordnen konnte |

## Bauen und starten

```bash
docker build -t itop-collector:0.2.2 -f deploy/collector/Dockerfile .
```

Konfiguration ausschließlich über Umgebungsvariablen:

| Variable | Bedeutung |
|---|---|
| `COLLECTOR_LISTEN` | Adresse, Default `:8080` |
| `COLLECTOR_ENROLL_TOKEN` | Einmal-Token für das Enrollment (Pflicht) |
| `COLLECTOR_REGISTRY` | Pfad der Geräte-Registry, Default `/var/lib/itop-collector/devices.json` |
| `ITOP_URL` | Basis-URL von iTop |
| `ITOP_USER` / `ITOP_PASSWORD` | iTop-Konto mit Profil *REST Services User* |
| `ITOP_DEFAULT_ORG_ID` | Organisation für neu angelegte CIs (Pflicht) |
| `ITOP_DS_PC` / `ITOP_DS_SERVER` / `ITOP_DS_VM` | IDs der Synchro Data Sources |
| `ITOP_SKIP_TLS_VERIFY` | Zertifikatsprüfung abschalten — nur für Testinstanzen |

`/var/lib/itop-collector` muss ein Volume sein. Ohne das ist nach jedem Neustart
jedes Gerät unbekannt und müsste sich neu registrieren.

Ein lauffähiges Beispiel steht im Stack `itop-test` auf itop.example.internal
(`/opt/itop-test/docker-compose.yml`, Service `itop-collector`, Port 8890).

## IP-Adressen

Der Agent meldet nur **statisch vergebene** IPv4-Adressen. DHCP-Adressen werden
ausgefiltert, weil sie wechseln — jeder Wechsel erzeugte sonst einen weiteren
Eintrag im IPAM, den niemand aufräumt.

Woher der Agent das weiß:

| Plattform | Quelle |
|---|---|
| Linux | Netlink `RTM_GETADDR`, Flag `IFA_F_PERMANENT` — dieselbe Quelle, aus der `ip addr` sein `dynamic` ableitet |
| Windows | `Win32_NetworkAdapterConfiguration.DHCPEnabled`, Zuordnung über die MAC |

Unter Linux bewusst über Netlink statt über einen Aufruf von `ip`: iproute2
fehlt auf Minimal-Images, und `ip -j` gibt es erst ab Version 4.15. Lease-Dateien
wären die dritte Möglichkeit, liegen aber je nach Netzwerkverwaltung woanders
oder gar nicht vor — auf der Testmaschine sind sie leer, obwohl die Adresse per
DHCP kam. Der Kernel weiß es trotzdem.

**Warum das Feld nicht über die Data Source läuft:** die Spaltenliste einer Data
Source ist fest, jede Meldung liefert also jede Spalte. Eine Maschine ohne
statische Adresse schickte einen Leerwert — und der Import würde eine im IPAM
gepflegte Adresse damit **überschreiben**. Schweigen ist aber keine Aussage.
Deshalb setzt der Collector `ipaddress_id` per REST, und nur wenn der Agent
wirklich eine dauerhafte Adresse gemeldet hat.

Zwei Schutzmaßnahmen:

* **Vorhandene Adressen werden gesucht, nicht blind angelegt.** TeeMIP verwaltet
  den Adressraum; eine vom IPAM oder einem Sync-Skript vergebene Adresse wird
  weiterverwendet.
* **Eine Adresse gehört nur einem Gerät.** iTop erzwingt das *nicht* — dieselbe
  `IPv4Address` lässt sich ohne Fehlermeldung an zwei CIs hängen. Meldet eine
  zweite Maschine dieselbe statische IP, verweigert der Collector die
  Verknüpfung und protokolliert, welches CI sie hält. Der häufigste Grund dafür
  ist ein Adresskonflikt im Netz — den will man sehen, nicht in die CMDB
  übernehmen.

## Die drei Regeln, die aus M0 kommen

Diese Punkte sind keine Stilfragen — sie stammen aus Tests gegen eine echte
Instanz und stehen ausführlich in der M0-Auswertung.

**1. Reconciliation nur über `agent_guid`.** iTop verknüpft mehrere
Reconciliation-Attribute mit `AND` und kennt keinen Fallback
(`synchrodatasource.class.inc.php:2203`). „GUID primär, Seriennummer sekundär"
ist in einer Data Source nicht abbildbar.

**2. Reimaging löst der Collector auf, nicht iTop.** Bei unbekannter GUID sucht
`ResolveReimaging` nach der Seriennummer und meldet unter der *bestehenden* GUID
weiter. Ohne diesen Schritt entsteht bei jedem Reimaging eine Dublette — ohne
Fehler, ohne Warnung. Mehrere Treffer werden nicht geraten, sondern als 409
abgewiesen.

**3. Die Zielklasse steht in der Registry, nicht in der Heuristik.** Jede Data
Source reconciled nur innerhalb ihrer `scope_class`. Wechselt ein Gerät die
Klasse, findet die neue Data Source das alte CI nicht und legt ein zweites an.
`ResolveClass` entscheidet deshalb genau einmal und protokolliert nur, wenn die
Heuristik später abweicht.

Dazu zwei Eigenheiten von iTop, über die der erste Versuch stolperte:

* **Fremdschlüssel lassen sich nicht mit freiem Text füllen.** `osfamily_id` und
  `osversion_id` zeigen auf Objekte. Nötig sind beide Seiten: in der Data Source
  `reconciliation_attcode = "name"`, und im Collector `EnsureOSRefs`, das
  fehlende `OSFamily`/`OSVersion` anlegt. Fehlt das, meldet der Import nur
  `Unable to create destination object: No result for the single row query` —
  ohne zu sagen, welches Feld gemeint ist.
* **`primary_key` muss gefüllt sein.** Die Spalte ist die quellseitige Kennung
  des Replicas. Bleibt sie leer, landen alle Zeilen auf demselben Replica und
  überschreiben sich gegenseitig, ohne Fehlermeldung.

## Verifiziert gegen die Testinstanz

| Szenario | Ergebnis |
|---|---|
| Enrollment, danach Meldung | CI angelegt, alle Felder gesetzt |
| Falsches Einmal-Token / kein Device-Token | 401 |
| Geänderte Meldung | `updated: 1`, nur agentgeführte Felder |
| Reimaging (neue GUID, gleiche Seriennummer) | **ein** CI, aktualisiert statt dupliziert |
| Unbekannte VirtualMachine | 409, kein Schreibversuch, Agent wiederholt nicht |
| VM existiert (vom Hypervisor angelegt) | übernommen, `agent_guid` nachgetragen, angereichert |
| Echter Agent auf einer KVM-VM (Linux) | als `VirtualMachine` erkannt und korrekt geroutet |
| Echter Agent auf Windows 11 IoT LTSC | WMI + Registry vollständig, `virtualization: kvm` |
| Windows-Dienst | meldet selbstständig nach 4,5 min, Einträge im Ereignisprotokoll |
| `.deb` im Container installiert | Binary, Unit, Konfiguration korrekt abgelegt |
| Statische IP gemeldet | `IPv4Address` im IPAM angelegt (`status=allocated`) und ans CI gehängt |
| DHCP-Adresse gemeldet | nicht im IPAM angelegt, `ipaddress_id` unberührt |
| DHCP-Maschine mit IPAM-Adresse | IPAM-Zuweisung überlebt die Agent-Meldung |
| Zwei Maschinen, dieselbe statische IP | zweite Verknüpfung verweigert und protokolliert |

## Tests

```bash
docker run --rm -v "$PWD":/src -w /src golang:1.23-alpine \
  sh -c 'gofmt -l . && go vet ./... && go test ./...'
```

Go ist zum Entwickeln nicht lokal nötig — der Container reicht.

## Offen

* **Windows-Agent (M1).** Braucht `itop-agent-skeleton.go`. Plattformneutral ist
  bereits alles außer `collect_*.go`, `identity_*.go` und dem Dienst-Wrapper.
* **Fehlerzählung pro Meldung.** iTop zählt in der Import-Zusammenfassung über
  **alle** Replicas einer Data Source, nicht nur über die gerade gelieferten
  Zeilen. Ein alter, unaufgelöster Replica lässt deshalb jede spätere Meldung
  als fehlerhaft erscheinen. Für eine genaue Aussage müsste der Collector nach
  dem Import den Replica zu diesem `primary_key` gezielt nachlesen.
* **MAC- und IP-Adressen an Server und VM.** `macaddress` und `ipaddress_id`
  gibt es in iTop **nur an `PC`** — an der Instanz nachgemessen. Für Server und
  VMs führt TeeMIP die Adressen über `IPInterface`-Objekte und
  `lnkIPInterfaceToIPAddress`; das ist eine eigene Ausbaustufe.
* **Software-Inventar.** Im Schema vorgesehen, wird noch nicht nach iTop
  geschrieben.
* **Container als CI.** `virtualization: "container"` wird erkannt, aber weiter
  als PC/Server behandelt. Ob Container überhaupt in die CMDB gehören, ist eine
  offene fachliche Frage — bis dahin lieber sichtbar als still verworfen.
* **Paketierung (M5).** `.deb`/`.rpm` via nfpm, systemd-Unit, MSI, Code-Signing.
