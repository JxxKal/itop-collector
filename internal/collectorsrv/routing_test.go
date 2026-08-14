package collectorsrv

import (
	"testing"

	"github.com/JxxKal/itop-collector/internal/report"
)

func TestClassifyFresh(t *testing.T) {
	faelle := []struct {
		name string
		rep  report.Report
		want TargetClass
	}{
		{"Windows-Client", report.Report{OSFamily: report.OSWindows, OSName: "Windows 11 Pro"}, ClassPC},
		{"Windows-Server", report.Report{OSFamily: report.OSWindows, OSName: "Windows Server 2022 Standard"}, ClassServer},
		{"Debian ohne Desktop", report.Report{OSFamily: report.OSLinux, OSName: "Debian GNU/Linux"}, ClassServer},
		{"Ubuntu Desktop", report.Report{OSFamily: report.OSLinux, OSName: "Ubuntu Desktop"}, ClassPC},
		// Virtualisierung schlaegt das Betriebssystem - auch bei einem
		// Windows-Server, der auf VMware laeuft.
		{"KVM-Gast", report.Report{OSFamily: report.OSLinux, OSName: "Debian GNU/Linux", Virtualization: "kvm"}, ClassVirtualMachine},
		{"VMware mit Windows Server", report.Report{OSFamily: report.OSWindows, OSName: "Windows Server 2022", Virtualization: "vmware"}, ClassVirtualMachine},
		// Ein Container ist kein Hardware-CI und darf nicht als VM gelten.
		{"Container", report.Report{OSFamily: report.OSLinux, OSName: "Debian GNU/Linux", Virtualization: "container"}, ClassServer},
	}
	for _, f := range faelle {
		if got := classifyFresh(&f.rep); got != f.want {
			t.Errorf("%s: got %q, want %q", f.name, got, f.want)
		}
	}
}
