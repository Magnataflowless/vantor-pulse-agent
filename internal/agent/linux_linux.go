//go:build linux

package agent

// Linux (desktop X11): janela em foco pelo EWMH (_NET_ACTIVE_WINDOW), ociosidade pela extensão
// MIT-SCREEN-SAVER, caixas de diálogo pelo zenity (ou kdialog). Cliente X11 em Go puro (jezek/xgb), sem cgo.

import (
	"crypto/rand"
	"fmt"
	"log"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/jezek/xgb"
	"github.com/jezek/xgb/screensaver"
	"github.com/jezek/xgb/xproto"
)

// Pastas: /etc/vantor-pulse (do PC, gravada pelo "vantor-agent enroll" como root) e ~/.local/share/vantor-pulse (de cada usuário).
func MachineDir() string { return "/etc/vantor-pulse" }
func UserDir() string {
	if d := os.Getenv("XDG_DATA_HOME"); d != "" {
		return filepath.Join(d, "vantor-pulse")
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".local", "share", "vantor-pulse")
}

var x11 struct {
	sync.Mutex
	conn  *xgb.Conn
	root  xproto.Window
	ss    bool
	atoms map[string]xproto.Atom
	err   error
	tried time.Time
}

// conn abre a conexão com o X na primeira vez (e tenta de novo a cada minuto se falhar).
func conn() *xgb.Conn {
	x11.Lock()
	defer x11.Unlock()
	if x11.conn != nil || time.Since(x11.tried) < time.Minute {
		return x11.conn
	}
	x11.tried = time.Now()
	c, err := xgb.NewConn()
	if err != nil {
		x11.err = err
		return nil
	}
	x11.conn, x11.err = c, nil
	x11.root = xproto.Setup(c).DefaultScreen(c).Root
	x11.ss = screensaver.Init(c) == nil
	x11.atoms = map[string]xproto.Atom{}
	return c
}

func atom(c *xgb.Conn, name string) xproto.Atom {
	x11.Lock()
	a, ok := x11.atoms[name]
	x11.Unlock()
	if ok {
		return a
	}
	r, err := xproto.InternAtom(c, false, uint16(len(name)), name).Reply()
	if err != nil {
		return 0
	}
	x11.Lock()
	x11.atoms[name] = r.Atom
	x11.Unlock()
	return r.Atom
}

func prop(c *xgb.Conn, w xproto.Window, name string) []byte {
	r, err := xproto.GetProperty(c, false, w, atom(c, name), xproto.GetPropertyTypeAny, 0, 1<<16).Reply()
	if err != nil || r == nil {
		return nil
	}
	return r.Value
}

// PlatformProblem explica quando o Linux não deixa coletar (sem X11, ou Wayland, que esconde as janelas).
func PlatformProblem() string {
	if os.Getenv("XDG_SESSION_TYPE") == "wayland" {
		return "Sessão Wayland: entre com a sessão Xorg para o registro funcionar"
	}
	if conn() == nil {
		return "Sem acesso à tela (X11): o registro está parado"
	}
	return ""
}

// Foreground devolve o programa (minúsculo), o título e o id da janela em foco.
func Foreground() (exe, title string, hwnd uintptr) {
	c := conn()
	if c == nil {
		return "", "", 0
	}
	v := prop(c, x11.root, "_NET_ACTIVE_WINDOW")
	if len(v) < 4 {
		return "", "", 0
	}
	w := xproto.Window(xgb.Get32(v))
	if w == 0 {
		return "", "", 0
	}
	title = string(prop(c, w, "_NET_WM_NAME"))
	if title == "" {
		title = string(prop(c, w, "WM_NAME"))
	}
	if p := prop(c, w, "_NET_WM_PID"); len(p) >= 4 {
		exe = processName(int(xgb.Get32(p)))
	}
	if exe == "" { // sem PID: usa a classe da janela (ex.: "firefox")
		if cls := strings.Split(string(prop(c, w, "WM_CLASS")), "\x00"); len(cls) > 1 {
			exe = strings.ToLower(cls[1])
		}
	}
	return exe, title, uintptr(w)
}

// processName: nome do executável do processo (ex.: "firefox", "libreoffice").
func processName(pid int) string {
	if pid <= 0 {
		return ""
	}
	if p, err := os.Readlink(fmt.Sprintf("/proc/%d/exe", pid)); err == nil {
		return strings.ToLower(filepath.Base(strings.TrimSuffix(p, " (deleted)")))
	}
	b, _ := os.ReadFile(fmt.Sprintf("/proc/%d/comm", pid))
	return strings.ToLower(strings.TrimSpace(string(b)))
}

// IsFullscreen: a janela está em tela cheia (vídeo ou reunião).
func IsFullscreen(hwnd uintptr) bool {
	c := conn()
	if c == nil || hwnd == 0 {
		return false
	}
	want := atom(c, "_NET_WM_STATE_FULLSCREEN")
	v := prop(c, xproto.Window(hwnd), "_NET_WM_STATE")
	for i := 0; i+4 <= len(v); i += 4 {
		if xproto.Atom(xgb.Get32(v[i:])) == want {
			return true
		}
	}
	return false
}

func screenSaver() (msIdle uint32, on bool) {
	c := conn()
	if c == nil || !x11.ss {
		return 0, false
	}
	r, err := screensaver.QueryInfo(c, xproto.Drawable(x11.root)).Reply()
	if err != nil {
		return 0, false
	}
	return r.MsSinceUserInput, r.State == screensaver.StateOn
}

// LastInputTick muda a cada uso de teclado ou mouse (segundo da última entrada; nunca o que foi digitado).
func LastInputTick() uint32 {
	ms, _ := screenSaver()
	return uint32((time.Now().UnixMilli() - int64(ms)) / 1000)
}

func IdleSeconds() int {
	ms, _ := screenSaver()
	return int(ms / 1000)
}

// IsLocked: protetor de tela ligado ou programa de bloqueio em foco.
func IsLocked(exe string) bool {
	if _, on := screenSaver(); on {
		return true
	}
	switch exe {
	case "i3lock", "xscreensaver", "light-locker", "gnome-screensaver", "xsecurelock", "slock":
		return true
	}
	return false
}

// O navegador não expõe o endereço no Linux sem acessibilidade: por enquanto o site aparece como o próprio navegador.
type UIA struct{}

func NewUIA() *UIA                          { return &UIA{} }
func (*UIA) Domain(uintptr) string          { return "" }
func IsBrowser(string) bool                 { return false }
func OpenURL(url string)                    { _ = exec.Command("xdg-open", url).Start() }
func Protect(data []byte) ([]byte, error)   { return data, nil } // sem DPAPI: o arquivo fica só com permissão de leitura local
func Unprotect(data []byte) ([]byte, error) { return data, nil }

// Ask mostra Sim/Não (Termo de Ciência) com zenity ou kdialog.
func Ask(title, text string) bool {
	if p, err := exec.LookPath("zenity"); err == nil {
		return exec.Command(p, "--question", "--title="+title, "--text="+text, "--width=640", "--ok-label=Aceito", "--cancel-label=Agora não").Run() == nil
	}
	if p, err := exec.LookPath("kdialog"); err == nil {
		return exec.Command(p, "--title", title, "--yesno", text).Run() == nil
	}
	log.Printf("sem zenity nem kdialog para mostrar o Termo de Ciência (instale: sudo apt install zenity)")
	return false
}

// Notify mostra uma mensagem (zenity, kdialog ou notificação do sistema).
func Notify(title, text string) {
	if p, err := exec.LookPath("zenity"); err == nil {
		_ = exec.Command(p, "--info", "--title="+title, "--text="+text, "--width=640").Run()
	} else if p, err := exec.LookPath("kdialog"); err == nil {
		_ = exec.Command(p, "--title", title, "--msgbox", text).Run()
	} else {
		_ = exec.Command("notify-send", title, text).Run()
	}
}

// WinUser: o login do Linux, em minúsculas (mesmo campo do usuário do Windows na API).
func WinUser() string {
	if u, err := user.Current(); err == nil {
		return strings.ToLower(u.Username)
	}
	return strings.ToLower(os.Getenv("USER"))
}

// QueueKey: chave da fila deste usuário, num arquivo que só ele lê (0600).
func QueueKey(dir string) ([]byte, error) {
	path := filepath.Join(dir, "fila.key")
	if k, err := os.ReadFile(path); err == nil && len(k) == 32 {
		return k, nil
	}
	k := make([]byte, 32)
	if _, err := rand.Read(k); err != nil {
		return nil, err
	}
	return k, os.WriteFile(path, k, 0o600)
}

// CollectInventory lê os dados técnicos do PC (sem nada pessoal).
func CollectInventory() Inventory {
	inv := Inventory{OS: "Linux"}
	inv.Hostname, _ = os.Hostname()
	if b, err := os.ReadFile("/etc/os-release"); err == nil {
		inv.OS, inv.OSVersion = parseOSRelease(string(b))
	}
	if b, err := os.ReadFile("/proc/cpuinfo"); err == nil {
		inv.CPU = parseCPU(string(b))
	}
	if b, err := os.ReadFile("/proc/meminfo"); err == nil {
		inv.RAMMb = parseMemTotalMb(string(b))
	}
	var st syscall.Statfs_t
	if syscall.Statfs("/", &st) == nil {
		inv.DiskTotalMb = int(st.Blocks * uint64(st.Bsize) / (1 << 20))
		inv.DiskFreeMb = int(st.Bavail * uint64(st.Bsize) / (1 << 20))
	}
	inv.IP = localIP()
	return inv
}

// Enroll matricula este PC (rodar como root na instalação): grava o servidor e o token em /etc/vantor-pulse.
func Enroll(server, token string) error {
	server = strings.TrimRight(server, "/")
	if server == "" || token == "" {
		return fmt.Errorf("informe --server https://... e --token CODIGO_DA_EMPRESA (os dois aparecem no painel, em Computadores)")
	}
	id, devToken, err := NewClient(server, "").Enroll(token, CollectInventory())
	if err != nil {
		return err
	}
	return SaveDevice(&Device{Server: server, DeviceID: id, Token: devToken})
}
