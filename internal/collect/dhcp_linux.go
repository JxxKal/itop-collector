//go:build linux

package collect

import (
	"net"
	"syscall"
)

// Der Kernel merkt sich zu jeder Adresse, ob sie dauerhaft gesetzt wurde
// (IFA_F_PERMANENT) oder eine Laufzeit hat. Genau daraus leitet auch "ip addr"
// sein "dynamic" ab.
//
// Bewusst ueber Netlink und nicht ueber einen Aufruf von "ip":
//   - iproute2 ist nicht auf jedem System installiert (Minimal-Images, Container)
//   - "ip -j" gibt es erst ab iproute2 4.15
//   - syscall.NetlinkRIB steckt in der Standardbibliothek, es kommt nichts dazu
//
// Lease-Dateien waeren die dritte Moeglichkeit, sind aber je nach
// Netzwerkverwaltung (dhclient, dhcpcd, NetworkManager, systemd-networkd) an
// anderer Stelle und teils gar nicht vorhanden. Auf der Testmaschine sind die
// Verzeichnisse leer, obwohl die Adresse per DHCP kam - der Kernel weiss es
// trotzdem.

// IFA_F_PERMANENT ist in syscall auf manchen Plattformen nicht exportiert.
// Der Wert ist Teil der stabilen Kernel-ABI (include/uapi/linux/if_addr.h).
const ifaFPermanent = 0x80

// dhcpInterfaces liefert die Namen der Schnittstellen, deren IPv4-Adressen
// NICHT dauerhaft gesetzt sind.
//
// Bei einem Fehler wird eine leere Menge zurueckgegeben - dann gelten alle
// Adressen als statisch. Das ist bewusst die konservative Richtung: der
// Collector legt hoechstens eine IP an, die er nicht haette anlegen sollen,
// statt eine vorhandene stillschweigend zu uebersehen.
func dhcpInterfaces() map[string]bool {
	out := map[string]bool{}

	msgs, err := syscall.NetlinkRIB(syscall.RTM_GETADDR, syscall.AF_INET)
	if err != nil {
		return out
	}
	parsed, err := syscall.ParseNetlinkMessage(msgs)
	if err != nil {
		return out
	}

	// Index -> Name, um die Netlink-Antwort den Schnittstellen zuzuordnen.
	names := map[int]string{}
	if ifaces, err := net.Interfaces(); err == nil {
		for _, i := range ifaces {
			names[i.Index] = i.Name
		}
	}

	for _, m := range parsed {
		if m.Header.Type != syscall.RTM_NEWADDR {
			continue
		}
		if len(m.Data) < syscall.SizeofIfAddrmsg {
			continue
		}
		// Feste Struktur am Anfang der Nutzdaten: Family, Prefixlen, Flags,
		// Scope, Index (uint32, wirtsseitige Bytereihenfolge).
		flags := m.Data[2]
		index := int(uint32(m.Data[4]) | uint32(m.Data[5])<<8 |
			uint32(m.Data[6])<<16 | uint32(m.Data[7])<<24)

		name, ok := names[index]
		if !ok {
			continue
		}
		// Fehlt IFA_F_PERMANENT, hat die Adresse eine Laufzeit - sie stammt also
		// von DHCP oder einer anderen dynamischen Zuteilung.
		if flags&ifaFPermanent == 0 {
			out[name] = true
		}
	}
	return out
}
