//go:build windows

// Package setup turns the single AetherMeter.exe into its own installer:
// per-user install (no admin), Start Menu / Desktop shortcuts, an entry in
// "Apps & features", Npcap download, and uninstall.
package setup

import (
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"syscall"
	"time"
	"unsafe"
)

const (
	AppName   = "AetherMeter"
	exeName   = "AetherMeter.exe"
	uninstKey = `Software\Microsoft\Windows\CurrentVersion\Uninstall\AetherMeter`
)

var (
	shell32  = syscall.NewLazyDLL("shell32.dll")
	ole32    = syscall.NewLazyDLL("ole32.dll")
	advapi32 = syscall.NewLazyDLL("advapi32.dll")
	user32   = syscall.NewLazyDLL("user32.dll")
	kernel32 = syscall.NewLazyDLL("kernel32.dll")

	pSHGetFolderPathW = shell32.NewProc("SHGetFolderPathW")
	pShellExecuteExW  = shell32.NewProc("ShellExecuteExW")
	pCoInitializeEx   = ole32.NewProc("CoInitializeEx")
	pCoCreateInstance = ole32.NewProc("CoCreateInstance")
	pRegCreateKeyExW  = advapi32.NewProc("RegCreateKeyExW")
	pRegSetValueExW   = advapi32.NewProc("RegSetValueExW")
	pRegCloseKey      = advapi32.NewProc("RegCloseKey")
	pRegDeleteTreeW   = advapi32.NewProc("RegDeleteTreeW")
	pMessageBoxW      = user32.NewProc("MessageBoxW")
	pWaitForSingleObj = kernel32.NewProc("WaitForSingleObject")
	pCloseHandle      = kernel32.NewProc("CloseHandle")
	pSHChangeNotify   = shell32.NewProc("SHChangeNotify")
)

// ---- message boxes -------------------------------------------------------------

const (
	MBOk          = 0x0
	MBYesNo       = 0x4
	MBYesNoCancel = 0x3
	MBIconInfo    = 0x40
	MBIconQuest   = 0x20
	MBIconWarn    = 0x30
	MBIconError   = 0x10
	IDYes         = 6
	IDNo          = 7
	IDCancel      = 2
)

func u16(s string) *uint16 { p, _ := syscall.UTF16PtrFromString(s); return p }

// Ask shows a message box and returns the button id.
func Ask(text string, flags uintptr) int {
	r, _, _ := pMessageBoxW.Call(0, uintptr(unsafe.Pointer(u16(text))), uintptr(unsafe.Pointer(u16(AppName))), flags|0x40000 /*topmost*/)
	return int(r)
}

// ---- paths ------------------------------------------------------------------------

const (
	csidlPrograms     = 0x02
	csidlDesktopDir   = 0x10
	csidlLocalAppData = 0x1c
)

func folder(csidl uintptr) string {
	buf := make([]uint16, 520)
	r, _, _ := pSHGetFolderPathW.Call(0, csidl, 0, 0, uintptr(unsafe.Pointer(&buf[0])))
	if r != 0 {
		return ""
	}
	return syscall.UTF16ToString(buf)
}

// InstallDir is %LOCALAPPDATA%\Programs\AetherMeter.
func InstallDir() string {
	base := folder(csidlLocalAppData)
	if base == "" {
		base = os.Getenv("LOCALAPPDATA")
	}
	return filepath.Join(base, "Programs", AppName)
}

func InstalledExe() string { return filepath.Join(InstallDir(), exeName) }

func startMenuDir() string { return filepath.Join(folder(csidlPrograms), AppName) }

func desktopLink() string { return filepath.Join(folder(csidlDesktopDir), AppName+".lnk") }

// CurrentExe returns the running executable's path.
func CurrentExe() string {
	p, err := os.Executable()
	if err != nil {
		return ""
	}
	if r, err := filepath.EvalSymlinks(p); err == nil {
		p = r
	}
	return p
}

// RunningInstalled reports whether this process is the installed copy.
func RunningInstalled() bool {
	return strings.EqualFold(filepath.Clean(CurrentExe()), filepath.Clean(InstalledExe()))
}

func IsInstalled() bool {
	_, err := os.Stat(InstalledExe())
	return err == nil
}

// ---- install / uninstall --------------------------------------------------------

// Install copies the running exe into InstallDir and creates shortcuts.
func Install(version string) error {
	src := CurrentExe()
	dst := InstalledExe()
	if err := os.MkdirAll(InstallDir(), 0o755); err != nil {
		return err
	}
	if err := copyFile(src, dst); err != nil {
		return fmt.Errorf("não consegui copiar o programa (ele está aberto?): %w", err)
	}

	coInit()
	var errs []string
	if err := os.MkdirAll(startMenuDir(), 0o755); err == nil {
		if err := createShortcut(filepath.Join(startMenuDir(), AppName+".lnk"), dst, "", "Medidor de DPS de PT para Aion 2"); err != nil {
			errs = append(errs, err.Error())
		}
		_ = createShortcut(filepath.Join(startMenuDir(), AppName+" (demonstração).lnk"), dst, "-demo", "Luta simulada para testar o overlay")
		_ = createShortcut(filepath.Join(startMenuDir(), "Desinstalar "+AppName+".lnk"), dst, "-uninstall", "Remove o AetherMeter")
	}
	if err := createShortcut(desktopLink(), dst, "", "Medidor de DPS de PT para Aion 2"); err != nil {
		errs = append(errs, err.Error())
	}

	writeUninstallEntry(version, dst)
	removeLegacy()
	pSHChangeNotify.Call(0x08000000 /*SHCNE_ASSOCCHANGED*/, 0, 0, 0)
	if len(errs) > 0 {
		return fmt.Errorf("instalado, mas alguns atalhos falharam: %s", strings.Join(errs, "; "))
	}
	return nil
}

// removeLegacy cleans up an install made under the old name (PartyMeter).
func removeLegacy() {
	const old = "PartyMeter"
	_ = os.Remove(filepath.Join(folder(csidlDesktopDir), old+".lnk"))
	_ = os.RemoveAll(filepath.Join(folder(csidlPrograms), old))
	k, _ := syscall.UTF16PtrFromString(`Software\Microsoft\Windows\CurrentVersion\Uninstall\` + old)
	pRegDeleteTreeW.Call(hkcu, uintptr(unsafe.Pointer(k)))
	base := folder(csidlLocalAppData)
	if base == "" {
		base = os.Getenv("LOCALAPPDATA")
	}
	oldDir := filepath.Join(base, "Programs", old)
	if _, err := os.Stat(oldDir); err == nil {
		_ = os.RemoveAll(oldDir) // fails harmlessly if the old version is still open
	}
}

// Uninstall removes shortcuts, the registry entry and (after exit) the install folder.
// If removeData is true the settings/log folder is deleted too.
func Uninstall(dataDir string, removeData bool) {
	_ = os.Remove(desktopLink())
	_ = os.RemoveAll(startMenuDir())
	k, _ := syscall.UTF16PtrFromString(uninstKey)
	pRegDeleteTreeW.Call(hkcu, uintptr(unsafe.Pointer(k)))

	dirs := []string{InstallDir()}
	if removeData && dataDir != "" {
		dirs = append(dirs, dataDir)
	}
	// the running exe can't delete itself: let a hidden cmd do it a moment after we exit
	var b strings.Builder
	b.WriteString(`cmd.exe /C ping 127.0.0.1 -n 3 > nul`)
	for _, d := range dirs {
		fmt.Fprintf(&b, ` & rmdir /s /q "%s"`, d)
	}
	cmd := exec.Command("cmd.exe")
	cmd.SysProcAttr = &syscall.SysProcAttr{CmdLine: b.String(), HideWindow: true, CreationFlags: 0x08000000}
	_ = cmd.Start()
	pSHChangeNotify.Call(0x08000000, 0, 0, 0)
}

// Launch starts the installed copy (detached) with optional args.
func Launch(exe string, args ...string) error {
	cmd := exec.Command(exe, args...)
	cmd.Dir = filepath.Dir(exe)
	return cmd.Start()
}

func copyFile(src, dst string) error {
	if strings.EqualFold(filepath.Clean(src), filepath.Clean(dst)) {
		return nil
	}
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	tmp := dst + ".new"
	out, err := os.Create(tmp)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		os.Remove(tmp)
		return err
	}
	if err := out.Close(); err != nil {
		return err
	}
	_ = os.Remove(dst + ".old")
	if _, err := os.Stat(dst); err == nil {
		// a running exe can be renamed but not overwritten
		if err := os.Rename(dst, dst+".old"); err != nil {
			os.Remove(tmp)
			return err
		}
	}
	return os.Rename(tmp, dst)
}

// ---- registry ("Apps & features") ---------------------------------------------------

const (
	hkcu     = 0x80000001
	keyWrite = 0x20006
	regSz    = 1
	regDword = 4
)

func writeUninstallEntry(version, exe string) {
	var h uintptr
	k := u16(uninstKey)
	if r, _, _ := pRegCreateKeyExW.Call(hkcu, uintptr(unsafe.Pointer(k)), 0, 0, 0, keyWrite, 0, uintptr(unsafe.Pointer(&h)), 0); r != 0 {
		return
	}
	defer pRegCloseKey.Call(h)
	setStr := func(name, val string) {
		u, _ := syscall.UTF16FromString(val)
		pRegSetValueExW.Call(h, uintptr(unsafe.Pointer(u16(name))), 0, regSz, uintptr(unsafe.Pointer(&u[0])), uintptr(len(u)*2))
	}
	setDword := func(name string, v uint32) {
		pRegSetValueExW.Call(h, uintptr(unsafe.Pointer(u16(name))), 0, regDword, uintptr(unsafe.Pointer(&v)), 4)
	}
	setStr("DisplayName", AppName+" — DPS meter Aion 2")
	setStr("DisplayVersion", version)
	setStr("Publisher", "AetherMeter (comunidade)")
	setStr("DisplayIcon", exe+",0")
	setStr("InstallLocation", filepath.Dir(exe))
	setStr("UninstallString", `"`+exe+`" -uninstall`)
	setStr("InstallDate", time.Now().Format("20060102"))
	setDword("NoModify", 1)
	setDword("NoRepair", 1)
	if fi, err := os.Stat(exe); err == nil {
		setDword("EstimatedSize", uint32(fi.Size()/1024))
	}
}

// ---- shortcuts (IShellLinkW via COM) -------------------------------------------------

type guid struct {
	d1     uint32
	d2, d3 uint16
	d4     [8]byte
}

var (
	clsidShellLink = guid{0x00021401, 0, 0, [8]byte{0xC0, 0, 0, 0, 0, 0, 0, 0x46}}
	iidShellLinkW  = guid{0x000214F9, 0, 0, [8]byte{0xC0, 0, 0, 0, 0, 0, 0, 0x46}}
	iidPersistFile = guid{0x0000010B, 0, 0, [8]byte{0xC0, 0, 0, 0, 0, 0, 0, 0x46}}
	coInitDone     bool
)

func coInit() {
	if !coInitDone {
		pCoInitializeEx.Call(0, 0x2 /*COINIT_APARTMENTTHREADED*/)
		coInitDone = true
	}
}

// comCall invokes method #idx of the COM object obj.
func comCall(obj uintptr, idx int, args ...uintptr) uintptr {
	vtbl := *(*unsafe.Pointer)(unsafe.Add(nil, obj))
	fn := *(*uintptr)(unsafe.Add(vtbl, uintptr(idx)*unsafe.Sizeof(uintptr(0))))
	r, _, _ := syscall.SyscallN(fn, append([]uintptr{obj}, args...)...)
	return r
}

func createShortcut(lnk, target, args, desc string) error {
	var sl uintptr
	if r, _, _ := pCoCreateInstance.Call(uintptr(unsafe.Pointer(&clsidShellLink)), 0, 1 /*CLSCTX_INPROC_SERVER*/, uintptr(unsafe.Pointer(&iidShellLinkW)), uintptr(unsafe.Pointer(&sl))); r != 0 || sl == 0 {
		return fmt.Errorf("CoCreateInstance(ShellLink) 0x%X", r)
	}
	defer comCall(sl, 2)                                               // Release
	comCall(sl, 20, uintptr(unsafe.Pointer(u16(target))))              // SetPath
	comCall(sl, 11, uintptr(unsafe.Pointer(u16(args))))                // SetArguments
	comCall(sl, 7, uintptr(unsafe.Pointer(u16(desc))))                 // SetDescription
	comCall(sl, 9, uintptr(unsafe.Pointer(u16(filepath.Dir(target))))) // SetWorkingDirectory
	comCall(sl, 17, uintptr(unsafe.Pointer(u16(target))), 0)           // SetIconLocation

	var pf uintptr
	if r := comCall(sl, 0, uintptr(unsafe.Pointer(&iidPersistFile)), uintptr(unsafe.Pointer(&pf))); r != 0 || pf == 0 {
		return fmt.Errorf("QueryInterface(IPersistFile) 0x%X", r)
	}
	defer comCall(pf, 2)
	if r := comCall(pf, 6, uintptr(unsafe.Pointer(u16(lnk))), 1); r != 0 { // Save
		return fmt.Errorf("salvar atalho %s: 0x%X", lnk, r)
	}
	return nil
}

// ---- Npcap ------------------------------------------------------------------------------

// NpcapInstalled checks for Npcap's wpcap.dll.
func NpcapInstalled() bool {
	sys := os.Getenv("SystemRoot")
	if sys == "" {
		sys = `C:\Windows`
	}
	for _, p := range []string{
		filepath.Join(sys, "System32", "Npcap", "wpcap.dll"),
		filepath.Join(sys, "System32", "wpcap.dll"),
	} {
		if _, err := os.Stat(p); err == nil {
			return true
		}
	}
	return false
}

const npcapFallback = "https://npcap.com/dist/npcap-1.89.exe"

var npcapRe = regexp.MustCompile(`https://npcap\.com/dist/npcap-([0-9]+(?:\.[0-9]+)*)\.exe`)

func latestNpcapURL(client *http.Client) string {
	resp, err := client.Get("https://npcap.com/")
	if err != nil {
		return npcapFallback
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 2<<20))
	ms := npcapRe.FindAllStringSubmatch(string(body), -1)
	if len(ms) == 0 {
		return npcapFallback
	}
	sort.Slice(ms, func(i, j int) bool { return versionLess(ms[j][1], ms[i][1]) })
	return ms[0][0]
}

func versionLess(a, b string) bool {
	pa, pb := strings.Split(a, "."), strings.Split(b, ".")
	for i := 0; i < len(pa) || i < len(pb); i++ {
		var x, y int
		if i < len(pa) {
			x, _ = strconv.Atoi(pa[i])
		}
		if i < len(pb) {
			y, _ = strconv.Atoi(pb[i])
		}
		if x != y {
			return x < y
		}
	}
	return false
}

type shellExecuteInfo struct {
	cbSize       uint32
	fMask        uint32
	hwnd         uintptr
	lpVerb       *uint16
	lpFile       *uint16
	lpParameters *uint16
	lpDirectory  *uint16
	nShow        int32
	hInstApp     uintptr
	lpIDList     uintptr
	lpClass      *uint16
	hkeyClass    uintptr
	dwHotKey     uint32
	hIconMonitor uintptr
	hProcess     uintptr
}

// InstallNpcap downloads the official Npcap installer and runs it (the user
// clicks through it; it asks for admin rights itself). Blocks until it closes.
func InstallNpcap() error {
	client := &http.Client{Timeout: 60 * time.Second}
	url := latestNpcapURL(client)
	resp, err := client.Get(url)
	if err != nil {
		return fmt.Errorf("download do Npcap falhou: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return fmt.Errorf("download do Npcap falhou: HTTP %d", resp.StatusCode)
	}
	path := filepath.Join(os.TempDir(), filepath.Base(url))
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	if _, err := io.Copy(f, io.LimitReader(resp.Body, 50<<20)); err != nil {
		f.Close()
		return err
	}
	f.Close()

	sei := shellExecuteInfo{
		fMask:  0x40, // SEE_MASK_NOCLOSEPROCESS
		lpVerb: u16("open"),
		lpFile: u16(path),
		nShow:  1,
	}
	sei.cbSize = uint32(unsafe.Sizeof(sei))
	if r, _, err := pShellExecuteExW.Call(uintptr(unsafe.Pointer(&sei))); r == 0 {
		return fmt.Errorf("não consegui abrir o instalador do Npcap: %v", err)
	}
	if sei.hProcess != 0 {
		pWaitForSingleObj.Call(sei.hProcess, 0xFFFFFFFF)
		pCloseHandle.Call(sei.hProcess)
	}
	_ = os.Remove(path)
	if !NpcapInstalled() {
		return errors.New("o Npcap não foi instalado")
	}
	return nil
}

// InstanceRunning reports whether a AetherMeter overlay is already open
// (without claiming the single-instance mutex ourselves).
func InstanceRunning() bool {
	p := kernel32.NewProc("OpenMutexW")
	h, _, _ := p.Call(0x00100000 /*SYNCHRONIZE*/, 0, uintptr(unsafe.Pointer(u16(`Local\AetherMeterSingleInstance`))))
	if h != 0 {
		pCloseHandle.Call(h)
		return true
	}
	return false
}
