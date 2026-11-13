package agent

// Leitura de dados técnicos do PC que não dependem do sistema (testada em sysinfo_test.go).

import (
	"net"
	"strings"
)

func localIP() string {
	ifaces, _ := net.Interfaces()
	for _, i := range ifaces {
		if i.Flags&net.FlagUp == 0 || i.Flags&net.FlagLoopback != 0 {
			continue
		}
		addrs, _ := i.Addrs()
		for _, a := range addrs {
			if ip, ok := a.(*net.IPNet); ok && ip.IP.To4() != nil && !ip.IP.IsLinkLocalUnicast() {
				return ip.IP.String()
			}
		}
	}
	return ""
}

// parseOSRelease lê /etc/os-release: nome ("Ubuntu") e versão ("24.04.1 LTS (Noble Numbat)").
func parseOSRelease(s string) (name, version string) {
	kv := map[string]string{}
	for _, line := range strings.Split(s, "\n") {
		if k, v, ok := strings.Cut(strings.TrimSpace(line), "="); ok {
			kv[k] = strings.Trim(v, `"'`)
		}
	}
	name = kv["NAME"]
	if name == "" {
		name = "Linux"
	}
	return name, kv["VERSION"]
}

// parseCPU: o primeiro "model name" de /proc/cpuinfo.
func parseCPU(s string) string {
	for _, line := range strings.Split(s, "\n") {
		if k, v, ok := strings.Cut(line, ":"); ok && strings.TrimSpace(k) == "model name" {
			return strings.TrimSpace(v)
		}
	}
	return ""
}

// parseMemTotalMb: MemTotal de /proc/meminfo (em kB) convertido para MB.
func parseMemTotalMb(s string) int {
	for _, line := range strings.Split(s, "\n") {
		if strings.HasPrefix(line, "MemTotal:") {
			return atoi(strings.TrimSpace(strings.TrimPrefix(line, "MemTotal:"))) / 1024
		}
	}
	return 0
}

func atoi(s string) int {
	n := 0
	for _, c := range s {
		if c < '0' || c > '9' {
			break
		}
		n = n*10 + int(c-'0')
	}
	return n
}
