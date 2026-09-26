package service

import (
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

// appExe is the app installed next to the service.
func appExe() string {
	self, err := os.Executable()
	if err != nil {
		return ""
	}
	return filepath.Join(filepath.Dir(self), "coreshift.exe")
}

// launchInstaller starts the installer silently and does not wait: it stops
// and replaces this service. It must not be a child the service's job could
// take down with it, hence its own group, detached and out of any job.
func launchInstaller(path, logPath string) error {
	args := []string{"/VERYSILENT", "/SUPPRESSMSGBOXES", "/NORESTART", "/SP-", "/LOG=" + logPath}
	flags := uint32(windows.CREATE_NEW_PROCESS_GROUP | windows.DETACHED_PROCESS)
	var err error
	for _, extra := range []uint32{windows.CREATE_BREAKAWAY_FROM_JOB, 0} {
		cmd := exec.Command(path, args...)
		cmd.Dir = filepath.Dir(path)
		cmd.SysProcAttr = &syscall.SysProcAttr{CreationFlags: flags | extra, HideWindow: true}
		// Not in a job that allows breaking away: start it plainly.
		if err = cmd.Start(); err == nil {
			return cmd.Process.Release()
		}
	}
	return err
}

// appSessions returns the sessions where the installed app is running.
func appSessions() []uint32 {
	want := appExe()
	snap, err := windows.CreateToolhelp32Snapshot(windows.TH32CS_SNAPPROCESS, 0)
	if err != nil || want == "" {
		return nil
	}
	defer windows.CloseHandle(snap)
	var sessions []uint32
	e := windows.ProcessEntry32{Size: uint32(unsafe.Sizeof(windows.ProcessEntry32{}))}
	for err := windows.Process32First(snap, &e); err == nil; err = windows.Process32Next(snap, &e) {
		if !strings.EqualFold(windows.UTF16ToString(e.ExeFile[:]), "coreshift.exe") {
			continue
		}
		if path, ok := processPath(e.ProcessID); !ok || !strings.EqualFold(path, want) {
			continue
		}
		var id uint32
		if windows.ProcessIdToSessionId(e.ProcessID, &id) == nil && !slices.Contains(sessions, id) {
			sessions = append(sessions, id)
		}
	}
	return sessions
}

func processPath(pid uint32) (string, bool) {
	h, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, pid)
	if err != nil {
		return "", false
	}
	defer windows.CloseHandle(h)
	buf := make([]uint16, windows.MAX_LONG_PATH)
	n := uint32(len(buf))
	if windows.QueryFullProcessImageName(h, 0, &buf[0], &n) != nil {
		return "", false
	}
	return windows.UTF16ToString(buf[:n]), true
}

// startApp starts the app, hidden in the tray, as the user of each session.
// Only a service running as SYSTEM may do this.
func startApp(sessions []uint32) error {
	exe := appExe()
	var errs []error
	for _, id := range sessions {
		if err := startAppIn(id, exe); err != nil {
			errs = append(errs, err)
		}
	}
	if len(errs) > 0 {
		return errs[0]
	}
	return nil
}

func startAppIn(session uint32, exe string) error {
	var tok windows.Token
	if err := windows.WTSQueryUserToken(session, &tok); err != nil {
		return err // signed out meanwhile
	}
	defer tok.Close()
	var env *uint16
	if err := windows.CreateEnvironmentBlock(&env, tok, false); err != nil {
		return err
	}
	defer windows.DestroyEnvironmentBlock(env)
	cmdline, err := windows.UTF16PtrFromString(`"` + exe + `" --tray`)
	if err != nil {
		return err
	}
	dir, _ := windows.UTF16PtrFromString(filepath.Dir(exe))
	desktop, _ := windows.UTF16PtrFromString(`winsta0\default`)
	si := windows.StartupInfo{Cb: uint32(unsafe.Sizeof(windows.StartupInfo{})), Desktop: desktop}
	var pi windows.ProcessInformation
	if err := windows.CreateProcessAsUser(tok, nil, cmdline, nil, nil, false,
		windows.CREATE_UNICODE_ENVIRONMENT|windows.CREATE_NEW_PROCESS_GROUP, env, dir, &si, &pi); err != nil {
		return err
	}
	windows.CloseHandle(pi.Thread)
	windows.CloseHandle(pi.Process)
	return nil
}
