package supervisor

import "time"

// SetHealthFloor sets the least time between two checks of a healthy
// connection, whatever Health.Interval says: a phone in battery saver asks
// for fewer requests through the server (each one keeps the radio awake).
// Zero lifts it. Like the idle pace (SetIdle), it never slows a check that
// failed, so a server that stops answering is left as quickly once seen.
// A floor set lower or lifted applies from the next check on.
func (s *Supervisor) SetHealthFloor(d time.Duration) {
	s.healthFloor.Store(int64(max(d, 0)))
}

// deferredBack stands for a return to the primary core that came due while
// the device was idle: trying the primary means starting a second core
// aside and checking it through the server, a phone with its screen off
// is better off without, and the backup works meanwhile. It never fires;
// backAfterIdle turns it into a timer once the device is in use again.
var deferredBack = make(<-chan time.Time)

// backOnWake is how soon after the device is in use again a deferred
// return is tried: after the check that waking brings.
const backOnWake = 5 * time.Second

// backAfterIdle is the monitor's return timer once the device is no longer
// idle: a return put off meanwhile comes now, any other stays as it is.
func backAfterIdle(back <-chan time.Time) <-chan time.Time {
	if back == deferredBack {
		return time.After(backOnWake)
	}
	return back
}
