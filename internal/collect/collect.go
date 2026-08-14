// Package collect sammelt die Inventardaten der lokalen Maschine.
//
// Grundregel fuer alle Sammler: eine fehlende oder unlesbare Datenquelle fuehrt
// zu einem leeren Feld, NIE zum Abbruch. Ein Container ohne /sys/class/dmi, eine
// ARM-Maschine ohne DMI oder ein nicht als root laufender Agent sollen eine
// unvollstaendige, aber brauchbare Meldung liefern.
package collect

import (
	"net"
	"strings"

	"github.com/JxxKal/itop-collector/internal/report"
)

// Collect sammelt alles, was auf der aktuellen Plattform verfuegbar ist.
// Implementiert je Plattform in collect_linux.go / collect_windows.go.
func Collect(agentGUID, agentVersion string) *report.Report {
	return collectPlatform(agentGUID, agentVersion)
}

// virtualNamePrefixes sind Schnittstellen, die nicht zur Maschine gehoeren,
// sondern von Docker, VPNs oder Bridges stammen. Sie in die CMDB zu schreiben
// erzeugt Rauschen: sie wechseln bei jedem Containerstart.
var virtualNamePrefixes = []string{
	"lo", "docker", "br-", "veth", "virbr", "tun", "tap", "kube", "cni", "flannel", "wg",
}

func isVirtualInterface(name string) bool {
	for _, p := range virtualNamePrefixes {
		if name == p || strings.HasPrefix(name, p) {
			return true
		}
	}
	return false
}

// interfaceMACs bildet Schnittstellenname -> MAC ab (Grossschreibung).
//
// Wird unter Windows gebraucht, um die WMI-Auskunft ueber DHCP der jeweiligen
// Schnittstelle zuzuordnen: WMI kennt den Adapter unter seiner Beschreibung,
// net.Interfaces() unter dem Verbindungsnamen.
func interfaceMACs() map[string]string {
	out := map[string]string{}
	ifaces, err := net.Interfaces()
	if err != nil {
		return out
	}
	for _, i := range ifaces {
		if mac := strings.ToUpper(i.HardwareAddr.String()); mac != "" {
			out[i.Name] = mac
		}
	}
	return out
}

// interfaces liest die Netzwerkschnittstellen ueber die Standardbibliothek.
// Plattformneutral - deshalb hier und nicht in den OS-Dateien.
func interfaces() []report.Interface {
	ifaces, err := net.Interfaces()
	if err != nil {
		return nil
	}
	// Plattformabhaengig: welche Schnittstellen bekommen ihre Adresse per DHCP.
	dhcp := dhcpInterfaces()
	out := make([]report.Interface, 0, len(ifaces))
	for _, ifc := range ifaces {
		if ifc.Flags&net.FlagLoopback != 0 || isVirtualInterface(ifc.Name) {
			continue
		}
		if ifc.HardwareAddr.String() == "" {
			continue
		}
		var ips []string
		if addrs, err := ifc.Addrs(); err == nil {
			for _, a := range addrs {
				if ipnet, ok := a.(*net.IPNet); ok {
					ips = append(ips, ipnet.IP.String())
				}
			}
		}
		out = append(out, report.Interface{
			Description: ifc.Name,
			MAC:         strings.ToUpper(ifc.HardwareAddr.String()),
			IPs:         ips,
			DHCP:        dhcp[ifc.Name],
		})
	}
	return out
}
