//go:build windows

package agent

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/registry"
	"golang.org/x/sys/windows/svc"
	"golang.org/x/sys/windows/svc/mgr"
)

const ServiceName = "VantorPulse"

// Service roda como LocalSystem: matricula o PC, garante um coletor em cada sessão aberta,
// manda o inventário a cada minuto e aplica a atualização pedida pelo painel.
type Service struct{}

func (Service) Execute(_ []string, req <-chan svc.ChangeRequest, status chan<- svc.Status) (bool, uint32) {
	status <- svc.Status{State: svc.StartPending}
	setupLog()
	setRecovery()
	status <- svc.Status{State: svc.Running, Accepts: svc.AcceptStop | svc.AcceptShutdown}

	stop := make(chan struct{})
	go serviceLoop(stop)
	for c := range req {
		switch c.Cmd {
		case svc.Interrogate:
			status <- c.CurrentStatus
		case svc.Stop, svc.Shutdown:
			close(stop)
			status <- svc.Status{State: svc.StopPending}
			return false, 0
		}
	}
	return false, 0
}

func setupLog() {
	_ = os.MkdirAll(MachineDir(), 0o755)
	if f, err := os.OpenFile(filepath.Join(MachineDir(), "servico.log"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644); err == nil {
		log.SetOutput(f)
	}
}

// setRecovery: se o serviço for encerrado, o Windows reinicia em 5 s.
func setRecovery() {
	m, err := mgr.Connect()
	if err != nil {
		return
	}
	defer m.Disconnect()
	s, err := m.OpenService(ServiceName)
	if err != nil {
		return
	}
	defer s.Close()
	_ = s.SetRecoveryActions([]mgr.RecoveryAction{{Type: mgr.ServiceRestart, Delay: 5 * time.Second}, {Type: mgr.ServiceRestart, Delay: 5 * time.Second}, {Type: mgr.ServiceRestart, Delay: 30 * time.Second}}, 86400)
}

func serviceLoop(stop <-chan struct{}) {
	var dev *Device
	started := false
	t := time.NewTicker(30 * time.Second)
	defer t.Stop()
	for n := 0; ; n++ {
		if dev == nil {
			d, err := ensureEnrolled()
			if err != nil {
				log.Printf("matrícula: %v", err)
			}
			dev = d
		}
		if dev != nil {
			ensureCollectors()
			if n%2 == 0 {
				b := &Batch{BatchID: newID(), AgentVersion: Version, Inventory: ptr(CollectInventory())}
				if !started {
					b.Sessions = []SessionEvent{{TS: time.Now().UTC().Format(time.RFC3339), Kind: "agent_start"}}
				}
				cfg, err := NewClient(dev.Server, dev.Token).Sync(b)
				switch {
				case err == ErrRevoked:
					log.Print("token revogado no painel; parando de enviar")
					dev = nil
				case err != nil:
					log.Printf("heartbeat: %v", err)
				default:
					started = true
					if cfg.Update != nil && cfg.Update.Version != Version {
						if err := applyUpdate(cfg.Update.URL, cfg.Update.SHA256); err != nil {
							log.Printf("atualização %s: %v", cfg.Update.Version, err)
						}
					}
				}
			}
		}
		select {
		case <-stop:
			return
		case <-t.C:
		}
	}
}

func ptr[T any](v T) *T { return &v }

// ensureEnrolled lê o servidor e o código da empresa gravados pelo instalador e matricula o PC.
func ensureEnrolled() (*Device, error) {
	if d, err := LoadDevice(); err == nil {
		return d, nil
	}
	k, err := registry.OpenKey(registry.LOCAL_MACHINE, `SOFTWARE\VantorPulse`, registry.QUERY_VALUE)
	if err != nil {
		return nil, fmt.Errorf("instalação incompleta (sem registro): %w", err)
	}
	server, _, _ := k.GetStringValue("Server")
	token, _, _ := k.GetStringValue("Token")
	msi, _, _ := k.GetStringValue("MsiPath")
	k.Close()
	if token == "" {
		token = TokenFromMsiName(msi)
	}
	server = strings.TrimRight(server, "/")
	if server == "" || token == "" {
		return nil, fmt.Errorf("faltou o servidor ou o código da empresa (instale com TOKEN=... SERVER=... ou pelo arquivo VantorPulse_<código>.msi)")
	}
	id, devToken, err := NewClient(server, "").Enroll(token, CollectInventory())
	if err != nil {
		return nil, err
	}
	d := &Device{Server: server, DeviceID: id, Token: devToken}
	return d, SaveDevice(d)
}

// ensureCollectors abre o coletor (com o ícone na bandeja) em cada sessão de usuário ativa que ainda não tem um.
func ensureCollectors() {
	exe, err := os.Executable()
	if err != nil {
		return
	}
	running := collectorSessions(filepath.Base(exe))
	var sessions *windows.WTS_SESSION_INFO
	var count uint32
	if err := windows.WTSEnumerateSessions(0, 0, 1, &sessions, &count); err != nil {
		return
	}
	defer windows.WTSFreeMemory(uintptr(unsafe.Pointer(sessions)))
	for _, s := range unsafe.Slice(sessions, count) {
		if s.State != windows.WTSActive || s.SessionID == 0 || running[s.SessionID] {
			continue
		}
		if err := startInSession(s.SessionID, exe); err != nil {
			log.Printf("coletor na sessão %d: %v", s.SessionID, err)
		}
	}
}

func collectorSessions(name string) map[uint32]bool {
	out := map[uint32]bool{}
	snap, err := windows.CreateToolhelp32Snapshot(windows.TH32CS_SNAPPROCESS, 0)
	if err != nil {
		return out
	}
	defer windows.CloseHandle(snap)
	var pe windows.ProcessEntry32
	pe.Size = uint32(unsafe.Sizeof(pe))
	for err = windows.Process32First(snap, &pe); err == nil; err = windows.Process32Next(snap, &pe) {
		if strings.EqualFold(windows.UTF16ToString(pe.ExeFile[:]), name) {
			var sid uint32
			if windows.ProcessIdToSessionId(pe.ProcessID, &sid) == nil && sid != 0 {
				out[sid] = true
			}
		}
	}
	return out
}

func startInSession(session uint32, exe string) error {
	var tok windows.Token
	if err := windows.WTSQueryUserToken(session, &tok); err != nil {
		return err
	}
	defer tok.Close()
	var env *uint16
	if err := windows.CreateEnvironmentBlock(&env, tok, false); err != nil {
		return err
	}
	defer windows.DestroyEnvironmentBlock(env)
	cmd, _ := windows.UTF16PtrFromString(fmt.Sprintf(`"%s" collect`, exe))
	desk, _ := windows.UTF16PtrFromString(`winsta0\default`)
	si := windows.StartupInfo{Desktop: desk}
	si.Cb = uint32(unsafe.Sizeof(si))
	var pi windows.ProcessInformation
	if err := windows.CreateProcessAsUser(tok, nil, cmd, nil, nil, false, windows.CREATE_UNICODE_ENVIRONMENT, env, nil, &si, &pi); err != nil {
		return err
	}
	windows.CloseHandle(pi.Thread)
	windows.CloseHandle(pi.Process)
	return nil
}

// applyUpdate baixa o MSI, confere o SHA-256 e instala em silêncio (o MSI reinicia o serviço).
func applyUpdate(url, wantSHA string) error {
	if !strings.HasPrefix(url, "https://") {
		return fmt.Errorf("endereço de atualização precisa ser https")
	}
	dir := filepath.Join(MachineDir(), "atualizacao")
	_ = os.MkdirAll(dir, 0o755)
	path := filepath.Join(dir, "VantorPulse.msi")
	res, err := http.Get(url)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	if res.StatusCode != 200 {
		return fmt.Errorf("download respondeu %d", res.StatusCode)
	}
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	h := sha256.New()
	_, err = io.Copy(io.MultiWriter(f, h), io.LimitReader(res.Body, 200<<20))
	f.Close()
	if err != nil {
		return err
	}
	if got := hex.EncodeToString(h.Sum(nil)); !strings.EqualFold(got, wantSHA) {
		os.Remove(path)
		return fmt.Errorf("SHA-256 não confere (%s)", got)
	}
	log.Printf("instalando atualização de %s", url)
	return exec.Command("msiexec", "/i", path, "/qn", "/norestart", "/l*v", filepath.Join(dir, "msiexec.log")).Start()
}

// InstallService/RemoveService servem para testes sem o MSI (o MSI faz isso na instalação real).
func InstallService(exe string) error {
	m, err := mgr.Connect()
	if err != nil {
		return err
	}
	defer m.Disconnect()
	s, err := m.CreateService(ServiceName, exe, mgr.Config{DisplayName: "Vantor Pulse", Description: "Agente transparente de produtividade (Vantor Pulse).", StartType: mgr.StartAutomatic}, "service")
	if err != nil {
		return err
	}
	defer s.Close()
	return s.Start()
}

func RemoveService() error {
	m, err := mgr.Connect()
	if err != nil {
		return err
	}
	defer m.Disconnect()
	s, err := m.OpenService(ServiceName)
	if err != nil {
		return err
	}
	defer s.Close()
	_, _ = s.Control(svc.Stop)
	return s.Delete()
}
