//go:build linux

package collect

import (
	"bufio"
	"os"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/JxxKal/itop-collector/internal/report"
)

const dmiPath = "/sys/class/dmi/id/"

// readTrimmed liest eine Datei und gibt sie ohne Rand-Leerzeichen zurueck.
// Fehler werden bewusst verschluckt: fehlende /sys-Pfade sind auf Containern,
// ARM-Boards und manchen VMs normal und duerfen die Sammlung nicht abbrechen.
func readTrimmed(path string) string {
	b, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(b))
}

func collectPlatform(agentGUID, agentVersion string) *report.Report {
	rep := &report.Report{
		AgentGUID:    agentGUID,
		AgentVersion: agentVersion,
		CollectedAt:  time.Now().UTC(),
		OSFamily:     report.OSLinux,
		Hostname:     hostname(),
		Domain:       domain(),
		Manufacturer: readTrimmed(dmiPath + "sys_vendor"),
		Model:        readTrimmed(dmiPath + "product_name"),
		// product_serial ist nur fuer root lesbar (0400). Laeuft der Agent als
		// Dienst, ist das gegeben; im interaktiven Test als normaler Benutzer
		// bleibt das Feld leer - das ist kein Fehler.
		SerialNumber:   SanitizeSerial(readTrimmed(dmiPath + "product_serial")),
		CPU:            cpuModel(),
		RAMBytes:       memTotalBytes(),
		Virtualization: detectVirtualization(),
		Interfaces:     interfaces(),
		Disks:          disks(),
		Software:       software(),
	}
	rep.OSName, rep.OSVersion = osRelease()
	return rep
}

func hostname() string {
	h, err := os.Hostname()
	if err != nil {
		return ""
	}
	// os.Hostname kann den FQDN liefern; iTop fuehrt den Namen ohne Domaene.
	if i := strings.Index(h, "."); i > 0 {
		return h[:i]
	}
	return h
}

func domain() string {
	h, err := os.Hostname()
	if err != nil {
		return ""
	}
	if i := strings.Index(h, "."); i > 0 {
		return h[i+1:]
	}
	return ""
}

// osRelease liefert (Name, Version) aus /etc/os-release.
//
// NAME statt PRETTY_NAME: PRETTY_NAME enthaelt die Version ("Debian GNU/Linux 13
// (trixie)"), und der Collector legt daraus eine iTop-OSFamily an. Mit
// PRETTY_NAME entstuende pro Release eine neue "Familie" - der Katalog waere
// nach kurzer Zeit unbrauchbar.
func osRelease() (name, version string) {
	f, err := os.Open("/etc/os-release")
	if err != nil {
		return "", ""
	}
	defer f.Close()

	sc := bufio.NewScanner(f)
	for sc.Scan() {
		key, val, ok := strings.Cut(sc.Text(), "=")
		if !ok {
			continue
		}
		val = strings.Trim(val, `"'`)
		switch key {
		case "NAME":
			name = val
		case "VERSION_ID":
			version = val
		}
	}
	return name, version
}

func cpuModel() string {
	f, err := os.Open("/proc/cpuinfo")
	if err != nil {
		return ""
	}
	defer f.Close()

	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := sc.Text()
		// x86 nutzt "model name", ARM "Model name" bzw. "Hardware".
		for _, key := range []string{"model name", "Model name", "Hardware", "cpu model"} {
			if strings.HasPrefix(line, key) {
				if _, val, ok := strings.Cut(line, ":"); ok {
					return strings.TrimSpace(val)
				}
			}
		}
	}
	return ""
}

func memTotalBytes() int64 {
	f, err := os.Open("/proc/meminfo")
	if err != nil {
		return 0
	}
	defer f.Close()

	sc := bufio.NewScanner(f)
	for sc.Scan() {
		if !strings.HasPrefix(sc.Text(), "MemTotal:") {
			continue
		}
		fields := strings.Fields(sc.Text())
		if len(fields) < 2 {
			return 0
		}
		kb, err := strconv.ParseInt(fields[1], 10, 64)
		if err != nil {
			return 0
		}
		return kb * 1024 // MemTotal steht in kB
	}
	return 0
}

// detectVirtualization erkennt die Virtualisierung anhand des DMI.
//
// Bewusst ohne systemd-detect-virt: das Binary fehlt auf Systemen ohne systemd,
// und der Agent soll abhaengigkeitsfrei bleiben. Die DMI-Kennungen sind
// eindeutig genug - Hypervisoren tragen sich dort selbst ein.
//
// Ein leerer Rueckgabewert heisst "physisch oder nicht erkennbar". Der Collector
// behandelt beides gleich, weil er im Zweifel lieber ein PC/Server-CI anlegt als
// eine VirtualMachine, die er ohnehin nicht anlegen koennte.
func detectVirtualization() string {
	// Container ZUERST pruefen. Ein Container sieht das DMI seines Wirts - laeuft
	// er in einer VM, meldete die DMI-Pruefung "kvm" und der Collector legte ein
	// VirtualMachine-CI an, das es gar nicht gibt. Der Container ist die
	// speziellere Aussage und hat deshalb Vorrang.
	if isContainer() {
		return "container"
	}

	haystack := strings.ToLower(strings.Join([]string{
		readTrimmed(dmiPath + "sys_vendor"),
		readTrimmed(dmiPath + "product_name"),
		readTrimmed(dmiPath + "chassis_vendor"),
		readTrimmed(dmiPath + "bios_vendor"),
	}, " "))

	// Reihenfolge zaehlt: "VMware" steht auch in manchen QEMU-Feldern nicht,
	// aber "Virtual Machine" ist unspezifisch und muss zuletzt greifen.
	switch {
	case strings.Contains(haystack, "qemu"), strings.Contains(haystack, "kvm"):
		return "kvm"
	case strings.Contains(haystack, "vmware"):
		return "vmware"
	case strings.Contains(haystack, "virtualbox"), strings.Contains(haystack, "innotek"):
		return "virtualbox"
	case strings.Contains(haystack, "xen"):
		return "xen"
	case strings.Contains(haystack, "parallels"):
		return "parallels"
	case strings.Contains(haystack, "bochs"):
		return "kvm"
	case strings.Contains(haystack, "microsoft") && strings.Contains(haystack, "virtual"):
		return "hyperv"
	}
	return ""
}

// isContainer erkennt die gaengigen Container-Laufzeiten.
//
// Mehrere Merkmale, weil keines allein zuverlaessig ist: Docker legt
// /.dockerenv an, Podman /run/.containerenv, cgroup v1 traegt die Laufzeit im
// Pfad. Unter cgroup v2 ist /proc/1/cgroup oft nur noch "0::/" - dort greift
// dann die von systemd gesetzte Variable container= in /proc/1/environ.
func isContainer() bool {
	if _, err := os.Stat("/.dockerenv"); err == nil {
		return true
	}
	if _, err := os.Stat("/run/.containerenv"); err == nil { // Podman
		return true
	}
	cg := readTrimmed("/proc/1/cgroup")
	for _, marker := range []string{"docker", "lxc", "containerd", "kubepods", "libpod"} {
		if strings.Contains(cg, marker) {
			return true
		}
	}
	// systemd setzt das in seiner Umgebung, wenn es in einem Container laeuft.
	if strings.Contains(readTrimmed("/proc/1/environ"), "container=") {
		return true
	}
	return false
}

// skipFilesystems sind Pseudo-Dateisysteme ohne eigenen Datentraeger. Sie in die
// CMDB zu schreiben erzeugt nur Rauschen.
var skipFilesystems = map[string]bool{
	"proc": true, "sysfs": true, "devtmpfs": true, "devpts": true, "tmpfs": true,
	"securityfs": true, "cgroup": true, "cgroup2": true, "pstore": true,
	"efivarfs": true, "bpf": true, "debugfs": true, "tracefs": true,
	"mqueue": true, "hugetlbfs": true, "fusectl": true, "configfs": true,
	"binfmt_misc": true, "autofs": true, "ramfs": true, "squashfs": true,
	"overlay": true, "nsfs": true, "rpc_pipefs": true,
}

func disks() []report.Disk {
	f, err := os.Open("/proc/mounts")
	if err != nil {
		return nil
	}
	defer f.Close()

	seen := map[string]bool{}
	var out []report.Disk
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		fields := strings.Fields(sc.Text())
		if len(fields) < 3 {
			continue
		}
		device, mountpoint, fstype := fields[0], fields[1], fields[2]
		if skipFilesystems[fstype] || !strings.HasPrefix(device, "/") || seen[device] {
			continue
		}
		var st syscall.Statfs_t
		if err := syscall.Statfs(mountpoint, &st); err != nil {
			continue
		}
		seen[device] = true
		out = append(out, report.Disk{
			DeviceID: device,
			// Bsize ist die Blockgroesse; Blocks/Bavail sind Blockzahlen.
			SizeBytes: int64(st.Blocks) * st.Bsize,
			// Bavail statt Bfree: Bfree enthaelt die fuer root reservierten
			// Bloecke, die kein normaler Prozess nutzen kann.
			FreeBytes: int64(st.Bavail) * st.Bsize,
		})
	}
	return out
}
