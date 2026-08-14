// Package report definiert das Meldeformat zwischen Agent und Collector.
//
// Das Schema ist fuer Windows und Linux identisch. Aenderungen sind
// ausschliesslich additiv: der Collector muss aeltere Agent-Versionen
// weiterverarbeiten koennen, deshalb wird AgentVersion mitgeschickt und
// nie ein Feld entfernt oder umbenannt.
package report

import (
	"net"
	"time"
)

// SchemaVersion beschreibt die Ausbaustufe des Meldeformats. Wird bei additiven
// Erweiterungen hochgezaehlt, damit der Collector im Zweifel entscheiden kann,
// welche Felder er ueberhaupt erwarten darf.
const SchemaVersion = 3

// OSFamily unterscheidet die beiden Plattformen. Der Collector nutzt den Wert
// als EINEN von mehreren Hinweisen fuer die Zielklasse - nicht als alleinige
// Grundlage, siehe Routing-Hinweise in collectorsrv.
type OSFamily string

const (
	OSWindows OSFamily = "windows"
	OSLinux   OSFamily = "linux"
)

// Interface ist eine Netzwerkschnittstelle mit ihren Adressen.
type Interface struct {
	Description string   `json:"description"`
	MAC         string   `json:"mac"`
	IPs         []string `json:"ips"`

	// DHCP sagt, ob die Adressen dieser Schnittstelle per DHCP zugeteilt wurden.
	//
	// Additiv ergaenzt (SchemaVersion 3). Grund: eine DHCP-Adresse gehoert nicht
	// in die CMDB - sie wechselt, und bei jedem Wechsel entstuende im IPAM ein
	// weiterer Eintrag, den niemand aufraeumt. Nur statisch vergebene Adressen
	// sind eine Aussage ueber das Geraet.
	//
	// Aeltere Agenten senden das Feld nicht; dann ist es false, und der Collector
	// wuerde eine DHCP-Adresse faelschlich fuer statisch halten. Deshalb liefert
	// PrimaryIP nur dann etwas, wenn der Agent das Feld ueberhaupt kennt.
	DHCP bool `json:"dhcp,omitempty"`
}

// Disk ist ein lokaler Datentraeger bzw. eine Partition.
type Disk struct {
	DeviceID  string `json:"device_id"`
	SizeBytes int64  `json:"size_bytes"`
	FreeBytes int64  `json:"free_bytes"`
}

// Software ist ein installiertes Programm.
type Software struct {
	Name      string `json:"name"`
	Version   string `json:"version"`
	Publisher string `json:"publisher"`
}

// Report ist die vollstaendige Meldung eines Agents.
//
// Grundregel des Agents: eine fehlende Datenquelle fuehrt zu einem leeren Feld,
// nie zum Abbruch. Der Collector muss also mit Leerwerten in jedem Feld ausser
// AgentGUID rechnen.
type Report struct {
	// AgentGUID ist der primaere Abgleichschluessel. Vom Agent erzeugt,
	// ueberlebt Umbenennung, nicht Reimaging (so gewollt).
	//
	// In iTop ist das der EINZIGE Reconciliation-Key. Belegt in M0: iTop
	// verknuepft mehrere Reconciliation-Attribute mit AND und kennt keinen
	// Fallback auf einen zweiten Schluessel.
	AgentGUID string `json:"agent_guid"`

	AgentVersion string    `json:"agent_version"`
	CollectedAt  time.Time `json:"collected_at"`

	OSFamily OSFamily `json:"os_family"`
	Hostname string   `json:"hostname"`
	Domain   string   `json:"domain"`

	Manufacturer string `json:"manufacturer"`
	Model        string `json:"model"`

	// SerialNumber ist leer, wenn der Agent einen OEM-Platzhalter erkannt hat.
	// Ein leerer Wert darf NIE zum Abgleich benutzt werden - siehe
	// SanitizeSerial.
	SerialNumber string `json:"serial_number"`

	OSName    string `json:"os_name"`
	OSVersion string `json:"os_version"`

	CPU      string `json:"cpu"`
	RAMBytes int64  `json:"ram_bytes"`

	// Virtualization benennt die erkannte Virtualisierung ("kvm", "vmware",
	// "hyperv", "xen", ...) oder ist leer bei physischer Hardware.
	//
	// Additiv ergaenzt (SchemaVersion 2). Der Collector braucht das Feld, um die
	// iTop-Zielklasse zu bestimmen: PC/Server sind physisch, VirtualMachine
	// nicht. Ohne dieses Merkmal kann er VMs nicht von Blech unterscheiden.
	//
	// Aeltere Agenten senden das Feld nicht - dann ist es leer, und der Collector
	// behandelt die Maschine wie bisher als physisch.
	Virtualization string `json:"virtualization,omitempty"`

	Interfaces []Interface `json:"interfaces"`
	Disks      []Disk      `json:"disks"`
	Software   []Software  `json:"software"`
}

// PrimaryMAC liefert die MAC der ersten Schnittstelle, die eine hat.
//
// iTop haelt an der Klasse PC genau EIN Feld macaddress - eine Liste passt da
// nicht hinein, und an Server und VirtualMachine gibt es das Feld ueberhaupt
// nicht (in M0 nachgewiesen). Fuer die vollstaendige Abbildung braucht es
// spaeter NetworkInterface-Objekte; bis dahin ist dies der pragmatische Wert.
func (r *Report) PrimaryMAC() string {
	for _, iface := range r.Interfaces {
		if iface.MAC != "" {
			return iface.MAC
		}
	}
	return ""
}

// PrimaryIP liefert die IPv4-Adresse, die das Geraet dauerhaft kennzeichnet.
//
// Ausgeschlossen sind:
//   - DHCP-Adressen: sie wechseln und wuerden das IPAM zumuellen
//   - Loopback und Link-Local (169.254/16): sagen nichts ueber das Netz aus
//   - IPv6: iTops ipaddress_id an PC/Server zeigt auf IPv4Address
//
// Leerer Rueckgabewert heisst "keine dauerhafte Adresse bekannt" - NICHT "das
// Geraet hat keine IP". Der Collector darf daraufhin nichts loeschen.
func (r *Report) PrimaryIP() string {
	for _, iface := range r.Interfaces {
		if iface.DHCP {
			continue
		}
		for _, raw := range iface.IPs {
			ip := net.ParseIP(raw)
			if ip == nil || ip.To4() == nil {
				continue // kein IPv4
			}
			if ip.IsLoopback() || ip.IsLinkLocalUnicast() || ip.IsUnspecified() {
				continue
			}
			return ip.String()
		}
	}
	return ""
}

// RAMMebibytes rechnet Bytes in MiB um, weil iTop RAM als Zahl in MiB fuehrt.
// Rundet ab; 0 bleibt 0.
func (r *Report) RAMMebibytes() int64 {
	if r.RAMBytes <= 0 {
		return 0
	}
	return r.RAMBytes / (1024 * 1024)
}
