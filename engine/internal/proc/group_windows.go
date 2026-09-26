package proc

import (
	"os"
	"os/exec"
	"sync"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

// processGroup is a job object that kills every core when the daemon's
// handle to it closes, including when the daemon crashes or is killed, so no
// orphaned core keeps the SOCKS port busy.
type processGroup struct {
	job windows.Handle
}

func newProcessGroup() (*processGroup, error) {
	job, err := windows.CreateJobObject(nil, nil)
	if err != nil {
		return nil, err
	}
	info := windows.JOBOBJECT_EXTENDED_LIMIT_INFORMATION{
		BasicLimitInformation: windows.JOBOBJECT_BASIC_LIMIT_INFORMATION{
			LimitFlags: windows.JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE,
		},
	}
	if _, err := windows.SetInformationJobObject(job, windows.JobObjectExtendedLimitInformation,
		uintptr(unsafe.Pointer(&info)), uint32(unsafe.Sizeof(info))); err != nil {
		windows.CloseHandle(job)
		return nil, err
	}
	return &processGroup{job: job}, nil
}

func (g *processGroup) add(p *os.Process) error {
	h, err := windows.OpenProcess(windows.PROCESS_SET_QUOTA|windows.PROCESS_TERMINATE, false, uint32(p.Pid))
	if err != nil {
		return err
	}
	defer windows.CloseHandle(h)
	return windows.AssignProcessToJobObject(g.job, h)
}

// prepareCmd keeps processes from opening console windows when the daemon
// runs as a service or from the GUI.
//
// A graceful process instead shares the daemon's console in a process group
// of its own: that is the only way to deliver it CTRL_BREAK, which Go
// programs such as sing-box handle like Ctrl+C and shut down cleanly on.
func prepareCmd(cmd *exec.Cmd, graceful bool) {
	if !graceful {
		cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: windows.CREATE_NO_WINDOW}
		return
	}
	ensureConsole()
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: windows.CREATE_NEW_PROCESS_GROUP}
}

// interrupt sends CTRL_BREAK to the process group of p, which prepareCmd
// made p its own leader of; the daemon itself is not in it.
func interrupt(p *os.Process) error {
	return windows.GenerateConsoleCtrlEvent(windows.CTRL_BREAK_EVENT, uint32(p.Pid))
}

var (
	kernel32             = windows.NewLazySystemDLL("kernel32.dll")
	user32               = windows.NewLazySystemDLL("user32.dll")
	procAllocConsole     = kernel32.NewProc("AllocConsole")
	procGetConsoleWindow = kernel32.NewProc("GetConsoleWindow")
	procShowWindow       = user32.NewProc("ShowWindow")
	consoleOnce          sync.Once
)

// ensureConsole gives the daemon a console to share with graceful children
// when it has none (as a service), and keeps it out of sight.
func ensureConsole() {
	consoleOnce.Do(func() {
		if hwnd, _, _ := procGetConsoleWindow.Call(); hwnd != 0 {
			return
		}
		if r, _, _ := procAllocConsole.Call(); r == 0 {
			return
		}
		if hwnd, _, _ := procGetConsoleWindow.Call(); hwnd != 0 {
			procShowWindow.Call(hwnd, windows.SW_HIDE)
		}
	})
}
