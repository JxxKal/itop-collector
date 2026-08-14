//go:build windows

package collect

import (
	"strings"
	"time"

	"github.com/JxxKal/itop-collector/internal/report"
	"github.com/yusufpapurcu/wmi"
)

// WMI-Ergebnisstrukturen.
//
// Die Feldnamen muessen exakt den WMI-Eigenschaften entsprechen - das Paket
// bildet ueber Reflection ab. Zeiger dort, wo WMI NULL liefern kann; ein
// nicht-Zeiger-Feld wuerde dann den Nullwert bekommen und die Unterscheidung
// "nicht gesetzt" gegen "leer" verlieren.
type win32ComputerSystem struct {
	Name                *string
	Domain              *string
	Manufacturer        *string
	Model               *string
	TotalPhysicalMemory *uint64
}

type win32BIOS struct {
	SerialNumber *string
}

type win32OperatingSystem struct {
	Caption *string
	Version *string
}

type win32Processor struct {
	Name *string
}

type win32LogicalDisk struct {
	DeviceID  *string
	Size      *uint64
	FreeSpace *uint64
}

func str(p *string) string {
	if p == nil {
		return ""
	}
	return strings.TrimSpace(*p)
}

func u64(p *uint64) int64 {
	if p == nil {
		return 0
	}
	return int64(*p)
}

// queryWMI kapselt die Abfrage und schluckt Fehler bewusst.
//
// Der Agent darf nie abbrechen: ein blockierter WMI-Dienst, ein beschaedigtes
// Repository oder fehlende Rechte fuehren zu leeren Feldern, nicht zum Ende der
// Sammlung. Fehler landen im Log der aufrufenden Ebene, nicht hier.
func queryWMI[T any](query string) []T {
	var dst []T
	if err := wmi.Query(query, &dst); err != nil {
		return nil
	}
	return dst
}

func collectPlatform(agentGUID, agentVersion string) *report.Report {
	rep := &report.Report{
		AgentGUID:    agentGUID,
		AgentVersion: agentVersion,
		CollectedAt:  time.Now().UTC(),
		OSFamily:     report.OSWindows,
		Interfaces:   interfaces(),
		Disks:        disks(),
		Software:     software(),
	}

	if cs := queryWMI[win32ComputerSystem](
		"SELECT Name, Domain, Manufacturer, Model, TotalPhysicalMemory FROM Win32_ComputerSystem",
	); len(cs) > 0 {
		rep.Hostname = str(cs[0].Name)
		rep.Domain = str(cs[0].Domain)
		rep.Manufacturer = str(cs[0].Manufacturer)
		rep.Model = str(cs[0].Model)
		rep.RAMBytes = u64(cs[0].TotalPhysicalMemory)
	}

	if bios := queryWMI[win32BIOS]("SELECT SerialNumber FROM Win32_BIOS"); len(bios) > 0 {
		rep.SerialNumber = SanitizeSerial(str(bios[0].SerialNumber))
	}

	if os := queryWMI[win32OperatingSystem](
		"SELECT Caption, Version FROM Win32_OperatingSystem",
	); len(os) > 0 {
		rep.OSName = str(os[0].Caption)
		rep.OSVersion = str(os[0].Version)
	}

	// Bei mehreren Prozessoren wird der erste genommen: iTop fuehrt cpu als
	// einzelnes Textfeld, eine Liste passt nicht hinein.
	if cpu := queryWMI[win32Processor]("SELECT Name FROM Win32_Processor"); len(cpu) > 0 {
		rep.CPU = str(cpu[0].Name)
	}

	rep.Virtualization = detectVirtualization(rep.Manufacturer, rep.Model)
	return rep
}

// detectVirtualization erkennt den Hypervisor an Hersteller und Modell.
//
// Win32_ComputerSystem meldet unter Virtualisierung den Hypervisor als
// Hersteller - "VMware, Inc.", "Microsoft Corporation" mit Modell "Virtual
// Machine", "QEMU" und so weiter. Das reicht aus und kostet keine zusaetzliche
// WMI-Abfrage.
//
// Leer heisst "physisch oder nicht erkennbar". Der Collector behandelt beides
// gleich - im Zweifel lieber ein PC/Server-CI als eine VirtualMachine, die er
// ohnehin nicht anlegen koennte.
func detectVirtualization(manufacturer, model string) string {
	h := strings.ToLower(manufacturer + " " + model)
	switch {
	case strings.Contains(h, "vmware"):
		return "vmware"
	case strings.Contains(h, "qemu"), strings.Contains(h, "kvm"), strings.Contains(h, "bochs"):
		return "kvm"
	case strings.Contains(h, "virtualbox"), strings.Contains(h, "innotek"):
		return "virtualbox"
	case strings.Contains(h, "xen"):
		return "xen"
	case strings.Contains(h, "parallels"):
		return "parallels"
	// Erst zuletzt pruefen: "Virtual Machine" allein ist zu unspezifisch und
	// taucht auch bei anderen Herstellern auf.
	case strings.Contains(h, "microsoft") && strings.Contains(h, "virtual"):
		return "hyperv"
	}
	return ""
}

// disks liest die lokalen Festplatten.
//
// DriveType=3 ist "Local Disk" - damit fallen Netzlaufwerke, CD-ROMs und
// Wechselmedien heraus. Die haben in der CMDB als Datentraeger der Maschine
// nichts verloren.
func disks() []report.Disk {
	rows := queryWMI[win32LogicalDisk](
		"SELECT DeviceID, Size, FreeSpace FROM Win32_LogicalDisk WHERE DriveType=3")
	out := make([]report.Disk, 0, len(rows))
	for _, d := range rows {
		out = append(out, report.Disk{
			DeviceID:  str(d.DeviceID),
			SizeBytes: u64(d.Size),
			FreeBytes: u64(d.FreeSpace),
		})
	}
	return out
}
