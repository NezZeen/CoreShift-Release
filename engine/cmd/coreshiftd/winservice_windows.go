package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"coreshift/engine/internal/service"

	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/svc"
	"golang.org/x/sys/windows/svc/mgr"
)

const serviceName = "CoreShift"

func isElevated() bool { return windows.GetCurrentProcessToken().IsElevated() }

// runService manages the Windows service. "run" is what the service control
// manager starts; the rest are for installing and controlling it.
func runService(ctx context.Context, args []string) error {
	if len(args) == 0 {
		return errors.New("usage: coreshiftd service {install|uninstall|remove-group|start|stop|run} [flags]")
	}
	switch args[0] {
	case "install":
		return installService(args[1:])
	case "uninstall":
		// Not the group: the installer of an update uninstalls the old
		// service too, and its members must stay.
		return uninstallService()
	case "remove-group":
		// Removing CoreShift.
		return service.RemoveAPIGroup()
	case "start", "stop":
		return controlService(args[0])
	case "run":
		return svc.Run(serviceName, &winService{args: args[1:]})
	}
	return fmt.Errorf("unknown service command %q", args[0])
}

func installService(args []string) error {
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	if tmp := os.TempDir(); strings.HasPrefix(strings.ToLower(exe), strings.ToLower(tmp)) {
		return errors.New("this binary lives in a temporary directory (go run?); build it first: go build -o coreshiftd.exe ./cmd/coreshiftd")
	}
	// Validate the flags now rather than at every service start.
	fs := flag.NewFlagSet("service install", flag.ContinueOnError)
	df := addDaemonFlags(fs)
	fs.String("api", defaultAPIAddr, apiAddrUsage)
	fs.Bool("exit-without-app", true, "stop, disconnecting, once the app is closed")
	if err := fs.Parse(args); err != nil {
		return err
	}
	cfg, err := df.config()
	if err != nil {
		return err
	}
	if !hasFlag(args, "cores-dir") {
		dir, _ := coresDir("")
		args = append(args, "-cores-dir", dir)
	}
	// The app starts the service and it stops once the app is closed, so
	// the VPN never runs without the app; -exit-without-app=false keeps it
	// running on its own.
	if !hasFlag(args, "exit-without-app") {
		args = append(args, "-exit-without-app")
	}

	m, err := mgr.Connect()
	if err != nil {
		return fmt.Errorf("connect to service manager (run elevated): %w", err)
	}
	defer m.Disconnect()
	s, err := m.CreateService(serviceName, exe, mgr.Config{
		DisplayName: "CoreShift VPN",
		Description: "Runs proxy cores, the TUN interface and DNS protection for the CoreShift VPN client.",
		StartType:   mgr.StartManual,
	}, append([]string{"service", "run"}, args...)...)
	if err != nil {
		return err
	}
	defer s.Close()
	if err := letUsersStart(s); err != nil {
		s.Delete()
		return fmt.Errorf("let users start the service: %w", err)
	}
	// Restart after a crash; DNS left behind is restored by Recover on start.
	_ = s.SetRecoveryActions([]mgr.RecoveryAction{
		{Type: mgr.ServiceRestart, Delay: 2 * time.Second},
		{Type: mgr.ServiceRestart, Delay: 10 * time.Second},
		{Type: mgr.ServiceRestart, Delay: time.Minute},
	}, 24*60*60)
	// A crash leaves the tunnel's DNS rule behind, and the service starts only
	// with the app: the task cleans it at boot. Without it the service still
	// works, so a failure is a warning.
	if err := installRecoveryTask(exe); err != nil {
		fmt.Fprintln(os.Stderr, "warning: the boot-time DNS recovery task was not created:", err)
	}
	// Who may use the service: the group, with the user who ran setup in
	// it. Without it only administrators can, and the app says so; the
	// service is there all the same.
	added, err := service.SetupAPIGroup(cfg.DataDir)
	for _, a := range added {
		fmt.Printf("added %s to the group %q\n", a, service.APIGroupName)
	}
	if err != nil {
		fmt.Fprintf(os.Stderr, "warning: the group %q: %v\n", service.APIGroupName, err)
	}
	fmt.Printf("installed service %q running %s %s\n", serviceName, exe, strings.Join(append([]string{"service", "run"}, args...), " "))
	return nil
}

// serviceSDDL is Windows' default access to a service, plus starting it
// (RP) for signed-in users, so the app can start the service without
// administrator rights. Stopping and configuring it stay with
// administrators.
const serviceSDDL = "D:(A;;CCLCSWRPWPDTLOCRRC;;;SY)(A;;CCDCLCSWRPWPDTLOCRSDRCWDWO;;;BA)(A;;CCLCSWRPLOCRRC;;;IU)(A;;CCLCSWLOCRRC;;;SU)"

func letUsersStart(s *mgr.Service) error {
	sd, err := windows.SecurityDescriptorFromString(serviceSDDL)
	if err != nil {
		return err
	}
	dacl, _, err := sd.DACL()
	if err != nil {
		return err
	}
	return windows.SetSecurityInfo(s.Handle, windows.SE_SERVICE, windows.DACL_SECURITY_INFORMATION, nil, nil, dacl, nil)
}

func uninstallService() error {
	m, err := mgr.Connect()
	if err != nil {
		return fmt.Errorf("connect to service manager (run elevated): %w", err)
	}
	defer m.Disconnect()
	s, err := m.OpenService(serviceName)
	if err != nil {
		return err
	}
	defer s.Close()
	if st, err := s.Query(); err == nil && st.State != svc.Stopped {
		s.Control(svc.Stop)
		waitServiceState(s, svc.Stopped)
	}
	removeRecoveryTask()
	return s.Delete()
}

func controlService(cmd string) error {
	m, err := mgr.Connect()
	if err != nil {
		return fmt.Errorf("connect to service manager (run elevated): %w", err)
	}
	defer m.Disconnect()
	s, err := m.OpenService(serviceName)
	if err != nil {
		return err
	}
	defer s.Close()
	if cmd == "start" {
		if err := s.Start(); err != nil {
			return err
		}
		return waitServiceState(s, svc.Running)
	}
	if _, err := s.Control(svc.Stop); err != nil {
		return err
	}
	return waitServiceState(s, svc.Stopped)
}

func waitServiceState(s *mgr.Service, want svc.State) error {
	for deadline := time.Now().Add(20 * time.Second); time.Now().Before(deadline); time.Sleep(200 * time.Millisecond) {
		st, err := s.Query()
		if err != nil {
			return err
		}
		if st.State == want {
			return nil
		}
	}
	return fmt.Errorf("service did not reach state %d", want)
}

func hasFlag(args []string, name string) bool {
	for _, a := range args {
		a = strings.TrimLeft(a, "-")
		if a == name || strings.HasPrefix(a, name+"=") {
			return true
		}
	}
	return false
}

type winService struct {
	args []string
}

// stopWaitHint is how long the service manager is told each step of
// stopping may take: disconnecting restores DNS and stops the TUN layer,
// which gets a few seconds to remove its adapter.
const stopWaitHint = 15 * time.Second

// stopping waits for the daemon to finish, telling the service manager it
// is still at it, so that it neither gives up on the service nor reports
// it hung, and answering its questions meanwhile.
func stopping(status chan<- svc.Status, requests <-chan svc.ChangeRequest, done <-chan error) error {
	cp := uint32(1)
	pending := func() svc.Status {
		return svc.Status{State: svc.StopPending, CheckPoint: cp, WaitHint: uint32(stopWaitHint / time.Millisecond)}
	}
	status <- pending()
	tick := time.NewTicker(stopWaitHint / 3)
	defer tick.Stop()
	for {
		select {
		case err := <-done:
			return err
		case <-tick.C:
			cp++
			status <- pending()
		case r := <-requests:
			if r.Cmd == svc.Interrogate {
				status <- pending()
			}
		}
	}
}

func (w *winService) Execute(_ []string, requests <-chan svc.ChangeRequest, status chan<- svc.Status) (bool, uint32) {
	status <- svc.Status{State: svc.StartPending}

	fs := flag.NewFlagSet("service run", flag.ContinueOnError)
	df := addDaemonFlags(fs)
	apiAddr := fs.String("api", defaultAPIAddr, apiAddrUsage)
	withApp := fs.Bool("exit-without-app", false, "stop, disconnecting, once the app is closed")
	if err := fs.Parse(w.args); err != nil {
		return true, 1
	}
	cfg, err := df.config()
	if err != nil {
		return true, 2
	}
	// Before anything in it is read or written: the directory and all in it
	// become SYSTEM's, and what users planted there goes.
	notes, secErr := service.SecureDataDir(cfg.DataDir)
	// The service restarts with the app; the log of a crashed run must outlive
	// the restarts that follow. It is the service's alone: it names sites.
	logPath := filepath.Join(cfg.DataDir, "coreshiftd.log")
	rotateLog(logPath, 3)
	log, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		return true, 3
	}
	defer log.Close()
	for _, n := range notes {
		fmt.Fprintln(log, "data directory:", n)
	}
	if secErr != nil {
		fmt.Fprintln(log, "warning: securing the data directory:", secErr)
	}
	// The installer made the group; should it be missing, it is made here,
	// before api.json is written for its members.
	added, groupErr := service.SetupAPIGroup(cfg.DataDir)
	for _, a := range added {
		fmt.Fprintf(log, "API group: added %s to %q\n", a, service.APIGroupName)
	}
	if groupErr != nil {
		fmt.Fprintf(log, "warning: API group %q: %v\n", service.APIGroupName, groupErr)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	// Only the installed service of a release build updates CoreShift;
	// `go run ... serve` never runs an installer over a working copy.
	cfg.SelfUpdate = service.Version != "dev"
	go func() { done <- serve(ctx, cfg, *apiAddr, log, false, *withApp) }()
	status <- svc.Status{State: svc.Running, Accepts: svc.AcceptStop | svc.AcceptShutdown}

	for {
		select {
		case err := <-done:
			if err != nil {
				fmt.Fprintln(log, "error:", err)
				return true, 4
			}
			return false, 0
		case r := <-requests:
			switch r.Cmd {
			case svc.Interrogate:
				status <- r.CurrentStatus
			case svc.Stop, svc.Shutdown:
				cancel()
				if err := stopping(status, requests, done); err != nil {
					fmt.Fprintln(log, "error:", err)
				}
				return false, 0
			}
		}
	}
}
