//go:build windows

package collect

import "strings"

// win32NetworkAdapterConfiguration liefert je Adapter, ob DHCP aktiv ist.
//
// Diese WMI-Klasse steht so schon in Abschnitt 5 des PROJECT.md; DHCPEnabled
// ist genau die Auskunft, die unter Linux erst aus dem Netlink-Flag
// hergeleitet werden muss.
type win32NetworkAdapterConfiguration struct {
	MACAddress  *string
	DHCPEnabled *bool
}

// dhcpInterfaces liefert die Schnittstellen, deren Adressen per DHCP kommen.
//
// Zuordnung ueber die MAC-Adresse, nicht ueber den Namen: WMI kennt den Adapter
// unter seiner Beschreibung ("Intel(R) PRO/1000 MT Network Connection"),
// net.Interfaces() unter dem Verbindungsnamen ("Ethernet"). Die MAC ist der
// einzige Wert, den beide Seiten gleich sehen.
//
// Der Aufrufer schlaegt spaeter mit dem Schnittstellennamen nach - deshalb wird
// hier zusaetzlich ueber net.Interfaces() auf den Namen umgeschluesselt.
func dhcpInterfaces() map[string]bool {
	out := map[string]bool{}

	rows := queryWMI[win32NetworkAdapterConfiguration](
		"SELECT MACAddress, DHCPEnabled FROM Win32_NetworkAdapterConfiguration WHERE IPEnabled=TRUE")
	if len(rows) == 0 {
		// Konservativ: ohne Auskunft gilt alles als statisch. Der Collector legt
		// dann hoechstens eine IP an, die er nicht haette anlegen sollen - das
		// ist besser, als eine vorhandene stillschweigend zu uebersehen.
		return out
	}

	byMAC := map[string]bool{}
	for _, r := range rows {
		if r.MACAddress == nil || r.DHCPEnabled == nil {
			continue
		}
		byMAC[strings.ToUpper(strings.TrimSpace(*r.MACAddress))] = *r.DHCPEnabled
	}

	for name, mac := range interfaceMACs() {
		if isDHCP, ok := byMAC[mac]; ok && isDHCP {
			out[name] = true
		}
	}
	return out
}
