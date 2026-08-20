# iTop — Extension custom-device-network

Ergänzt die Geräteklassen um **MAC-Adresse** und **DNS-Name**.

Betroffen sind die sechs angeforderten Klassen `NAS`, `Printer`,
`VirtualMachine`, `Server`, `PC` und `NetworkDevice` — durch Vererbung
zusätzlich vier weitere, siehe unten.

> **Kurzfassung:** Nach dem Einspielen trägt jede der sechs Klassen die Felder
> `macaddress` und `dns_name`. `PC` und `Printer` behalten dabei ihr
> vorhandenes Kernfeld — es wird nicht angefasst und nicht dupliziert.

## Feldübersicht

| Feld | Typ | Pflicht | Prüfmuster |
|---|---|---|---|
| `macaddress` | String | nein | `AA:BB:CC:DD:EE:FF`, `AA-BB-CC-DD-EE-FF`, `AABBCCDDEEFF` oder leer |
| `dns_name` | String | nein | FQDN oder Kurzname, Unterstriche erlaubt, oder leer |

Beide Prüfmuster sind bewusst nachsichtig gehalten. Ein strengeres würde
legitime Bestandsdaten abweisen und die Pflege behindern; gar keines würde jeden
Tippfehler durchlassen.

Der Leerwert ist ausdrücklich erlaubt — sonst wäre das Feld faktisch Pflicht und
ließe sich an bestehenden CIs nicht speichern.

## Wo die Felder hängen und warum

Die Vererbung der Zielklassen in iTop 3.2:

```
FunctionalCI
├── PhysicalDevice
│   ├── ConnectableCI ─┬─ PC          ← hat macaddress bereits
│   │                  ├─ Printer     ← hat macaddress bereits
│   │                  └─ DatacenterDevice ─┬─ Server
│   │                                       ├─ NAS
│   │                                       └─ NetworkDevice
│   └── Peripheral     ← hat macaddress bereits, anderer Ast
└── VirtualDevice ── VirtualMachine
```

Daraus ergeben sich **zwei unterschiedliche Einhängepunkte**, je Feld einer:

| Feld | Definiert an | Wirkt auf |
|---|---|---|
| `dns_name` | `ConnectableCI`, `VirtualDevice` | alle sechs Klassen |
| `macaddress` | `DatacenterDevice`, `VirtualDevice` | Server, NAS, NetworkDevice, VirtualMachine |

### Warum `macaddress` nur teilweise ergänzt wird

`PC`, `Printer` und `Peripheral` bringen das Feld von Haus aus mit. Dort wird
nichts angefasst — ein zweites MAC-Feld daneben wäre eine Dublette, bei der
niemand mehr weiß, welches gilt. Ergänzt wird ausschließlich, wo es fehlt.

### Warum der Attributcode derselbe ist

Das neue Feld heißt bewusst ebenfalls `macaddress` und nicht etwa
`mac_address`. Die Klassen liegen in getrennten Ästen, es kollidiert also
nichts — und am Ende trägt jede der sechs Klassen ein Feld mit demselben Namen.
Zwei Feldnamen, die sich nur um einen Unterstrich unterscheiden, wären eine
Falle, in die früher oder später jemand beim Schreiben einer OQL-Abfrage tappt.

### Konsequenz für Abfragen

Weil `macaddress` an mehreren Stellen definiert ist, gibt es **keine gemeinsame
Oberklasse**, die es trägt. Eine Abfrage über alle Gerätetypen muss deshalb je
Klasse gestellt werden:

```sql
SELECT PC WHERE macaddress = 'AA:BB:CC:DD:EE:FF'
SELECT Server WHERE macaddress = 'AA:BB:CC:DD:EE:FF'
SELECT VirtualMachine WHERE macaddress = 'AA:BB:CC:DD:EE:FF'
```

Für `dns_name` geht es dagegen in einer Abfrage — sie deckt PC, Printer und den
gesamten `DatacenterDevice`-Zweig ab:

```sql
SELECT ConnectableCI WHERE dns_name = 'srv-01.example.internal'
```

### Mitvererbte Klassen

Neben den sechs angeforderten erhalten vier weitere Klassen die Felder:

| Klasse | über | Felder |
|---|---|---|
| `SANSwitch` | `DatacenterDevice` | beide |
| `StorageSystem` | `DatacenterDevice` | beide |
| `TapeLibrary` | `DatacenterDevice` | beide |
| `VirtualHost` | `VirtualDevice` | beide |

Bewusst in Kauf genommen: es sind ebenfalls netzangeschlossene Geräte, bei denen
beide Felder sinnvoll sind. Die Alternative wäre, die Felder sechsmal einzeln zu
definieren — dann wären es sechs *verschiedene* Attribute, und auch `dns_name`
ließe sich nicht mehr klassenübergreifend abfragen.

## Installation

> **Achtung:** Der Setup-Lauf ändert das Datenbankschema. iTop ist dabei nicht
> benutzbar. Vorher ankündigen und **Backup anlegen**.

### 1. Backup

```bash
docker exec -e MYSQL_PWD=<db-passwort> <stack>-itop-db-1 \
  mariadb-dump -u<db-benutzer> \
    --single-transaction --routines --triggers \
    --default-character-set=utf8mb4 <db-name> \
  | gzip > /opt/itop/backups/itop-$(date +%Y%m%d-%H%M%S).sql.gz

# Vollständigkeit prüfen — ein abgebrochener Dump ist gzip-seitig gültig!
zcat /opt/itop/backups/itop-*.sql.gz | tail -3
```

Die letzte Zeile muss `-- Dump completed on …` lauten.

### 2. Extension ablegen

```bash
mkdir -p /opt/itop/extensions
# Inhalt aus dem Repository: deploy/itop-extensions/custom-device-network
ls /opt/itop/extensions/custom-device-network/
#   datamodel.custom-device-network.xml
#   module.custom-device-network.php
#   dictionaries/
```

### 3. Mount im Stack ergänzen

In Portainer die Stack-Definition bearbeiten, beim Service `itop` **und** beim
Service `itop-cron`:

```yaml
    volumes:
      # … vorhandene Einträge …
      - /opt/itop/extensions/custom-device-network:/var/www/html/extensions/custom-device-network
```

> **Beide Services.** Fehlt der Mount beim Cron-Container, arbeitet dieser mit
> einem anderen Datenmodell als das Frontend.

Stack neu ausrollen. **„Re-pull image" nicht anhaken**, wenn das iTop-Image nur
lokal existiert — ein Pull-Versuch gegen Docker Hub bricht das Deployment ab.

### 4. Setup-Lauf

`https://<itop>/setup/` aufrufen → *Upgrade an existing iTop instance* → bei der
Modulauswahl **alle bisher installierten Extensions anhaken**, plus
`custom-device-network`.

> iTop kann installierte Module nicht deinstallieren. Verschwindet eines aus der
> Auswahl, bricht der Setup-Lauf ab.

### 5. Nachprüfen

```bash
docker exec <stack>-itop-db-1 mariadb -u<user> -p<pw> -D <db> -e "
  SHOW COLUMNS FROM datacenterdevice LIKE 'macaddress';
  SHOW COLUMNS FROM connectableci LIKE 'dns_name';
  SHOW COLUMNS FROM virtualdevice LIKE '%';"
```

Erwartet: `macaddress` an `datacenterdevice` und `virtualdevice`, `dns_name` an
`connectableci` und `virtualdevice`.

> **Wenn iTop die Felder nicht kennt** (`invalid attribute code`), hält der
> Modell-Cache noch den alten Stand: `docker restart <stack>-itop-1`.

## Verifiziert

Auf der Testinstanz (iTop 3.2.3-2) nachgemessen:

| Prüfung | Ergebnis |
|---|---|
| Alle sechs Zielklassen tragen beide Felder | ✔ |
| `PC` und `Printer` unverändert, kein zweites MAC-Feld | ✔ |
| `Peripheral` unberührt | ✔ |
| Mitvererbung an die vier weiteren Klassen | ✔ |
| MAC: drei Schreibweisen und Leerwert angenommen | ✔ |
| MAC: zu kurz, nicht hexadezimal, Freitext abgewiesen | ✔ |
| DNS: FQDN, Kurzname, Unterstrich, Leerwert angenommen | ✔ |
| DNS: führender Bindestrich, Leerzeichen abgewiesen | ✔ |

## Rückweg

Eine Extension lässt sich in iTop **nicht deinstallieren** — installierte Module
bleiben registriert, und die Spalten bleiben im Schema.

Das ist unkritisch: beide Felder sind nullable und tauchen in keinem Formular
auf, solange sie nicht in die Ansichten aufgenommen werden. Wer wirklich
zurückmuss, spielt Datenbank und Volumes aus dem Backup von Schritt 1 zurück —
**`itop-env` muss mit zurück**, sonst passt der kompilierte Code nicht mehr zum
Schema.

## Update-Sicherheit

Die Datei definiert ausschließlich **neue** Felder (`_delta="define"`) an
bestehenden Klassen (`_delta="must_exist"`). Kein Core-Feld wird überschrieben —
ein iTop-Update kann hier nicht kollidieren.

## Verwandte Seiten

* Repository: `github.com/JxxKal/itop-collector`, Verzeichnis
  `deploy/itop-extensions/custom-device-network`
* Extension `custom-agent-inventory` — `agent_guid`, `agent_last_seen` und
  `agent_match_patterns`
* Rollout-Runbook für Agent und Collector: `docs/Rollout-Runbook.md`
