# Rollout-Runbook

**itop-agent in einen bestehenden iTop-Stack einbringen**

Inventar-Agent, Collector und Datenmodell-Erweiterung in eine laufende
iTop-3.2.3-2-Installation unter Portainer. Ein Schritt ändert das
Datenbankschema und erzeugt Ausfallzeit — er steht bewusst hinter dem Backup.

| | |
|---|---|
| Ziel-iTop | 3.2.3-2 |
| Collector | Service im vorhandenen Stack |
| Ausfallzeit | ca. 10–20 min (Schritt 3) |
| Gesamtdauer | ca. 1,5–2,5 h ohne Rollout |

## Reihenfolge

| # | Schritt | Dauer |
|---|---|---|
| 0 | [Voraussetzungen prüfen](#0-voraussetzungen-prüfen) | 15 min |
| 1 | [Backup](#1-backup) | 10 min, Pflicht |
| 2 | [iTop-Konto für den Collector](#2-itop-konto-für-den-collector) | 5 min |
| 3 | [Extension einspielen und Setup-Lauf](#3-extension-einspielen-und-setup-lauf) | 20 min, **mit Ausfall** |
| 4 | [Synchro Data Sources anlegen](#4-synchro-data-sources-anlegen) | 10 min |
| 5 | [Softwaregruppen mit Mustern versehen](#5-softwaregruppen-mit-mustern-versehen) | 15 min |
| 6 | [Collector in den Stack](#6-collector-in-den-stack) | 15 min |
| 7 | [Agenten ausrollen](#7-agenten-ausrollen) | laufend |
| 8 | [Abnahme](#8-abnahme) | 15 min |
| 9 | [Rückweg](#9-rückweg) | |

---

## 0. Voraussetzungen prüfen

Vier Dinge vorab klären. Jedes davon hat in der Testumgebung Zeit gekostet, weil
es erst später auffiel.

### Läuft der iTop-Cron, und unter welchem Konto?

Ohne laufenden Cron passiert bei Synchro Data Sources nichts — Importe bleiben
liegen. Das Konto muss in iTop das Profil **Administrator** haben; *SuperUser
genügt nicht*. Belegt in `webservices/cron.php:481`
(`UserRights::IsAdministrator()`). Die Fehlermeldung lautet nur
„Access restricted to administrators" und nennt das fehlende Profil nicht.

```bash
docker exec <stack>-itop-cron-1 id
docker logs --tail 20 <stack>-itop-cron-1
```

> **Wichtig:** Der Cron-Container **muss als `www-data` laufen**, nicht als root.
> Er teilt sich das Volume `itop-data` mit dem Webcontainer. Läuft er als root,
> legt er root-eigene Cache-Dateien an, und Apache stürzt danach beim Twig-Cache
> ab: *„Unable to create the cache directory …/data/cache-production/twig/templates/…"*.
> Der Entrypoint korrigiert die Rechte nur beim Start — ein dauerhaft als root
> laufender Cron erzeugt sie immer wieder neu.
>
> Zeigt `id` ein `uid=0(root)`, im Stack beim Cron-Service `user: "www-data"`
> ergänzen und neu ausrollen.

### Ist `mysqldump` im iTop-Image?

Der Setup-Lauf in Schritt 3 bricht sonst in `CheckBackupPrerequisites` ab
(*„mysqldump could not be executed (retcode=127)"*), und der nächtliche
Hintergrund-Task `BackupExec` scheitert still.

```bash
docker exec <stack>-itop-1 sh -c 'command -v mysqldump || echo FEHLT'
```

Fehlt es, im Dockerfile `mariadb-client` ergänzen und ein neues Image mit
**neuem Tag** bauen. Das Paket legt `mysqldump` als Symlink auf `mariadb-dump`
an — genau den Namen sucht iTop.

### Wie sind Extensions eingebunden?

Im aktuellen Aufbau stecken die Extensions *im Image*, und auf
`/var/www/html/extensions/` liegt bewusst kein Volume — ein nicht leeres Named
Volume würde die gebackenen Extensions verdecken, ohne Fehlermeldung. Für die
neue Erweiterung gibt es zwei Wege:

| Weg | Wann | Aufwand |
|---|---|---|
| **Bind-Mount** je Extension | Regelfall; gleiches Muster wie `custom-service-software` | Stack-Änderung, kein Rebuild |
| **Ins Image backen** | wenn die Extension stabil ist | Rebuild mit neuem Tag |

Dieses Runbook nutzt den Bind-Mount — er erlaubt Änderungen am Datenmodell ohne
Rebuild und ist damit für die Einführungsphase richtig.

### Erreichen die Agenten den Collector?

Der Collector nimmt Meldungen auf einem Port des Docker-Hosts entgegen. Firewall
und Netzwege von den Client-Netzen dorthin vorher klären — sonst fällt es erst
beim Rollout auf.

---

## 1. Backup

> **Nicht überspringen.** Schritt 3 ändert das Datenbankschema. Ein Setup-Lauf
> lässt sich nicht rückgängig machen — der Rückweg führt ausschließlich über
> dieses Backup.

```bash
# Datenbank
mkdir -p /opt/itop/backups
docker exec -e MYSQL_PWD=<db-passwort> <stack>-itop-db-1 \
  mariadb-dump -u<db-benutzer> \
    --single-transaction --routines --triggers \
    --default-character-set=utf8mb4 \
    <db-name> \
  | gzip > /opt/itop/backups/itop-$(date +%Y%m%d-%H%M%S).sql.gz

# Vollständigkeit prüfen — ein abgebrochener Dump ist gzip-seitig gültig!
zcat /opt/itop/backups/itop-*.sql.gz | tail -3
```

> **Prüfen:** Die letzte Zeile muss `-- Dump completed on …` lauten. Fehlt sie,
> ist der Dump abgebrochen — und `gzip` meldet das nicht.

```bash
# Volumes: conf, env-production und data
docker run --rm \
  -v <stack>_itop-conf:/v/conf:ro \
  -v <stack>_itop-env:/v/env:ro \
  -v <stack>_itop-data:/v/data:ro \
  -v /opt/itop/backups:/backup \
  debian:bookworm-slim \
  tar czf /backup/itop-volumes-$(date +%Y%m%d-%H%M%S).tgz -C /v .
```

`itop-env` muss unbedingt mit — dort liegt das kompilierte Datenmodell. Ohne es
passt nach einem Rückweg der Code nicht mehr zum Schema.

---

## 2. iTop-Konto für den Collector

In iTop unter *Administration → Benutzerkonten* ein Konto anlegen, z. B.
`syncagent`, mit dem Profil **REST Services User**.

> **Rechtetrennung:** Das Collector-Konto braucht *nur* REST Services User. Der
> Cron braucht *Administrator*. Beides in einem Konto zusammenzufassen macht die
> Collector-Zugangsdaten faktisch zu Admin-Zugangsdaten — in der Testumgebung
> bewusst so entschieden, produktiv sollten es zwei getrennte Konten sein.

Anschließend die Organisation ermitteln, unter der neue CIs entstehen sollen.
Der Collector braucht deren numerische ID:

```bash
curl -sk -X POST "https://<itop>/webservices/rest.php?version=1.3" \
  -d "auth_user=syncagent" --data-urlencode "auth_pwd=<passwort>" \
  --data-urlencode 'json_data={"operation":"core/get","class":"Organization",
    "key":"SELECT Organization","output_fields":"id,name"}'
```

---

## 3. Extension einspielen und Setup-Lauf

> **Ausfallzeit.** Dieser Schritt kompiliert das Datenmodell neu und ändert das
> Datenbankschema. iTop ist dabei nicht benutzbar. Vorher ankündigen, Backup aus
> Schritt 1 muss vorliegen.

### 3.1 Extension ablegen

Die Erweiterung `custom-agent-inventory` ergänzt drei Felder: an `FunctionalCI`
die beiden Felder `agent_guid` (Abgleichschlüssel) und `agent_last_seen`, an
`Software` das Feld `agent_match_patterns` für die Zuordnung des
Softwareinventars.

```bash
mkdir -p /opt/itop/extensions
cd /opt/itop/extensions
# aus dem Repository: deploy/itop-extensions/custom-agent-inventory
ls custom-agent-inventory/
#   datamodel.custom-agent-inventory.xml
#   module.custom-agent-inventory.php
#   dictionaries/
```

> **Warum an FunctionalCI:** Die Zielklassen sind `PC`, `Server` und
> `VirtualMachine`. `PhysicalDevice` deckt PC und Server ab, aber nicht
> VirtualMachine — die hängt unter `VirtualDevice`. Gemeinsamer Vorfahr aller
> drei ist erst `FunctionalCI`.

### 3.2 Mount im Stack ergänzen

In Portainer die Stack-Definition bearbeiten und beim Service `itop` **und** beim
Service `itop-cron` ergänzen:

```yaml
    volumes:
      # … vorhandene Einträge …
      - /opt/itop/extensions/custom-agent-inventory:/var/www/html/extensions/custom-agent-inventory
```

> **Beide Services.** Der Cron-Container braucht denselben Mount. Fehlt er dort,
> arbeitet der Cron mit einem anderen Datenmodell als das Frontend.

Stack neu ausrollen. **„Re-pull image" nicht anhaken**, wenn das iTop-Image nur
lokal existiert — ein Pull-Versuch gegen Docker Hub bricht das Deployment ab.

### 3.3 Setup-Lauf

Im Browser `https://<itop>/setup/` aufrufen.

1. Welcome → *Continue*
2. *Upgrade an existing iTop instance*
3. Bei der Modulauswahl **alle bisher installierten Extensions anhaken**, plus
   `custom-agent-inventory`
4. Durchlaufen lassen

> **Alle Module anhaken.** iTop kann bereits installierte Module nicht
> deinstallieren. Verschwindet eines aus der Auswahl oder vom Dateisystem, bricht
> der Setup-Lauf ab.

Vor Abschluss des Setups wirft die Oberfläche Fehler — `env-production` ist noch
gegen das alte Modell kompiliert. Das ist erwartet.

### 3.4 Nachprüfen

```bash
docker exec <stack>-itop-1 ls /var/www/html/env-production/custom-agent-inventory/
docker exec <stack>-itop-db-1 mariadb -u<user> -p<pw> -D <db> \
  -e "SHOW COLUMNS FROM functionalci LIKE 'agent%';
      SHOW COLUMNS FROM software LIKE 'agent%';"
```

Erwartet: `agent_guid` (varchar), `agent_last_seen` (datetime) und
`agent_match_patterns` (text).

> **Nach jedem Setup-Lauf.** Läuft ein nginx vor iTop und steht in `proxy_pass`
> ein statischer Name, liefert er nach dem Redeploy **502** — er hat die alte
> Container-IP festgehalten. Dann `docker restart <stack>-itop-proxy-1`.
> Dauerhaft behebt das ein `resolver 127.0.0.11 valid=10s;` mit einer Variablen
> in `proxy_pass`; mit Variable muss `$request_uri` explizit angehängt werden,
> sonst landen alle Anfragen auf `/`.

Kennt die laufende Instanz die neuen Attribute nicht (`invalid attribute code`),
hält der Modell-Cache noch den alten Stand: `docker restart <stack>-itop-1`.

---

## 4. Synchro Data Sources anlegen

Eine Data Source je Zielklasse — `scope_class` ist ein Einzelwert, mehrere
Klassen in einer Quelle gehen nicht.

```bash
python3 mk_datasource.py PC             synchro_data_agent_pc
python3 mk_datasource.py Server         synchro_data_agent_server
python3 mk_datasource.py VirtualMachine synchro_data_agent_vm
```

Das Skript legt die Quelle an und setzt die Attribut-Policies. Es liegt unter
`deploy/itop-stack/mk_datasource.py` und erwartet Zugangsdaten in `itop_rest.py`.

| Gruppe | Attribute | Policy |
|---|---|---|
| Abgleichschlüssel | `agent_guid` | `write_if_empty` + `reconcile` |
| Agent ist führend | Name, Seriennummer, CPU, RAM, Hersteller, Modell, OS, MAC, Last-Seen | `master_locked` |
| Nur bei Anlage | `org_id` | `write_if_empty` |
| Agent fasst nie an | Standort, Kritikalität, Inventarnummer, Beschreibung, Status, Verknüpfungen | `update=0` |

> **Klassenspezifische Defaults entfernen.** Jede Zielklasse bringt eigene
> Reconciliation-Vorgaben mit. `VirtualMachine` setzt zum Beispiel
> `virtualhost_id` — bleibt das Flag stehen, ist es ein zweiter, mit `AND`
> verknüpfter Schlüssel, und der Import scheitert an *„Could not reconcile on
> null value for attribute 'virtualhost_id'"*. Das Skript räumt das auf; bei
> manueller Anlage selbst prüfen.

### Fremdschlüssel über den Namen abgleichen

`osfamily_id` und `osversion_id` zeigen auf Objekte, nicht auf Text. Ohne diese
Einstellung meldet der Import nur *„Unable to create destination object: No
result for the single row query"* — ohne zu sagen, welches Feld gemeint ist. Das
Skript setzt `reconciliation_attcode = name`; der Collector legt fehlende
`OSFamily`/`OSVersion` selbst an.

Zum Schluss die IDs der drei Quellen notieren — sie werden in Schritt 6
gebraucht.

---

## 5. Softwaregruppen mit Mustern versehen

> **Ausgangslage:** Die Softwaregruppen **existieren in der Produktion bereits**.
> Dieser Schritt legt nichts an — er hängt den vorhandenen Katalogeinträgen nur
> die Zuordnungsmuster an.

Der Agent meldet, was das Gerät hergibt: „Microsoft .NET Framework 4.8.1",
„ASP.NET Core Runtime 8.0.11", „Google Chrome". In der CMDB soll davon nur die
Gruppe ankommen. Die Regel dafür steht am Katalogeintrag selbst, im Feld
`agent_match_patterns` — nicht im Collector. Eine Gruppe ergänzen oder ein Muster
nachschärfen passiert damit in iTop, ohne dass etwas neu ausgerollt wird.

### 5.1 Ist-Zustand ansehen

```bash
python3 software_groups.py --list
```

Zeigt den Katalog und markiert mit `[M]`, welche Einträge schon Muster haben.
Die Namen in der Liste sind maßgeblich — sie müssen mit denen im Skript
übereinstimmen.

### 5.2 Probelauf

```bash
python3 software_groups.py --dry-run
```

Das Skript ordnet in drei Stufen zu, damit Schreibweisen nicht zu Dubletten
führen:

| Stufe | Bedingung | Verhalten |
|---|---|---|
| 1 | exakter Name | Muster werden gesetzt |
| 2 | gleicher Name ohne Groß-/Kleinschreibung und Mehrfach-Leerzeichen | gesetzt, *der Unterschied wird gemeldet* |
| 3 | kein Treffer | **gemeldet, nicht angelegt** |

> **Nichts blind anlegen.** Das Skript legt standardmäßig **keine** Gruppen an.
> Ein zweiter Katalogeintrag, nur weil ein Name minimal anders geschrieben ist,
> wäre in einer gepflegten CMDB schlimmer als eine fehlende Zuordnung.
>
> Meldet der Probelauf Einträge als „nicht gefunden", heißen sie in iTop
> wahrscheinlich anders. Dann die Namen **im Skript** an den Katalog anpassen —
> nicht umgekehrt. Nur wenn eine Gruppe wirklich fehlt, mit `--create-missing`
> bewusst anlegen.

### 5.3 Setzen

```bash
python3 software_groups.py
```

Der Lauf ist wiederholbar: beim zweiten Mal meldet er alles als „unveraendert".

### Mustersyntax

| Zeile | Bedeutung |
|---|---|
| `.net` | trifft, wenn der Text im gemeldeten Namen vorkommt (Groß-/Klein egal) |
| `!Java Auto Updater` | schließt aus — schlägt jeden Einschluss |
| `/^microsoft edge( \|$)/` | regulärer Ausdruck, wenn ein Teiltreffer zu weit greift |
| `# …` | Kommentar |

Zwei Muster brauchen Sorgfalt: Der Punkt in `.net` ist wesentlich — ohne ihn
träfe das Muster auch „Telnet". Und `Edge` allein wäre zu weit gefasst, es steckt
auch in „Edge Diagnostics Adapter"; dafür der reguläre Ausdruck.

### Was daraus in iTop entsteht

Je Gruppe, deren Muster auf *irgendein* installiertes Programm passt, genau
**eine** Verknüpfung am CI. Zwanzig .NET-Versionen ergeben eine Verknüpfung,
nicht zwanzig. Verschwindet die Software, verschwindet die Verknüpfung.

> **Geprüft.** Von Hand gepflegte Verknüpfungen bleiben unberührt — der Collector
> fasst nur Verknüpfungen zu Katalogeinträgen *mit* Mustern an. Und ein leeres
> Softwareinventar entfernt nichts: ältere Agenten senden das Feld nicht, ein
> hängender Paketmanager liefert nichts, und Schweigen darf nicht als „nichts
> installiert" gelten.

---

## 6. Collector in den Stack

### 6.1 Image bereitstellen

```bash
# auf einem Rechner mit Docker und Repo-Zugriff
make image VERSION=0.8.0
docker save itop-collector:0.8.0 | gzip > itop-collector-0.8.0.tar.gz

# auf dem Produktiv-Host
docker load < itop-collector-0.8.0.tar.gz
```

### 6.2 Service ergänzen

Der Collector läuft im selben Netz wie iTop und spricht den Webcontainer
**direkt** an — nicht über den nginx. Das spart die TLS-Terminierung im internen
Verkehr.

```yaml
  itop-collector:
    image: itop-collector:0.8.0
    depends_on:
      - itop
    environment:
      - COLLECTOR_LISTEN=:8080
      - COLLECTOR_ENROLL_TOKEN=${COLLECTOR_ENROLL_TOKEN}
      - COLLECTOR_REGISTRY=/var/lib/itop-collector/devices.json
      - ITOP_URL=http://itop:80
      - ITOP_USER=${ITOP_USER}
      - ITOP_PASSWORD=${ITOP_PASSWORD}
      - ITOP_DEFAULT_ORG_ID=<org-id aus Schritt 2>
      - ITOP_DS_PC=<id>
      - ITOP_DS_SERVER=<id>
      - ITOP_DS_VM=<id>
    volumes:
      - itop-collector-data:/var/lib/itop-collector
    ports:
      - "8890:8080"
    networks:
      - itop
    restart: unless-stopped
```

Und bei den Volumes am Ende der Datei:

```yaml
volumes:
  itop-collector-data:
```

> **Registry braucht ein Volume.** `/var/lib/itop-collector` hält die Zuordnung
> Gerät → Token. Ohne Volume ist nach jedem Neustart *jedes* Gerät unbekannt und
> müsste sich neu registrieren.

Das Einmal-Token erzeugen und in den Stack-Umgebungsvariablen hinterlegen — nicht
in die Stack-Definition schreiben:

```bash
openssl rand -hex 32
```

### 6.3 Prüfen

```bash
curl -s http://<host>:8890/healthz
# {"devices":0,"status":"ok"}
docker logs <stack>-itop-collector-1
```

---

## 7. Agenten ausrollen

### Windows — MSI

```powershell
msiexec /i itop-agent-0.8.0.msi /qn `
  COLLECTORURL=https://<collector>:8890 `
  ENROLLTOKEN=<einmal-token>
```

Das Paket legt die Datei nach `C:\Program Files\itop-agent\`, richtet den Dienst
ein, schreibt die Konfiguration nach `HKLM\SOFTWARE\iTopAgent` und registriert
die Ereignisquelle. Der Dienst registriert sich beim ersten Start selbst und
löscht das Einmal-Token danach.

> **Signatur und interne CA.** Die Artefakte sind mit einem Zertifikat der
> internen `OT-CA` signiert. Windows verwirft die Kette auf jeder Maschine, die
> diese CA nicht kennt — also auf genau den nicht domänengebundenen Rechnern, für
> die der Agent gebaut wurde. Dort vorher ausrollen:
>
> ```powershell
> Import-Certificate -FilePath OT-CA.crt -CertStoreLocation Cert:\LocalMachine\Root
> ```
>
> Auf domänengebundenen Maschinen verteilt die GPO die CA ohnehin.

### Linux — .deb / .rpm

```bash
dpkg -i itop-agent_0.8.0_amd64.deb
$EDITOR /etc/itop-agent.conf        # ITOP_COLLECTOR_URL setzen
itop-agent -enroll <einmal-token>
systemctl start itop-agent
```

Das Paket aktiviert den Dienst, **startet ihn aber nicht** — ohne Device-Token
würde er nur scheitern.

> **Vor dem Rollout prüfen.** `itop-agent -collect` sammelt einmal und gibt JSON
> aus, ohne zu senden. Braucht weder Collector noch Registrierung — der
> schnellste Weg zu sehen, was der Agent auf einer bestimmten Maschine überhaupt
> erkennt.

### Was gemeldet wird

Hostname, Domäne, Hersteller, Modell, Seriennummer, CPU, RAM, Betriebssystem,
Netzwerkschnittstellen, Datenträger und das Softwareinventar. Bewusst
ausgefiltert: OEM-Platzhalter-Seriennummern, Docker-Bridges und `veth`-Paare,
Pseudo-Dateisysteme — und **per DHCP vergebene IP-Adressen**, weil sie wechseln
und das IPAM zumüllen würden.

---

## 8. Abnahme

- [ ] Ein Testgerät je Plattform registriert und gemeldet; CI in iTop vorhanden
- [ ] Alle vom Agent geführten Felder gefüllt (CPU, RAM, OS, Seriennummer)
- [ ] Ein von Hand gepflegtes Feld (z. B. Standort) überlebt eine erneute Meldung
- [ ] Cron läuft: `ExecAsyncTask` und `CheckStopWatchThresholds` mit steigender Laufzahl
- [ ] `/conflicts` am Collector liefert eine leere Liste
- [ ] Softwaregruppen am Test-CI zugeordnet, jede höchstens einmal
- [ ] `/unmatched` durchgesehen und offensichtliche Lücken ergänzt
- [ ] Nach ~24 h: `agent_last_seen` hat sich ohne Zutun erneuert

```bash
# Konflikte, die ein Mensch auflösen muss
curl -s -H "Authorization: Bearer <einmal-token>" http://<host>:8890/conflicts

# Programmnamen ohne Gruppe, absteigend nach Häufigkeit
curl -s -H "Authorization: Bearer <einmal-token>" http://<host>:8890/unmatched

# Geräte, die sich lange nicht gemeldet haben (Triage-Liste)
# als gespeicherte Abfrage in iTop anlegen:
SELECT PC WHERE agent_last_seen < DATE_SUB(NOW(), INTERVAL 30 DAY)
```

> **Keine automatische Obsoleszenz.** Die Data Sources stehen auf
> `delete_policy = ignore`. Abwesenheit ist kein Beleg für Ausserbetriebnahme —
> ein Notebook im Urlaub und ein verschrottetes Gerät senden dasselbe, nämlich
> nichts. Die Bewertung bleibt beim Menschen, die Abfrage oben ist die
> Triage-Liste.

---

## 9. Rückweg

Je nachdem, wie weit der Rollout gediehen ist:

| Umfang | Vorgehen |
|---|---|
| Nur Collector zurücknehmen | Service aus dem Stack entfernen, neu ausrollen. iTop bleibt unberührt; die Datenfelder bleiben stehen und stören nicht. |
| Extension zurücknehmen | Nicht ohne Weiteres möglich — iTop kann installierte Module nicht deinstallieren. Die Felder bleiben im Schema. Das ist unkritisch: sie sind nullable und tauchen in keinem Formular auf. |
| Vollständig zurück | Volumes aus Schritt 1 zurückspielen — **`itop-env` muss mit zurück** — und die Datenbank aus dem Dump wiederherstellen. |

```bash
zcat /opt/itop/backups/itop-<stempel>.sql.gz \
  | docker exec -i -e MYSQL_PWD=<pw> <stack>-itop-db-1 mariadb -u<user> <db>
docker restart <stack>-itop-proxy-1
```

> **Gerätekennung nicht löschen.** Bei Deinstallation eines Agents bleiben GUID
> und Device-Token bewusst stehen — unter Windows in `HKLM\SOFTWARE\iTopAgent`,
> unter Linux in `/var/lib/itop-agent`. Werden sie entfernt, bekommt die Maschine
> bei der nächsten Installation eine neue GUID und damit ein *zweites* CI in
> iTop.

---

Die Entwurfsentscheidungen hinter Reconciliation, Reimaging-Auflösung und
Klassen-Routing stehen in [M0-Auswertung.md](M0-Auswertung.md).
