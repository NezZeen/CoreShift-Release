package sysproxy

import (
	"context"
	"fmt"
	"net/netip"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"unsafe"

	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/registry"
)

// WinINet is the proxy of Windows for the current user, as "Параметры →
// Сеть и Интернет → Прокси" shows it: what Edge, Chrome, the Store apps
// and most programs use. It is read and written through WinINet's
// per-connection options rather than the registry values alone: they
// keep the binary DefaultConnectionSettings, which Windows prefers, in
// step with ProxyEnable and ProxyServer, and they cover the setup script
// and the automatic detection, which would otherwise win over a manual
// proxy. Programs that run are told (INTERNET_OPTION_SETTINGS_CHANGED and
// REFRESH); Chrome and Edge also watch the registry.
type WinINet struct{}

var (
	wininet              = windows.NewLazySystemDLL("wininet.dll")
	procInternetSetOpt   = wininet.NewProc("InternetSetOptionW")
	procInternetQueryOpt = wininet.NewProc("InternetQueryOptionW")
	kernel32             = windows.NewLazySystemDLL("kernel32.dll")
	procGlobalFree       = kernel32.NewProc("GlobalFree")
)

const (
	optRefresh          = 37
	optSettingsChanged  = 39
	optPerConnection    = 75
	optProxySettingsChg = 95

	perConnFlags         = 1
	perConnProxyServer   = 2
	perConnProxyBypass   = 3
	perConnAutoConfigURL = 4
	perConnFlagsUI       = 10

	proxyTypeDirect = 1
	proxyTypeProxy  = 2
)

// perConnOption is INTERNET_PER_CONN_OPTIONW: a DWORD and a union of a
// DWORD, a string and a FILETIME, 8 bytes aligned as a pointer is. Value[0]
// is the union's DWORD or string; on 32-bit Windows Value[1] is the rest
// of the FILETIME.
type perConnOption struct {
	Option uint32
	Value  [8 / unsafe.Sizeof(uintptr(0))]uintptr
}

// perConnList is INTERNET_PER_CONN_OPTION_LISTW for the LAN settings
// (no connection name).
type perConnList struct {
	Size       uint32
	Connection *uint16
	Count      uint32
	Error      uint32
	Options    *perConnOption
}

func (WinINet) Name() string { return "windows" }

func (w WinINet) Read(context.Context) (Settings, error) {
	s, err := w.query(perConnFlagsUI)
	if err != nil {
		// Before Windows 7 there are no UI flags.
		s, err = w.query(perConnFlags)
	}
	return s, err
}

func (WinINet) query(flagsOption uint32) (Settings, error) {
	opts := []perConnOption{{Option: flagsOption}, {Option: perConnProxyServer}, {Option: perConnProxyBypass}, {Option: perConnAutoConfigURL}}
	list := perConnList{Count: uint32(len(opts)), Options: &opts[0]}
	list.Size = uint32(unsafe.Sizeof(list))
	size := list.Size
	r, _, err := procInternetQueryOpt.Call(0, optPerConnection, uintptr(unsafe.Pointer(&list)), uintptr(unsafe.Pointer(&size)))
	if r == 0 {
		return nil, fmt.Errorf("InternetQueryOption: %w", err)
	}
	str := func(o *perConnOption) string {
		if o.Value[0] == 0 {
			return ""
		}
		// The union holds a string WinINet allocated, freed here.
		v := windows.UTF16PtrToString(*(**uint16)(unsafe.Pointer(&o.Value[0])))
		procGlobalFree.Call(o.Value[0])
		return v
	}
	return Settings{
		"flags":      strconv.FormatUint(uint64(uint32(opts[0].Value[0])), 10),
		"server":     str(&opts[1]),
		"bypass":     str(&opts[2]),
		"autoconfig": str(&opts[3]),
	}, nil
}

func (w WinINet) Write(_ context.Context, s Settings) error {
	flags, err := strconv.ParseUint(s["flags"], 10, 32)
	if err != nil {
		return fmt.Errorf("flags %q: %w", s["flags"], err)
	}
	ptr := func(v string) *uint16 {
		if v == "" {
			return nil
		}
		p, _ := windows.UTF16PtrFromString(v)
		return p
	}
	server, bypass, auto := ptr(s["server"]), ptr(s["bypass"]), ptr(s["autoconfig"])
	set := func(flagsOption uint32) error {
		opts := []perConnOption{{Option: flagsOption}, {Option: perConnProxyServer}, {Option: perConnProxyBypass}, {Option: perConnAutoConfigURL}}
		// The unions point at the strings, which the KeepAlive calls
		// below hold until WinINet has copied them.
		opts[0].Value[0] = uintptr(flags)
		*(**uint16)(unsafe.Pointer(&opts[1].Value[0])) = server
		*(**uint16)(unsafe.Pointer(&opts[2].Value[0])) = bypass
		*(**uint16)(unsafe.Pointer(&opts[3].Value[0])) = auto
		list := perConnList{Count: uint32(len(opts)), Options: &opts[0]}
		list.Size = uint32(unsafe.Sizeof(list))
		r, _, err := procInternetSetOpt.Call(0, optPerConnection, uintptr(unsafe.Pointer(&list)), uintptr(list.Size))
		runtime.KeepAlive(opts)
		if r == 0 {
			return fmt.Errorf("InternetSetOption: %w", err)
		}
		return nil
	}
	if err := set(perConnFlagsUI); err != nil {
		if err := set(perConnFlags); err != nil {
			return err
		}
	}
	runtime.KeepAlive(server)
	runtime.KeepAlive(bypass)
	runtime.KeepAlive(auto)
	for _, o := range []uintptr{optProxySettingsChg, optSettingsChanged, optRefresh} {
		procInternetSetOpt.Call(0, o, 0, 0)
	}
	return nil
}

// winBypass is lanHosts as WinINet takes them: wildcards, and <local>
// for names without a dot.
func winBypass() string {
	list := []string{"localhost", "127.*", "10.*"}
	for i := 16; i <= 31; i++ {
		list = append(list, "172."+strconv.Itoa(i)+".*")
	}
	list = append(list, "192.168.*", "169.254.*", "[::1]", "*.local", "<local>")
	return strings.Join(list, ";")
}

func (WinINet) Proxy(addr netip.AddrPort) Settings {
	// One server for every protocol: the port's HTTP proxy, which takes
	// CONNECT for HTTPS. WinINet's "socks=" is SOCKS4, which the port does
	// not speak.
	return Settings{
		"flags":      strconv.Itoa(proxyTypeDirect | proxyTypeProxy),
		"server":     addr.String(),
		"bypass":     winBypass(),
		"autoconfig": "",
	}
}

func (WinINet) Direct() Settings {
	return Settings{"flags": strconv.Itoa(proxyTypeDirect), "server": "", "bypass": "", "autoconfig": ""}
}

// Owns: our server with the proxy on. Windows may add automatic
// detection to the flags by itself; that leaves the proxy ours.
func (WinINet) Owns(cur, ours Settings) bool {
	f, err := strconv.ParseUint(cur["flags"], 10, 32)
	return err == nil && f&proxyTypeProxy != 0 && cur["server"] == ours["server"]
}

// RunOnce is the sign-in entry of Windows: HKCU's RunOnce runs the
// command once at the next sign-in, and removes it.
type RunOnce struct {
	// Command is what runs: `"…\coreshiftd.exe" sysproxy logon`.
	Command string
}

const (
	runOnceKey   = `Software\Microsoft\Windows\CurrentVersion\RunOnce`
	runOnceValue = "CoreShiftProxyRestore"
)

func (r RunOnce) Set() error {
	k, _, err := registry.CreateKey(registry.CURRENT_USER, runOnceKey, registry.SET_VALUE)
	if err != nil {
		return err
	}
	defer k.Close()
	return k.SetStringValue(runOnceValue, r.Command)
}

func (RunOnce) Clear() error {
	k, err := registry.OpenKey(registry.CURRENT_USER, runOnceKey, registry.SET_VALUE)
	if err == registry.ErrNotExist {
		return nil
	}
	if err != nil {
		return err
	}
	defer k.Close()
	if err := k.DeleteValue(runOnceValue); err != nil && err != registry.ErrNotExist {
		return err
	}
	return nil
}

// Default is the manager for the user running this: Windows' proxy, the
// journal in %LOCALAPPDATA%\CoreShift, the RunOnce entry.
func Default(context.Context) (*Manager, error) {
	dir, err := os.UserCacheDir()
	if err != nil {
		return nil, err
	}
	m := &Manager{Journal: filepath.Join(dir, "CoreShift", "sysproxy.json"), Backends: []Backend{WinINet{}}}
	if self, err := os.Executable(); err == nil {
		m.Autostart = RunOnce{Command: `"` + self + `" sysproxy logon`}
	}
	return m, nil
}
