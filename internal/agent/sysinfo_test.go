package agent

import "testing"

func TestParseLinuxInfo(t *testing.T) {
	name, ver := parseOSRelease("PRETTY_NAME=\"Ubuntu 24.04.1 LTS\"\nNAME=\"Ubuntu\"\nVERSION=\"24.04.1 LTS (Noble Numbat)\"\n")
	if name != "Ubuntu" || ver != "24.04.1 LTS (Noble Numbat)" {
		t.Fatalf("os-release: %q %q", name, ver)
	}
	if n, _ := parseOSRelease(""); n != "Linux" {
		t.Fatalf("os-release vazio: %q", n)
	}
	if cpu := parseCPU("processor\t: 0\nmodel name\t: Intel(R) Core(TM) i5-8250U CPU @ 1.60GHz\nprocessor\t: 1\nmodel name\t: outro\n"); cpu != "Intel(R) Core(TM) i5-8250U CPU @ 1.60GHz" {
		t.Fatalf("cpu: %q", cpu)
	}
	if mb := parseMemTotalMb("MemTotal:        8041256 kB\nMemFree:  1 kB\n"); mb != 7852 {
		t.Fatalf("memória: %d", mb)
	}
}
