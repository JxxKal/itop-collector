#!/usr/bin/env python3
"""
Legt die Basis-Softwaregruppen im iTop-Katalog an.

Jede Gruppe ist ein Software-Eintrag mit Zuordnungsmustern im Feld
agent_match_patterns. Der Collector liest diese Liste aus iTop - Gruppen
ergaenzen oder Muster nachschaerfen geht damit dort, ohne dass am Collector
etwas neu ausgerollt werden muss.

Aufruf:
    python3 software_groups.py            anlegen bzw. Muster aktualisieren
    python3 software_groups.py --dry-run  nur zeigen, was passieren wuerde

MUSTERSYNTAX (ein Muster je Zeile):
    text            trifft, wenn text im gemeldeten Namen vorkommt (Gross-/Klein egal)
    !text           schliesst aus - schlaegt jeden Einschluss
    /regex/         regulaerer Ausdruck, gegen den klein geschriebenen Namen
    # Kommentar     wird uebersprungen

Die Muster unten sind ein ARBEITSSTAND, kein Endergebnis. Welche Programmnamen
tatsaechlich auftauchen, weiss man erst, wenn die ersten Maschinen gemeldet
haben - dafuer gibt es den Endpunkt /unmatched am Collector.
"""
import sys

sys.path.insert(0, "/opt/itop")
from itop_rest import call

# Bewusst breit gefasste Einschluesse plus gezielte Ausschluesse. Ein zu enges
# Muster uebersieht Installationen still; ein zu weites faellt in /unmatched
# bzw. an einer falschen Zuordnung auf und laesst sich nachschaerfen.
GROUPS = {
    ".Net Framework": """
.net
# Deckt .NET Framework, .NET Runtime/Host/SDK und ASP.NET Core gleichermassen ab.
# Der Punkt ist wesentlich - ohne ihn traefe das Muster auch "Telnet".
!.NET Reflector
""",

    "Baramundi Management": """
baramundi
""",

    "BeyondTrust Privileged Remote Access": """
beyondtrust
bomgar
# Bomgar ist der fruehere Produktname; auf aelteren Installationen steht er noch.
""",

    "Browser": """
google chrome
mozilla firefox
/^microsoft edge( |$)/
# "Edge" allein waere zu weit gefasst - es steckt auch in Produktnamen wie
# "Edge Diagnostics Adapter". Der regulaere Ausdruck bindet es an den Anfang.
opera
brave
vivaldi
chromium
!chrome remote desktop
!firefox esr policy
""",

    "IFIX": """
ifix
proficy
""",

    "Java JRE": """
java
jre
openjdk
adoptium
temurin
!javascript
!java auto updater
""",

    "LibreOffice": """
libreoffice
openoffice
""",

    "OPC": """
opc
# Absichtlich breit: OPC-Komponenten treten unter vielen Herstellernamen auf.
# Faellt hier etwas falsch hinein, gezielt ausschliessen.
""",

    "PCS 7": """
pcs 7
simatic pcs
""",

    "Splunk Universal Forwarder": """
splunk
""",

    "SQL Server Management Studio": """
sql server management studio
ssms
""",

    "TeBIS3 Windows Client (64 bit)": """
tebis
""",

    "Visual C++ Redistributable": """
visual c++
vc_redist
!visual studio
# Visual Studio selbst ist keine Redistributable-Komponente.
""",

    "WebnavigatorClient": """
webnavigator
""",

    "WinCC": """
wincc
""",
}

# vendor und version sind an Software Pflichtfelder. Fuer eine Gruppe sind beide
# fachlich sinnlos - deshalb Werte, an denen in der iTop-Liste sofort erkennbar
# ist, dass hier kein Produkt steht, sondern eine Sammelkategorie.
VENDOR = "Sammelgruppe"
VERSION = "alle"
TYPE = "PCSoftware"


def find(name):
    r = call({
        "operation": "core/get",
        "class": "Software",
        "key": "SELECT Software WHERE name = '%s'" % name.replace("'", "\\'"),
        "output_fields": "id,name,agent_match_patterns",
    })
    if r.get("code") != 0:
        raise RuntimeError(r.get("message"))
    for v in (r.get("objects") or {}).values():
        return v["fields"]
    return None


def upsert(name, patterns, dry_run=False):
    patterns = patterns.strip() + "\n"
    existing = find(name)

    if existing is None:
        if dry_run:
            print("  ANLEGEN     %s" % name)
            return
        r = call({
            "operation": "core/create",
            "class": "Software",
            "fields": {"name": name, "vendor": VENDOR, "version": VERSION,
                       "type": TYPE, "agent_match_patterns": patterns},
            "output_fields": "id,name",
            "comment": "Basis-Softwaregruppe fuer den itop-agent",
        })
        ok = r.get("code") == 0
        print("  %-11s %-40s %s" % ("angelegt" if ok else "FEHLER",
                                    name, "" if ok else r.get("message")[:90]))
        return

    if existing.get("agent_match_patterns", "").strip() == patterns.strip():
        print("  unveraendert %-39s (id %s)" % (name, existing["id"]))
        return

    if dry_run:
        print("  MUSTER NEU  %s (id %s)" % (name, existing["id"]))
        return

    r = call({
        "operation": "core/update",
        "class": "Software",
        "key": int(existing["id"]),
        "fields": {"agent_match_patterns": patterns},
        "output_fields": "id,name",
        "comment": "Zuordnungsmuster aktualisiert",
    })
    ok = r.get("code") == 0
    print("  %-11s %-40s %s" % ("Muster neu" if ok else "FEHLER",
                                name, "" if ok else r.get("message")[:90]))


if __name__ == "__main__":
    dry = "--dry-run" in sys.argv
    if dry:
        print("Probelauf - es wird nichts geaendert.\n")
    for name, patterns in GROUPS.items():
        upsert(name, patterns, dry_run=dry)
    print("\n%d Gruppen verarbeitet." % len(GROUPS))
