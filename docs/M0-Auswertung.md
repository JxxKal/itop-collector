# M0 — Fundament validiert

**Instanz:** https://itop.example.internal:8889/ · Datasource `itop-agent PC` (id 1) · 2026-08-13

Ziel laut PROJECT.md: *„Synchro-Datasource in iTop definieren und mit handgebauten
Testdaten das Reconciliation-Verhalten durchspielen (Anlage, Update, Konflikt,
Obsoleszenz). Vor allem anderen Agent-Ausbau."*

---

## Das wichtigste Ergebnis zuerst

**„GUID primär, Seriennummer sekundär" lässt sich in einer Synchro Data Source
nicht abbilden.** Das ist keine Konfigurationsfrage, sondern eine Eigenschaft von
iTop.

Belegt in `synchro/synchrodatasource.class.inc.php:2203`:

```php
$sOQL = 'SELECT '.$oDataSource->GetTargetClass().' WHERE '.implode(' AND ', $aCriterias);
```

Alle als `reconcile` markierten Attribute werden mit **AND** verknüpft. Es gibt
keinen Fallback auf einen zweiten Schlüssel. Zusätzlich (Zeile 2214): ist der Wert
eines Reconciliation-Attributs `NULL`, bricht iTop die Reconciliation für diesen
Datensatz mit Fehler ab.

Für das Projekt heißt das zweierlei:

* Beide Felder als Schlüssel zu markieren bedeutet *„GUID **und** Seriennummer
  müssen übereinstimmen"*. Nach einem Reimaging (neue GUID, gleiche Seriennummer)
  passt dann nichts mehr — es entsteht eine Dublette. Das ist das Gegenteil des
  gewünschten Verhaltens.
* Weil der Agent OEM-Platzhalter-Seriennummern zu leer filtert (PROJECT.md §5),
  würde `serialnumber` als Schlüssel bei genau den Geräten *jede* Reconciliation
  scheitern lassen, die keine brauchbare Seriennummer melden.

**Konsequenz:** Reconciliation nur auf `agent_guid`. Der Reimaging-Fall gehört in
den Collector — vor dem Push per REST nach `serialnumber` suchen und bei Treffer
die vorhandene `agent_guid` weiterverwenden statt die neue zu melden. PROJECT.md
§9.6 sieht diese Logik in `itopsync.go` bereits vor; M0 zeigt, dass sie nicht
optional ist, sondern die einzige Stelle, an der der Fall behandelt werden kann.

Test 3c unten zeigt den Schaden konkret.

---

## Konfiguration der Datasource

| Einstellung | Wert | Warum |
|---|---|---|
| `scope_class` | `PC` | erste Zielklasse; Server/VirtualMachine analog später |
| `reconciliation_policy` | `use_attributes` | |
| Reconciliation-Attribut | **nur** `agent_guid` | siehe oben |
| `action_on_zero` | `create` | unbekannte Maschine anlegen |
| `action_on_one` | `update` | Normalfall |
| `action_on_multiple` | `error` | nicht raten — Konflikt sichtbar machen |
| `delete_policy` | **`ignore`** | Abwesenheit ist kein Beleg für Ausserbetriebnahme — siehe Test 4 |
| `delete_policy_update` | leer | |
| `full_load_periodicity` | 259200 s (3 Tage) | mit `ignore` wirkungslos; Wert passt zum Meldeabstand, falls doch aktiviert |

### Attribut-Policies

| Gruppe | Attribute | `update` | `update_policy` |
|---|---|---|---|
| Schlüssel | `agent_guid` | 1 (+`reconcile`) | `write_if_empty` |
| Agent führend | `name`, `serialnumber`, `cpu`, `ram`, `brand_id`, `model_id`, `osfamily_id`, `osversion_id`, `macaddress`, `agent_last_seen` | 1 | `master_locked` |
| nur bei Anlage | `org_id` | 1 | `write_if_empty` |
| Agent nie | `location_id`, `business_criticity`, `asset_number`, `purchase_date`, `end_of_warranty`, `description`, `move2production`, `status`, `type`, `ipaddress_id`, alle `*_list` | 0 | — |

**Zu `org_id`:** PROJECT.md zählt organisatorische Zuordnungen zu „Agent fasst nie
an". An `PC` ist `org_id` aber Pflichtfeld, und ein Attribut mit `update=0` taucht
in der Synchro-Tabelle gar nicht erst auf — die Anlage würde scheitern. Auflösung:
`update=1` mit `write_if_empty`. Einmal bei der Anlage setzen, danach nie wieder
anfassen. In Test 2 verifiziert.

---

## Testergebnisse

| # | Szenario | Erwartet | Ergebnis |
|---|---|---|---|
| 1 | Anlage, 3 neue Maschinen | 3 CIs angelegt | ✅ 3 angelegt, 0 Fehler |
| 2 | Update inkl. Umbenennung | Agent-Felder aktualisiert, Rest unberührt | ✅ siehe unten |
| 3 | Zwei CIs mit derselben GUID | Fehler, kein Raten | ✅ Replica bleibt `new`, präziser Fehlertext |
| 3b | Dublette entfernt, erneuter Import | Erholung ohne Eingriff | ✅ bindet sauber an das richtige CI |
| 3c | Reimaging: neue GUID, gleiche Seriennummer | — | ⚠️ **Dublette, ohne Fehler oder Warnung** |
| 4 | Maschine meldet sich nicht mehr | `status` → `obsolete` | ⚠️ funktioniert, trifft aber **alle** — siehe unten |
| 5 | VM anlegen, ohne den Hypervisor zu kennen | — | ⚠️ **unmöglich**: `virtualhost_id` ist Pflicht |
| 6 | Maschine der falschen Klasse gemeldet | — | ⚠️ **zweites CI, ohne Fehler** |

### Test 2 im Detail

Der Sync meldete `wks-alpha` unter neuem Namen (`wks-alpha-umbenannt`), mit
neuer CPU, doppeltem RAM und `org_id=2` statt `3`. Vorher hatte ein Mensch
`description` und `status=stock` gesetzt.

Danach:

* Name, CPU, RAM aktualisiert → Agent ist führend ✅
* **wiedergefunden trotz Umbenennung** → die GUID überlebt Umbenennung, wie in
  PROJECT.md §2 angenommen ✅
* `org_id` blieb auf `3`, obwohl die CSV `2` lieferte → `write_if_empty` ✅
* `status=stock` und die Beschreibung unverändert → `update=0` ✅

Ebenfalls bestätigt: CIs aus einer Datasource bleiben für Attribute **außerhalb**
der Datasource von Hand editierbar. Die Sperre wirkt feldweise, nicht auf das
ganze Objekt.

### Test 3 im Detail

Fehlertext im Replica:

```
2 destination objects match the reconciliation criterias:
agent_guid=11111111-1111-4111-8111-111111111111
```

Der Replica bleibt auf `status='new'` mit `dest_id=0`, **kein** CI wird angefasst.
Der Collector kann diesen Zustand über `priv_sync_replica.status_last_error`
auslesen — das ist die Datenquelle für das in §9.6 geforderte Konfliktlogging.

Beim Löschversuch eines synchronisierten CIs meldet iTop:
*„Sie können das Objekt nicht löschen, weil es zur externen Datenquelle
itop-agent PC gehört."* Erst nach Entfernen des Replicas ist es löschbar.

### Test 3c im Detail — der Reimaging-Fall

Eingespielt: neue GUID `4444…`, unveränderte Seriennummer `SN-ALPHA-001`.

```
id=45  wks-alpha-umbenannt  serial=SN-ALPHA-001  guid=1111…  status=stock       desc='Von Hand gepflegt…'
id=49  wks-alpha-umbenannt  serial=SN-ALPHA-001  guid=4444…  status=production  desc=''
```

`Objects created: 1`, **null Fehler, null Warnungen**. Die von Hand gepflegten
Daten bleiben am alten CI zurück, der Agent füttert ab jetzt das neue. Genau der
Datenverlust, den „Seriennummer sekundär" verhindern sollte.

Das ist kein Fehlverhalten von iTop — die Datasource tut, was konfiguriert wurde.
Es zeigt nur, dass die Lücke im Collector geschlossen werden muss.

### Test 4 im Detail — zwei Fallstricke bei der Obsoleszenz

Aufbau: `full_load_periodicity` für den Test auf 60 s gesenkt, Import **ohne** die
beiden alpha-Maschinen, 75 s gewartet, dann `synchro_exec` allein laufen lassen.

Ergebnis: `Objects obsoleted: 4` — **alle** CIs, auch `wks-beta` und `wks-gamma`,
die sich im letzten Import gerade gemeldet hatten.

#### Fallstrick 1: die Frist läuft gegen die Uhr, nicht gegen den letzten Import

`synchrodatasource.class.inc.php`, Berechnung des Stichtags:

```php
if ($this->m_bIsImportPhaseDateKnown) {
    $oLimitDate = clone $this->m_oImportPhaseStartDate;   // Import lief -> ab Importbeginn
} else {
    if ($iFullLoadInterval <= 0) {
        $oLimitDate = new DateTime('1970-01-01');          // Exec allein + Intervall 0 -> nichts tun
    } else {
        $oLimitDate = self::GetDataBaseCurrentDateTime();   // Exec allein -> ab JETZT
    }
}
$oLimitDate->Modify("-$iFullLoadInterval seconds");
```

Läuft `synchro_exec` allein — und genau so läuft es unter iTops Cron —, ist der
Stichtag **jetzt minus `full_load_periodicity`**. Ob überhaupt ein Import
stattgefunden hat, spielt keine Rolle.

**Konsequenz für den Betrieb:** Fällt der Collector länger aus als
`full_load_periodicity`, markiert iTop beim nächsten Cron-Lauf die **komplette
Flotte** als obsolet. Das ist kein Randfall, sondern die normale Folge eines
Collector-Ausfalls, eines Netzproblems oder eines abgelaufenen Zertifikats.

**Konsequenz für die Dimensionierung:** Der Agent meldet laut §7 alle 24 h plus
bis zu 30 min Jitter, dazu Retry-Backoff von bis zu 21 min — im schlechtesten Fall
also gut 24 h 51 min zwischen zwei Meldungen. Mit `full_load_periodicity = 86400`
(24 h) würden gesunde Maschinen regelmäßig in die Obsoleszenz kippen. Der Wert
muss deutlich darüber liegen; hier ist er jetzt auf **259200 s (3 Tage)** gesetzt,
sodass eine Maschine erst nach etwa drei verpassten Meldungen auffällt.

#### Fallstrick 2: die Markierung ist eine Einbahnstraße

Zwei Beobachtungen, die zusammen ein Problem ergeben:

* `delete_policy_update` schreibt `status:obsolete` **unabhängig** vom
  `update`-Flag des Attributs. `status` steht bei uns auf `update=0`, wurde aber
  trotzdem überschrieben — an CI 45 sogar über den von Hand gesetzten Wert `stock`.
* Ein anschließender Import mit allen Maschinen setzt den Status **nicht** zurück.
  Verifiziert: alle vier bleiben `obsolete`. Logisch, denn `update=0` heißt, der
  Sync schreibt das Feld nie.

Es gibt also einen Weg hinein und keinen hinaus. Nach einem Collector-Ausfall
stünde die ganze Flotte auf `obsolete` und müsste von Hand zurückgesetzt werden.

#### Entscheidung: automatische Obsoleszenz abgeschaltet

`delete_policy` steht jetzt auf **`ignore`**, `delete_policy_update` ist leer.
Das ist eine bewusste Abweichung von §9.5 und beruht auf einer Überlegung, die
die beiden Fallstricke oben erst richtig einordnet:

**Der Collector kann „Gerät ist aus" nicht von „Gerät existiert nicht mehr"
unterscheiden.** Beides erzeugt dasselbe Signal — keine Meldung. Ein Notebook im
Urlaub, eine Maschine in Reparatur und ein verschrottetes Gerät sind für die
Datasource identisch. Automatisch zu markieren heißt also, aus einem
mehrdeutigen Signal eine eindeutige Aussage zu machen. Genau davor warnt auch
die Source-of-Truth-Tabelle in §2: `status` ist ein Feld, das das Gerät nicht
kennt.

Die Information geht dabei nicht verloren — sie steht in `agent_last_seen`, das
der Agent bei jedem Sync schreibt. Nur die *Bewertung* bleibt beim Menschen.
Verifizierte Abfrage:

```sql
SELECT PC WHERE agent_last_seen < DATE_SUB(NOW(), INTERVAL 30 DAY)
```

Gegengeprüft mit `INTERVAL -1 DAY` (4 Treffer) und `>` statt `<` (4 Treffer) —
der Ausdruck filtert tatsächlich und liefert nicht nur stillschweigend nichts.
Als gespeicherte Abfrage oder Dashboard-Kachel ist das die Triage-Liste.

**Was das kostet:** Ausgemusterte Geräte werden nicht mehr automatisch auffällig.
§1 nennt veraltete CIs ausdrücklich als eines der Probleme, die das Projekt lösen
soll — dieser Teil wandert damit von „automatisch" zu „regelmäßig anschauen".
Das ist der Preis dafür, keine Fehlmarkierungen zu erzeugen.

**Falls doch automatisch markiert werden soll**, dann nicht auf `status`, sondern
auf ein eigenes Feld `agent_reporting` an `FunctionalCI` (dieselbe Extension):
`delete_policy_update = agent_reporting:verschwunden`, und `agent_reporting` mit
`update=1`/`master_locked`, sodass der Agent bei jedem Sync `aktiv` schreibt.
Damit heilt die Markierung von selbst und `status` bleibt beim Menschen. Fallstrick 1
(flottenweite Markierung bei Collector-Ausfall) bliebe allerdings bestehen.

`full_load_periodicity` (3 Tage) ist mit `delete_policy=ignore` wirkungslos —
verschwundene Replicas werden erkannt, aber es folgt keine Aktion
(„Replica disappeared, no action taken"). Der Wert bleibt gesetzt, damit er beim
etwaigen Wiedereinschalten stimmt.

### Test 5 — VirtualMachine lässt sich vom Agent nicht anlegen

`VirtualMachine.virtualhost_id` ist ein Pflicht-Fremdschlüssel auf `VirtualHost`
(`is_null_allowed=false`, definiert in `itop-virtualization-mgmt`). Eine VM weiß
von innen nicht, auf welchem Hypervisor sie läuft — der Agent kann den Wert also
nicht liefern. Ergebnis des Imports:

```
Objects creation errors: 1
Unable to create destination object:
  Ungültiger Attributwert 'Host' (virtualhost_id) : Null not allowed
```

Zusätzlich stolperte der erste Versuch über etwas anderes: `VirtualMachine` bringt
**virtualhost_id als eigenen Reconciliation-Default** mit (iTop identifiziert VMs
über Host + Name). Das Flag bleibt stehen, wenn man nur die selbst gewählten
Attribute konfiguriert — und wird dann zu einem zweiten, mit AND verknüpften
Schlüssel:

```
Could not reconcile on null value for attribute 'virtualhost_id'
```

`mk_datasource.py` räumt deshalb jetzt **vollständig** auf statt listengetrieben:
alles außer `agent_guid` bekommt `reconcile=0`. Jede neue Zielklasse kann eigene
Defaults mitbringen; ohne diesen Schritt schleichen sie sich still ein.

**Konsequenz:** Der Agent darf VirtualMachine-CIs nicht anlegen. Sinnvoll ist die
Arbeitsteilung, die iTops Datenmodell ohnehin nahelegt — VMs entstehen aus einer
Hypervisor-Quelle (vCenter/Proxmox), die den Host kennt, und der Agent
*ergänzt* sie nur. Technisch: eigene Datasource mit `action_on_zero = error`
statt `create`, damit ein unbekannter Treffer auffällt statt zu scheitern.

Nebenbei zeigen die Klassenunterschiede, dass sich das Report-Schema aus §4 nicht
uniform mappen lässt:

| fehlt an | Attribute |
|---|---|
| `Server` | `macaddress`, `type`, `ipaddress_id` |
| `VirtualMachine` | `serialnumber`, `brand_id`, `model_id`, `macaddress`, `location_id`, `asset_number`, `purchase_date`, `end_of_warranty`, `type`, `ipaddress_id`, `networkdevice_list` |

`macaddress` gibt es nur an `PC`. Die MAC-Adressen aus `interfaces[]` brauchen an
Server und VM also einen anderen Träger (`NetworkInterface`-Objekte) oder müssen
dort entfallen.

### Test 6 — die Klassenentscheidung ist praktisch unumkehrbar

`wks-beta` existierte als `PC` (id 46). Dieselbe GUID über die Server-Datasource
gemeldet:

```
Objects created: 1, creation errors: 0, reconciliation errors: 0

id  name      finalclass  agent_guid
46  wks-beta  PC          2222…
50  wks-beta  Server      2222…
```

Kein Fehler, keine Warnung. Der Grund: die Reconciliation-Abfrage ist auf die
`scope_class` der jeweiligen Datasource beschränkt — `SELECT Server WHERE
agent_guid=…` sieht den `PC` nicht. Es gibt keine klassenübergreifende Prüfung.

**Konsequenz:** Ändert der Collector je seine Meinung über die Klasse einer
Maschine — durch eine verbesserte Heuristik, ein OS-Upgrade, einen Tippfehler in
der Regel —, forkt das CI still. Das Routing muss deshalb *stabil* sein, nicht
nur *richtig*: einmal getroffen, sollte die Klasse an der GUID kleben und nicht
bei jedem Report neu abgeleitet werden. Praktisch heißt das, die Zuordnung
GUID → Zielklasse gehört in die Device-Registry des Collectors, neben das Token.

---

## Was das für die Roadmap heißt

**Neu, gehört vor M3:** Reimaging-Auflösung im Collector. Vor jedem Push mit
unbekannter GUID per REST nach `serialnumber` suchen; bei genau einem Treffer
dessen `agent_guid` übernehmen, bei mehreren einen Konflikt loggen und nichts
tun. Ohne diesen Schritt wächst die CMDB bei jedem Reimaging um eine Dublette.

**Offene Entscheidung aus §11 beantwortet:** *„Custom-Feld `agent_guid` am CI —
wer pflegt die Anpassung?"* → Erledigt als Extension `custom-agent-inventory`
(`agent_guid`, `agent_last_seen` an `FunctionalCI`). Liegt als Bind-Mount unter
`/opt/itop-test/extensions/`, iterierbar ohne Image-Rebuild. Bewusst an
`FunctionalCI`, weil `PhysicalDevice` zwar `PC` und `Server` abdeckt, aber nicht
`VirtualMachine` — die hängt unter `VirtualDevice`.

**Offene Entscheidung aus §11 beantwortet:** *„nur PC, oder PC/Server/
VirtualMachine getrennt?"* → Getrennt, es geht nicht anders (`scope_class` ist ein
Einzelwert). Alle drei Datasources sind angelegt und identisch konfiguriert:

| id | Zielklasse | Tabelle | konfigurierte Attribute |
|---|---|---|---|
| 1 | `PC` | `synchro_data_agent_pc` | 29 |
| 2 | `Server` | `synchro_data_agent_server` | 26 |
| 3 | `VirtualMachine` | `synchro_data_agent_vm` | 18 |

Daran hängen aber zwei Auflagen, die Test 5 und 6 ergeben haben:

* **VirtualMachine nicht vom Agent anlegen lassen** — `virtualhost_id` ist Pflicht
  und dem Gerät unbekannt. Quelle für VMs ist der Hypervisor, der Agent ergänzt.
* **Das Klassen-Routing muss stabil sein.** Ein Klassenwechsel forkt das CI ohne
  Fehlermeldung. Die Zuordnung GUID → Zielklasse gehört in die Device-Registry
  des Collectors und wird einmal entschieden, nicht bei jedem Report neu.

**§9.5 ist überholt.** Die automatische Obsoleszenz ist abgeschaltet: der Collector
kann Abwesenheit nicht von Ausserbetriebnahme unterscheiden. Ersatz ist eine
Triage-Abfrage auf `agent_last_seen`, die ein Mensch bewertet. Der Absatz in
PROJECT.md gehört entsprechend umgeschrieben. Details in Test 4.

**Falls die Obsoleszenz je wieder aktiviert wird:** `full_load_periodicity` muss
deutlich über dem maximalen Meldeabstand liegen (24 h + Jitter + Backoff
≈ 24 h 51 min) — sonst kippen gesunde Maschinen. Aktuell auf 3 Tage gesetzt und
an §7 gekoppelt: ändert sich das Report-Intervall, muss der Wert mitwandern.

**Bewusst offen gelassen:** keine `unique_rule` auf `agent_guid`. Für M0 war das
richtig, um das Konfliktverhalten überhaupt beobachten zu können. Nach M0 ist
eine Eindeutigkeitsregel sinnvoll — dann als bewusste Entscheidung mit bekanntem
Verhalten.

**Zustand der Testinstanz:** Die vier Test-CIs sind wieder auf ihren Ausgangsstatus
gesetzt (`wks-alpha-umbenannt` id 45 auf `stock`, der Rest auf `production`). Die
Dublette aus Test 3c (id 49) steht bewusst noch da — sie ist der Beleg für den
Reimaging-Befund.

---

## Reproduzieren

Alle Testdaten und Skripte liegen auf dem Host:

```
/opt/itop-test/m0/01-anlage.csv      Test 1
/opt/itop-test/m0/02-update.csv      Test 2
/opt/itop-test/m0/03-konflikt.csv    Test 3 / 3b
/opt/itop-test/m0/04-reimaging.csv   Test 3c
/opt/itop-test/m0/05-obsolet.csv     Test 4
/opt/itop-test/m0/m0.py              Zustandsanzeige
/opt/itop-test/itop_rest.py          REST-Client
```

Import:

```bash
docker exec -u www-data itop-test-itop-1 sh -c \
  'cd /var/www/html/synchro && php synchro_import.php \
     --auth_user=syncagent --auth_pwd=... --data_source_id=1 \
     --csvfile=/tmp/01.csv --output=summary'
```

> Die Spalte `primary_key` ist die quellseitige Kennung des Replicas und muss
> gefüllt sein. Bleibt sie leer, landen **alle** Zeilen auf demselben Replica und
> überschreiben sich gegenseitig — ohne Fehlermeldung. Beim ersten Versuch wurden
> so aus drei Maschinen eine. Für den Agent ist `primary_key = agent_guid` richtig.
