package main

import (
	"errors"
	"testing"
	"time"

	"golang.org/x/sys/windows/svc"
)

// While the daemon disconnects, the service manager hears that the service
// is stopping and how long a step may take, and gets answers to its
// questions.
func TestStoppingReportsProgress(t *testing.T) {
	status := make(chan svc.Status, 10)
	requests := make(chan svc.ChangeRequest)
	done := make(chan error, 1)
	result := make(chan error, 1)
	go func() { result <- stopping(status, requests, done) }()

	st := <-status
	if st.State != svc.StopPending || st.WaitHint == 0 || st.CheckPoint == 0 {
		t.Errorf("first status = %+v", st)
	}
	requests <- svc.ChangeRequest{Cmd: svc.Interrogate}
	if st := <-status; st.State != svc.StopPending {
		t.Errorf("answer = %+v", st)
	}
	done <- errors.New("x")
	select {
	case err := <-result:
		if err == nil || err.Error() != "x" {
			t.Errorf("err = %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("stopping did not return")
	}
}
