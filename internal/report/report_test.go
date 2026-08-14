package report

import "testing"

func TestPrimaryIP(t *testing.T) {
	faelle := []struct {
		name string
		rep  Report
		want string
	}{
		{
			name: "statische Adresse wird genommen",
			rep: Report{Interfaces: []Interface{
				{Description: "eth0", IPs: []string{"192.0.2.50"}, DHCP: false},
			}},
			want: "192.0.2.50",
		},
		{
			// Der Kern der Anforderung: DHCP-Adressen gehoeren nicht in die CMDB.
			name: "DHCP-Adresse wird uebersprungen",
			rep: Report{Interfaces: []Interface{
				{Description: "eth0", IPs: []string{"192.0.2.70"}, DHCP: true},
			}},
			want: "",
		},
		{
			name: "statisch schlaegt DHCP, unabhaengig von der Reihenfolge",
			rep: Report{Interfaces: []Interface{
				{Description: "eth0", IPs: []string{"10.0.0.5"}, DHCP: true},
				{Description: "eth1", IPs: []string{"192.0.2.50"}, DHCP: false},
			}},
			want: "192.0.2.50",
		},
		{
			// 169.254/16 vergibt Windows, wenn kein DHCP-Server antwortet. Die
			// Adresse ist NICHT als DHCP markiert, sagt aber nichts aus.
			name: "APIPA wird uebersprungen",
			rep: Report{Interfaces: []Interface{
				{Description: "eth0", IPs: []string{"169.254.13.7"}, DHCP: false},
			}},
			want: "",
		},
		{
			name: "IPv6 zaehlt nicht, IPv4 daneben schon",
			rep: Report{Interfaces: []Interface{
				{Description: "eth0", IPs: []string{"fe80::1", "2001:db8::1", "192.0.2.50"}, DHCP: false},
			}},
			want: "192.0.2.50",
		},
		{
			name: "Loopback wird uebersprungen",
			rep: Report{Interfaces: []Interface{
				{Description: "lo", IPs: []string{"127.0.0.1"}, DHCP: false},
			}},
			want: "",
		},
		{
			name: "Muell wird ignoriert",
			rep: Report{Interfaces: []Interface{
				{Description: "eth0", IPs: []string{"nicht-ip", ""}, DHCP: false},
			}},
			want: "",
		},
		{name: "keine Schnittstellen", rep: Report{}, want: ""},
	}
	for _, f := range faelle {
		if got := f.rep.PrimaryIP(); got != f.want {
			t.Errorf("%s: got %q, want %q", f.name, got, f.want)
		}
	}
}
