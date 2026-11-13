//go:build windows

package agent

import (
	"crypto/rand"
	"fmt"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"strings"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/registry"
)

var (
	user32                       = windows.NewLazySystemDLL("user32.dll")
	kernel32                     = windows.NewLazySystemDLL("kernel32.dll")
	procGetForegroundWindow      = user32.NewProc("GetForegroundWindow")
	procGetWindowTextW           = user32.NewProc("GetWindowTextW")
	procGetWindowThreadProcessId = user32.NewProc("GetWindowThreadProcessId")
	procGetLastInputInfo         = user32.NewProc("GetLastInputInfo")
	procGetWindowRect            = user32.NewProc("GetWindowRect")
	procMonitorFromWindow        = user32.NewProc("MonitorFromWindow")
	procGetMonitorInfoW          = user32.NewProc("GetMonitorInfoW")
	procMessageBoxW              = user32.NewProc("MessageBoxW")
	procGlobalMemoryStatusEx     = kernel32.NewProc("GlobalMemoryStatusEx")
	procGetTickCount             = kernel32.NewProc("GetTickCount")
)

type rect struct{ Left, Top, Right, Bottom int32 }

// Pastas: ProgramData (do PC, escrita pelo serviço) e LocalAppData (de cada usuário, escrita pelo coletor).
func MachineDir() string { return filepath.Join(os.Getenv("ProgramData"), "VantorPulse") }
func UserDir() string    { return filepath.Join(os.Getenv("LOCALAPPDATA"), "VantorPulse") }

// IsLocked: tela de bloqueio ou de login do Windows em foco.
func IsLocked(exe string) bool { return exe == "lockapp.exe" || exe == "logonui.exe" }

// PlatformProblem: no Windows não há impedimento conhecido para a coleta.
func PlatformProblem() string { return "" }

// OpenURL abre um endereço no navegador padrão.
func OpenURL(url string) { _ = exec.Command("rundll32", "url.dll,FileProtocolHandler", url).Start() }

// Foreground devolve o executável (minúsculo), o título e o handle da janela em foco.
func Foreground() (exe, title string, hwnd uintptr) {
	hwnd, _, _ = procGetForegroundWindow.Call()
	if hwnd == 0 {
		return "", "", 0
	}
	buf := make([]uint16, 512)
	n, _, _ := procGetWindowTextW.Call(hwnd, uintptr(unsafe.Pointer(&buf[0])), uintptr(len(buf)))
	title = syscall.UTF16ToString(buf[:n])
	var pid uint32
	procGetWindowThreadProcessId.Call(hwnd, uintptr(unsafe.Pointer(&pid)))
	h, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, pid)
	if err != nil {
		return "", title, hwnd
	}
	defer windows.CloseHandle(h)
	size := uint32(len(buf))
	if err := windows.QueryFullProcessImageName(h, 0, &buf[0], &size); err != nil {
		return "", title, hwnd
	}
	return strings.ToLower(filepath.Base(syscall.UTF16ToString(buf[:size]))), title, hwnd
}

// IsFullscreen: a janela cobre o monitor inteiro (vídeo ou reunião em tela cheia).
func IsFullscreen(hwnd uintptr) bool {
	var r rect
	if ok, _, _ := procGetWindowRect.Call(hwnd, uintptr(unsafe.Pointer(&r))); ok == 0 {
		return false
	}
	mon, _, _ := procMonitorFromWindow.Call(hwnd, 2 /* MONITOR_DEFAULTTONEAREST */)
	mi := struct {
		Size          uint32
		Monitor, Work rect
		Flags         uint32
	}{}
	mi.Size = uint32(unsafe.Sizeof(mi))
	if ok, _, _ := procGetMonitorInfoW.Call(mon, uintptr(unsafe.Pointer(&mi))); ok == 0 {
		return false
	}
	return r.Left <= mi.Monitor.Left && r.Top <= mi.Monitor.Top && r.Right >= mi.Monitor.Right && r.Bottom >= mi.Monitor.Bottom
}

// LastInputTick é o instante (em ms desde o boot) do último toque no teclado ou mouse.
// Só o instante: o agente não instala gancho de teclado e nunca sabe o que foi digitado.
func LastInputTick() uint32 {
	li := struct{ Size, Time uint32 }{Size: 8}
	procGetLastInputInfo.Call(uintptr(unsafe.Pointer(&li)))
	return li.Time
}

// IdleSeconds: segundos desde o último toque no teclado ou mouse.
func IdleSeconds() int {
	now, _, _ := procGetTickCount.Call()
	return int((uint32(now) - LastInputTick()) / 1000)
}

// Ask mostra uma caixa Sim/Não do Windows (usada no Termo de Ciência).
func Ask(title, text string) bool {
	t, _ := windows.UTF16PtrFromString(title)
	m, _ := windows.UTF16PtrFromString(text)
	const flags = 0x4 /*YESNO*/ | 0x40 /*ICONINFORMATION*/ | 0x10000 /*SETFOREGROUND*/ | 0x40000 /*TOPMOST*/
	r, _, _ := procMessageBoxW.Call(0, uintptr(unsafe.Pointer(m)), uintptr(unsafe.Pointer(t)), flags)
	return r == 6 // IDYES
}

// Notify mostra uma mensagem simples.
func Notify(title, text string) {
	t, _ := windows.UTF16PtrFromString(title)
	m, _ := windows.UTF16PtrFromString(text)
	procMessageBoxW.Call(0, uintptr(unsafe.Pointer(m)), uintptr(unsafe.Pointer(t)), 0x40|0x10000)
}

// WinUser é o login do Windows sem o domínio, em minúsculas.
func WinUser() string {
	u, err := user.Current()
	if err != nil {
		return os.Getenv("USERNAME")
	}
	name := u.Username
	if i := strings.LastIndex(name, `\`); i >= 0 {
		name = name[i+1:]
	}
	return strings.ToLower(name)
}

// Protect/Unprotect usam o DPAPI no escopo da máquina: só este computador abre o dado.
func Protect(data []byte) ([]byte, error) {
	in := windows.DataBlob{Size: uint32(len(data)), Data: &data[0]}
	var out windows.DataBlob
	if err := windows.CryptProtectData(&in, nil, nil, 0, nil, windows.CRYPTPROTECT_LOCAL_MACHINE|windows.CRYPTPROTECT_UI_FORBIDDEN, &out); err != nil {
		return nil, err
	}
	defer windows.LocalFree(windows.Handle(unsafe.Pointer(out.Data)))
	return append([]byte(nil), unsafe.Slice(out.Data, out.Size)...), nil
}

func Unprotect(data []byte) ([]byte, error) {
	in := windows.DataBlob{Size: uint32(len(data)), Data: &data[0]}
	var out windows.DataBlob
	if err := windows.CryptUnprotectData(&in, nil, nil, 0, nil, windows.CRYPTPROTECT_UI_FORBIDDEN, &out); err != nil {
		return nil, err
	}
	defer windows.LocalFree(windows.Handle(unsafe.Pointer(out.Data)))
	return append([]byte(nil), unsafe.Slice(out.Data, out.Size)...), nil
}

// QueueKey carrega (ou cria) a chave da fila deste usuário, guardada protegida pelo DPAPI.
func QueueKey(dir string) ([]byte, error) {
	path := filepath.Join(dir, "fila.key")
	if enc, err := os.ReadFile(path); err == nil {
		if k, err := Unprotect(enc); err == nil && len(k) == 32 {
			return k, nil
		}
	}
	k := make([]byte, 32)
	if _, err := rand.Read(k); err != nil {
		return nil, err
	}
	enc, err := Protect(k)
	if err != nil {
		return nil, err
	}
	return k, os.WriteFile(path, enc, 0o600)
}

// CollectInventory lê os dados técnicos do PC (sem nada pessoal).
func CollectInventory() Inventory {
	inv := Inventory{OS: "Windows"}
	inv.Hostname, _ = os.Hostname()
	if k, err := registry.OpenKey(registry.LOCAL_MACHINE, `SOFTWARE\Microsoft\Windows NT\CurrentVersion`, registry.QUERY_VALUE); err == nil {
		name, _, _ := k.GetStringValue("ProductName")
		disp, _, _ := k.GetStringValue("DisplayVersion")
		build, _, _ := k.GetStringValue("CurrentBuildNumber")
		k.Close()
		if b := atoi(build); b >= 22000 {
			name = strings.Replace(name, "Windows 10", "Windows 11", 1) // o registro do Windows 11 ainda diz "Windows 10"
		}
		inv.OS = name
		inv.OSVersion = strings.TrimSpace(fmt.Sprintf("%s (build %s)", disp, build))
	}
	if k, err := registry.OpenKey(registry.LOCAL_MACHINE, `HARDWARE\DESCRIPTION\System\CentralProcessor\0`, registry.QUERY_VALUE); err == nil {
		inv.CPU, _, _ = k.GetStringValue("ProcessorNameString")
		inv.CPU = strings.TrimSpace(inv.CPU)
		k.Close()
	}
	ms := struct {
		Length, MemoryLoad                                                                                   uint32
		TotalPhys, AvailPhys, TotalPageFile, AvailPageFile, TotalVirtual, AvailVirtual, AvailExtendedVirtual uint64
	}{}
	ms.Length = uint32(unsafe.Sizeof(ms))
	if ok, _, _ := procGlobalMemoryStatusEx.Call(uintptr(unsafe.Pointer(&ms))); ok != 0 {
		inv.RAMMb = int(ms.TotalPhys / (1 << 20))
	}
	var free, total, totalFree uint64
	if root, err := windows.UTF16PtrFromString(os.Getenv("SystemDrive") + `\`); err == nil && windows.GetDiskFreeSpaceEx(root, &free, &total, &totalFree) == nil {
		inv.DiskTotalMb = int(total / (1 << 20))
		inv.DiskFreeMb = int(free / (1 << 20))
	}
	inv.IP = localIP()
	return inv
}
