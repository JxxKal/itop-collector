# Überblick

Was in diesem Repository liegt und wie es zusammenhängt.

```
┌─────────────┐   HTTPS/JSON    ┌───────────┐   synchro_import   ┌──────┐
│ itop-agent  │ ──────────────► │ Collector │ ─────────────────► │ iTop │
│ (Win/Linux) │  Device-Token   │   (Go)    │  Service-Account   │ CMDB │
└─────────────┘                 └───────────┘                    └──────┘
```

| Verzeichnis | Inhalt |
|---|---|
| `cmd/agent/` | Agent: sammelt und meldet |
| `cmd/collector/` | Collector: nimmt Meldungen an, schreibt nach iTop |
| `internal/report/` | Meldeformat v3 (gemeinsames JSON-Schema) |
| `internal/collect/` | Datensammlung — Linux (DMI, /proc, Netlink, dpkg) und Windows (WMI, Registry) |
| `internal/identity/` | GUID und Device-Token: `/var/lib/itop-agent` bzw. `HKLM\SOFTWARE\iTopAgent` |
| `internal/config/` | Konfiguration: Umgebung, dann Registry bzw. `/etc/itop-agent.conf` |
| `internal/push/` | HTTPS-Push mit CA-Pinning, Retry und Backoff |
| `internal/sched/` | Takt: 24 h + Jitter, verzögerter Erststart |
| `internal/service/` | Windows-Dienst (svc + Eventlog); unter Linux systemd |
| `internal/collectorsrv/` | Routing, Reimaging-Auflösung, VM-Übernahme, IP-Pflege, CSV, iTop-Anbindung |
| `deploy/collector/` | Dockerfile des Collectors (statisch, auf `scratch`, ~6,5 MB) |
| `deploy/linux/` | systemd-Unit, nfpm-Definition für `.deb`/`.rpm` |
| `deploy/itop-stack/` | Kompletter iTop-Stack: Dockerfile, Compose, nginx, PHP, Datasource-Skript |
| `deploy/itop-extensions/` | iTop-Datenmodell-Erweiterung `custom-agent-inventory` |
| `docs/` | Auswertung von Meilenstein M0 |

## Schnellstart

```bash
cp .env.example .env && $EDITOR .env

# iTop-Stack (nur nötig, wenn keine iTop-Instanz vorhanden ist)
cd deploy/itop-stack && docker compose up -d

# Collector-Image bauen
make image
```

Der Agent braucht Go nicht lokal — alle Make-Ziele laufen im Container:

```bash
make all        # Agent für Linux und Windows, Collector
make check      # gofmt, vet für BEIDE Plattformen, Tests
make packages   # .deb und .rpm
```

## Reihenfolge bei der Einrichtung

1. **iTop-Extension einspielen.** `deploy/itop-extensions/custom-agent-inventory`
   nach `extensions/` der iTop-Instanz, dann Setup laufen lassen. Sie ergänzt
   `agent_guid` und `agent_last_seen` an `FunctionalCI` — bewusst dort, weil
   `PhysicalDevice` zwar PC und Server abdeckt, aber nicht `VirtualMachine`.

2. **Synchro Data Sources anlegen.** `deploy/itop-stack/mk_datasource.py` legt je
   Zielklasse eine an und konfiguriert die Attribut-Policies. `scope_class` ist
   ein Einzelwert — eine Data Source pro Klasse ist unvermeidlich.

3. **Collector starten**, IDs der Data Sources in `.env` eintragen.

4. **Agent ausrollen und registrieren:**

   ```powershell
   .\itop-agent.exe -set-url https://collector.example.internal
   .\itop-agent.exe -enroll <einmal-token>
   .\itop-agent.exe -install
   Start-Service itop-agent
   ```

   ```bash
   dpkg -i itop-agent_*.deb
   $EDITOR /etc/itop-agent.conf
   itop-agent -enroll <einmal-token>
   systemctl start itop-agent
   ```

## Warum manches so gebaut ist

Die wichtigsten Entwurfsentscheidungen stammen nicht aus Vorüberlegungen,
sondern aus Tests gegen eine echte iTop-3.2.3-Instanz. Sie stehen ausführlich in
`docs/M0-Auswertung.md` und im Haupt-`README.md`; die Kurzfassung:

* **Reconciliation nur über `agent_guid`.** iTop verknüpft mehrere
  Reconciliation-Attribute mit `AND` und kennt keinen Fallback. „GUID primär,
  Seriennummer sekundär" ist in einer Data Source nicht abbildbar.
* **Reimaging löst der Collector auf.** Sonst entsteht bei jedem Neuaufsetzen
  eine Dublette — ohne Fehler, ohne Warnung.
* **Die Zielklasse steht in der Registry, nicht in der Heuristik.** Ein
  Klassenwechsel würde ein zweites CI erzeugen, weil jede Data Source nur
  innerhalb ihrer `scope_class` sucht.
* **VirtualMachines legt der Agent nicht an.** `virtualhost_id` ist Pflicht und
  dem Gastsystem unbekannt. Der Hypervisor legt an, der Agent ergänzt.
* **Nur statische IP-Adressen.** DHCP-Adressen wechseln und würden das IPAM
  zumüllen. Die IP läuft per REST statt über die Data Source, weil ein Leerwert
  in der CSV eine im IPAM gepflegte Adresse überschreiben würde.

## Sicherheit

`.env` enthält Zugangsdaten und ist per `.gitignore` ausgeschlossen. Im
Repository steht ausschließlich `.env.example` mit Platzhaltern.

Zwei Punkte, die vor dem Produktivbetrieb zu klären sind:

* **Getrennte iTop-Konten** für Cron (braucht Administrator) und Collector
  (braucht nur REST Services User). Ein gemeinsames Konto macht die
  Collector-Zugangsdaten zu Admin-Zugangsdaten.
* **Code-Signing des Agents.** Ein unsignierter Dienst, der Hardware ausliest
  und nach Hause telefoniert, wird von Defender und EDR als Schadsoftware
  eingestuft.
