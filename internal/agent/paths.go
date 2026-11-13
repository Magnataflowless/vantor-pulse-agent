package agent

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
)

// Pastas: MachineDir (do PC) e UserDir (de cada usuário) ficam nos arquivos de cada sistema.

// Device é a identidade deste PC na API (no Windows, protegida pelo DPAPI).
type Device struct {
	Server   string `json:"server"`
	DeviceID string `json:"deviceId"`
	Token    string `json:"token"`
}

func devicePath() string { return filepath.Join(MachineDir(), "device.bin") }

// UserState guarda o que o colaborador já aceitou e o uso de pausas do dia.
type UserState struct {
	AcceptedTerms []int          `json:"acceptedTerms"`
	PauseUsedDay  string         `json:"pauseUsedDay"`
	PauseUsedMin  map[string]int `json:"pauseUsedMin"`
}

func (s *UserState) Accepted(v int) bool {
	for _, x := range s.AcceptedTerms {
		if x == v {
			return true
		}
	}
	return false
}

func readJSON(path string, v any) error {
	b, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	return json.Unmarshal(b, v)
}

func writeJSON(path string, v any) error {
	b, err := json.Marshal(v)
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

func newID() string {
	b := make([]byte, 12)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

var msiToken = regexp.MustCompile(`(?i)VantorPulse_([A-Za-z0-9_-]{16,64})`)

// TokenFromMsiName lê o código da empresa no nome do instalador ("VantorPulse_<token> (1).msi").
func TokenFromMsiName(path string) string {
	if m := msiToken.FindStringSubmatch(filepath.Base(path)); m != nil {
		return m[1]
	}
	return ""
}
