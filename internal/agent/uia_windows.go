//go:build windows

package agent

import (
	"runtime"
	"syscall"
	"unsafe"

	ole "github.com/go-ole/go-ole"
)

// Lê a barra de endereço do navegador pela UI Automation (a mesma API dos leitores de tela)
// e devolve só o domínio. O conteúdo da página nunca é lido.

var browsers = map[string]bool{"chrome.exe": true, "msedge.exe": true, "firefox.exe": true, "brave.exe": true, "opera.exe": true, "vivaldi.exe": true, "arc.exe": true}

func IsBrowser(exe string) bool { return browsers[exe] }

var (
	clsidCUIAutomation = ole.NewGUID("{FF48DBA4-60EF-4201-AA87-54103EEF594E}")
	iidIUIAutomation   = ole.NewGUID("{30CBE57D-D9D0-452A-AB13-7AC5AC4825EE}")
)

const (
	uiaControlTypePropertyID = 30003
	uiaValueValuePropertyID  = 30045
	uiaEditControlTypeID     = 50004
	treeScopeDescendants     = 4
)

// UIA mantém o objeto COM numa thread fixa (exigência do COM).
type UIA struct {
	req chan uintptr
	res chan string
}

func NewUIA() *UIA {
	u := &UIA{req: make(chan uintptr), res: make(chan string)}
	go u.loop()
	return u
}

// Domain devolve o domínio da aba ativa da janela do navegador, ou "" se não conseguir.
func (u *UIA) Domain(hwnd uintptr) string {
	u.req <- hwnd
	return <-u.res
}

func (u *UIA) loop() {
	runtime.LockOSThread()
	if err := ole.CoInitializeEx(0, ole.COINIT_MULTITHREADED); err != nil {
		for range u.req {
			u.res <- ""
		}
	}
	unk, err := ole.CreateInstance(clsidCUIAutomation, iidIUIAutomation)
	for hwnd := range u.req {
		if err != nil {
			u.res <- ""
			continue
		}
		u.res <- DomainOf(addressBar(unk, hwnd))
	}
}

func vcall(obj *ole.IUnknown, idx int, args ...uintptr) uintptr {
	vtbl := *(*[64]uintptr)(unsafe.Pointer(obj.RawVTable))
	r, _, _ := syscall.SyscallN(vtbl[idx], append([]uintptr{uintptr(unsafe.Pointer(obj))}, args...)...)
	return r
}

func addressBar(auto *ole.IUnknown, hwnd uintptr) (value string) {
	defer func() {
		if recover() != nil {
			value = ""
		}
	}()
	var root *ole.IUnknown
	if vcall(auto, 6 /*ElementFromHandle*/, hwnd, uintptr(unsafe.Pointer(&root))) != 0 || root == nil {
		return ""
	}
	defer root.Release()

	v := ole.NewVariant(ole.VT_I4, uiaEditControlTypeID)
	var cond *ole.IUnknown
	if vcall(auto, 23 /*CreatePropertyCondition*/, uiaControlTypePropertyID, uintptr(unsafe.Pointer(&v)), uintptr(unsafe.Pointer(&cond))) != 0 || cond == nil {
		return ""
	}
	defer cond.Release()

	// A primeira caixa de texto da janela do navegador é a barra de endereço.
	var edit *ole.IUnknown
	if vcall(root, 5 /*FindFirst*/, treeScopeDescendants, uintptr(unsafe.Pointer(cond)), uintptr(unsafe.Pointer(&edit))) != 0 || edit == nil {
		return ""
	}
	defer edit.Release()

	var out ole.VARIANT
	ole.VariantInit(&out)
	if vcall(edit, 10 /*GetCurrentPropertyValue*/, uiaValueValuePropertyID, uintptr(unsafe.Pointer(&out))) != 0 {
		return ""
	}
	defer ole.VariantClear(&out)
	if out.VT != ole.VT_BSTR {
		return ""
	}
	return out.ToString()
}
