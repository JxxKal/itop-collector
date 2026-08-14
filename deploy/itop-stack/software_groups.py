#!/usr/bin/env python3
"""
Hinterlegt die Zuordnungsmuster an den Software-Gruppen im iTop-Katalog.

AUSGANGSLAGE: In der Produktion existieren die Gruppen bereits. Dieses Skript
ergaenzt sie nur um das Feld agent_match_patterns - es legt standardmaessig
NICHTS an. Ein zweiter Katalogeintrag, nur weil ein Name anders geschrieben ist,
waere in einer gepflegten CMDB schlimmer als eine fehlende Zuordnung.

    python3 software_groups.py --dry-run       zeigen, was passieren wuerde
    python3 software_groups.py                 Muster an vorhandene Gruppen schreiben
    python3 software_groups.py --create-missing  fehlende Gruppen zusaetzlich anlegen
    python3 software_groups.py --list            Katalog zeigen (Ist-Zustand)

Die Zuordnung laeuft in drei Stufen, damit Schreibweisen nicht zu Dubletten
fuehren:

    1. exakter Name
    2. Name ohne Beachtung von Gross-/Kleinschreibung und Mehrfach-Leerzeichen
    3. kein Treffer -> wird GEMELDET, nicht angelegt

Stufe 2 meldet den Unterschied mit, damit sichtbar ist, dass hier ein anderer
Eintrag getroffen wurde als erwartet.

MUSTERSYNTAX (ein Muster je Zeile):
    text            trifft, wenn text im gemeldeten Namen vorkommt (Gross-/Klein egal)
    !text           schliesst aus - schlaegt jeden Einschluss
    /regex/         regulaerer Ausdruck, gegen den klein geschriebenen Namen
    # Kommentar     wird uebersprungen
"""
import re
import sys

sys.path.insert(0, "/opt/itop")
from itop_rest import call

# Bewusst breit gefasste Einschluesse plus gezielte Ausschluesse. Ein zu enges
# Muster uebersieht Installationen still; ein zu weites faellt in /unmatched
# bzw. an einer falschen Zuordnung auf und laesst sich nachschaerfen.
#
# ARBEITSSTAND: welche Programmnamen tatsaechlich auftauchen, weiss man erst,
# wenn die ersten Maschinen aus der Flaeche gemeldet haben. Der Endpunkt
# /unmatched am Collector zeigt, was noch fehlt.
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

# Nur fuer --create-missing. vendor und version sind an Software Pflichtfelder;
# fuer eine Gruppe sind beide fachlich sinnlos, deshalb Werte, an denen in der
# iTop-Liste erkennbar ist, dass hier kein Produkt steht.
VENDOR = "Sammelgruppe"
VERSION = "alle"
TYPE = "PCSoftware"


def normalise(name):
    """Vergleichsform: klein, Mehrfach-Leerzeichen zusammengezogen."""
    return re.sub(r"\s+", " ", name.strip()).lower()


def load_catalog():
    """Liest den gesamten Software-Katalog einmal."""
    r = call({
        "operation": "core/get",
        "class": "Software",
        "key": "SELECT Software",
        "output_fields": "id,name,agent_match_patterns",
    })
    if r.get("code") != 0:
        raise RuntimeError(r.get("message"))
    return [v["fields"] for v in (r.get("objects") or {}).values()]


def find(catalog, name):
    """(Eintrag, Trefferart) oder (None, None)."""
    for e in catalog:
        if e["name"] == name:
            return e, "exakt"
    target = normalise(name)
    for e in catalog:
        if normalise(e["name"]) == target:
            return e, "abweichende Schreibweise"
    return None, None


def set_patterns(entry, patterns):
    r = call({
        "operation": "core/update",
        "class": "Software",
        "key": int(entry["id"]),
        "fields": {"agent_match_patterns": patterns},
        "output_fields": "id,name",
        "comment": "Zuordnungsmuster fuer den itop-agent",
    })
    if r.get("code") != 0:
        return r.get("message")
    for v in (r.get("objects") or {}).values():
        if v.get("code") != 0:
            return v.get("message")
    return None


def create(name, patterns):
    r = call({
        "operation": "core/create",
        "class": "Software",
        "fields": {"name": name, "vendor": VENDOR, "version": VERSION,
                   "type": TYPE, "agent_match_patterns": patterns},
        "output_fields": "id,name",
        "comment": "Softwaregruppe fuer den itop-agent",
    })
    if r.get("code") != 0:
        return r.get("message")
    for v in (r.get("objects") or {}).values():
        if v.get("code") != 0:
            return v.get("message")
    return None


def main():
    dry = "--dry-run" in sys.argv
    create_missing = "--create-missing" in sys.argv

    catalog = load_catalog()

    if "--list" in sys.argv:
        print("Katalog (%d Eintraege):\n" % len(catalog))
        for e in sorted(catalog, key=lambda x: x["name"].lower()):
            marker = "M" if e.get("agent_match_patterns", "").strip() else " "
            print("  [%s] %-45s id %s" % (marker, e["name"], e["id"]))
        print("\n  [M] = hat bereits Zuordnungsmuster")
        return 0

    if dry:
        print("Probelauf - es wird nichts geaendert.\n")

    fehlend, geaendert, unveraendert, fehler = [], 0, 0, 0

    for name, patterns in GROUPS.items():
        patterns = patterns.strip() + "\n"
        entry, how = find(catalog, name)

        if entry is None:
            fehlend.append(name)
            continue

        hinweis = "" if how == "exakt" else "  <- %s: '%s'" % (how, entry["name"])

        if entry.get("agent_match_patterns", "").strip() == patterns.strip():
            print("  unveraendert  %-45s (id %s)%s" % (name, entry["id"], hinweis))
            unveraendert += 1
            continue

        if dry:
            vorher = "leer" if not entry.get("agent_match_patterns", "").strip() else "vorhanden"
            print("  WUERDE SETZEN %-45s (id %s, bisher %s)%s"
                  % (name, entry["id"], vorher, hinweis))
            geaendert += 1
            continue

        err = set_patterns(entry, patterns)
        if err:
            print("  FEHLER        %-45s %s" % (name, err[:80]))
            fehler += 1
        else:
            print("  gesetzt       %-45s (id %s)%s" % (name, entry["id"], hinweis))
            geaendert += 1

    if fehlend:
        print("\n  Nicht im Katalog gefunden (%d):" % len(fehlend))
        for n in fehlend:
            print("    - %s" % n)
        if create_missing:
            print()
            for n in fehlend:
                if dry:
                    print("  WUERDE ANLEGEN %s" % n)
                    continue
                err = create(n, GROUPS[n].strip() + "\n")
                print("  %-13s %s%s" % ("angelegt" if not err else "FEHLER", n,
                                        "" if not err else "  " + err[:80]))
        else:
            print("\n  Diese Eintraege wurden NICHT angelegt. Entweder heissen sie in")
            print("  iTop anders - dann die Namen oben in GROUPS anpassen - oder mit")
            print("  --create-missing bewusst anlegen lassen.")

    print("\n  %d gesetzt, %d unveraendert, %d nicht gefunden, %d Fehler"
          % (geaendert, unveraendert, len(fehlend), fehler))
    return 1 if fehler else 0


if __name__ == "__main__":
    sys.exit(main())
