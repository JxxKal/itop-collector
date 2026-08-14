#!/usr/bin/env python3
"""
Legt eine Synchro Data Source fuer eine Zielklasse an und konfiguriert die
Attribut-Policies nach der Source-of-Truth-Tabelle aus PROJECT.md Abschnitt 2.

Hintergrund: scope_class ist ein EINZELWERT. Der Agent zielt aber auf PC, Server
und VirtualMachine - also braucht jede Klasse ihre eigene Datasource. Dieses
Skript haelt die drei identisch konfiguriert, damit sie nicht auseinanderlaufen.

Aufruf:  python3 mk_datasource.py <Klasse> <tabellenname>
"""
import sys

sys.path.insert(0, "/opt/itop")
from itop_rest import call  # noqa: E402

# Reconciliation ausschliesslich ueber agent_guid.
# Begruendung in der M0-Auswertung: iTop verknuepft mehrere reconcile-Attribute
# mit AND und kennt keinen Fallback (synchrodatasource.class.inc.php:2203).
RECONCILE = "agent_guid"

MASTER = ["name", "serialnumber", "cpu", "ram", "brand_id", "model_id",
          "osfamily_id", "osversion_id", "macaddress", "agent_last_seen"]

# Pflichtfelder der Zielklasse: muessen bei der ANLAGE gesetzt werden, danach nie
# wieder. update=0 ginge nicht - dann fehlte die Spalte in der Synchro-Tabelle.
CREATE_ONLY = ["org_id"]

NEVER = ["location_id", "business_criticity", "asset_number", "purchase_date",
         "end_of_warranty", "description", "move2production", "status", "type",
         "ipaddress_id", "applicationsolution_list", "contacts_list",
         "documents_list", "networkdevice_list", "providercontracts_list",
         "services_list", "tickets_list"]


def create_source(target_class, table):
    res = call({
        "operation": "core/create",
        "class": "SynchroDataSource",
        "comment": f"M0: Datasource fuer {target_class}",
        "output_fields": "id,name,scope_class",
        "fields": {
            "name": f"itop-agent {target_class}",
            "description": f"Inventar-Sync vom itop-agent, Zielklasse {target_class}.",
            "status": "production",
            "user_id": 2,
            "scope_class": target_class,
            "database_table_name": table,
            "reconciliation_policy": "use_attributes",
            "action_on_zero": "create",
            "action_on_one": "update",
            "action_on_multiple": "error",
            # Keine automatische Obsoleszenz: Abwesenheit ist kein Beleg fuer
            # Ausserbetriebnahme. Triage laeuft ueber agent_last_seen.
            "delete_policy": "ignore",
            "full_load_periodicity": 259200,
        },
    })
    if res.get("code") != 0:
        print(f"FEHLER beim Anlegen: {res.get('message')}")
        return None
    obj = list(res["objects"].values())[0]
    if obj.get("code") != 0:
        print(f"FEHLER beim Anlegen: {obj.get('message')}")
        return None
    sid = obj["fields"]["id"]
    print(f"  Datasource {sid} fuer {target_class} angelegt")
    return int(sid)


def configure(sid):
    def setatt(attcode, update, reconcile, policy):
        r = call({
            "operation": "core/update",
            "class": "SynchroAttribute",
            "key": f"SELECT SynchroAttribute WHERE sync_source_id={sid} AND attcode='{attcode}'",
            "fields": {"update": update, "reconcile": reconcile, "update_policy": policy},
            "output_fields": "attcode",
            # Pflichtparameter bei core/update. Fehlt er, antwortet iTop mit
            # code 100 "Missing parameter 'comment'" - und das sieht von aussen
            # genauso aus wie "Attribut gibt es an dieser Klasse nicht".
            "comment": "M0: Source-of-Truth-Konfiguration",
        })
        # Nicht jede Klasse hat jedes Attribut - fehlende sind kein Fehler.
        # Andere Fehlercodes schon, die sollen auffallen.
        # "No item found for query" = das Attribut gibt es an dieser Klasse
        # nicht. Jeder andere Fehler soll auffallen statt still zu verschwinden.
        msg = str(r.get("message", "")).lower()
        if r.get("code") != 0 and "no item found" not in msg:
            raise RuntimeError(f"{attcode}: {r.get('message')}")
        if r.get("code") != 0:
            return None
        return len(r.get("objects") or {})

    applied, missing = [], []
    for att, upd, rec, pol in ([(RECONCILE, 1, 1, "write_if_empty")]
                               + [(a, 1, 0, "master_locked") for a in MASTER]
                               + [(a, 1, 0, "write_if_empty") for a in CREATE_ONLY]
                               + [(a, 0, 0, "master_locked") for a in NEVER]):
        n = setatt(att, upd, rec, pol)
        (applied if n else missing).append(att)
    print(f"  konfiguriert: {len(applied)} Attribute")
    if missing:
        print(f"  an dieser Klasse nicht vorhanden: {', '.join(missing)}")

    # Jede Zielklasse bringt ihre EIGENEN Reconciliation-Defaults mit, die nicht
    # in den Listen oben stehen muessen. VirtualMachine zum Beispiel setzt
    # virtualhost_id - iTop identifiziert VMs ueber Host + Name. Bleibt so ein
    # Flag stehen, ist es ein zweiter, mit AND verknuepfter Schluessel, und der
    # Import scheitert an:
    #   "Could not reconcile on null value for attribute 'virtualhost_id'"
    #
    # Deshalb hier nicht listengetrieben aufraeumen, sondern vollstaendig: alles
    # ausser dem gewollten Schluessel bekommt reconcile=0.
    leftovers = call({
        "operation": "core/get",
        "class": "SynchroAttribute",
        "key": f"SELECT SynchroAttribute WHERE sync_source_id={sid} AND reconcile=1",
        "output_fields": "attcode",
    })
    for obj in (leftovers.get("objects") or {}).values():
        att = obj["fields"]["attcode"]
        if att == RECONCILE:
            continue
        call({
            "operation": "core/update",
            "class": "SynchroAttribute",
            "key": f"SELECT SynchroAttribute WHERE sync_source_id={sid} AND attcode='{att}'",
            "fields": {"reconcile": 0},
            "output_fields": "attcode",
            "comment": "M0: klassenspezifischen Reconciliation-Default entfernt",
        })
        print(f"  Reconciliation-Default entfernt: {att}")


if __name__ == "__main__":
    # "configure <id>" konfiguriert eine bereits angelegte Quelle nach.
    if sys.argv[1] == "configure":
        configure(int(sys.argv[2]))
    else:
        sid = create_source(sys.argv[1], sys.argv[2])
        if sid:
            configure(sid)
