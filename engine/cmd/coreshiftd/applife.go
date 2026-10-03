package main

import (
	"context"
	"fmt"
	"io"
	"time"
)

// appSession is what autoConnectWithApp needs of the service.
type appSession interface {
	WaitAppAttached(ctx context.Context) bool
	WaitAppGone(ctx context.Context, first, grace time.Duration) bool
	AutoConnect(ctx context.Context) error
}

// autoConnectWithApp runs auto-connect each time the app starts: when it
// attaches after having been gone for appGrace, so an event stream that
// merely reconnects does not count. This is when the Windows service, which
// starts with the app, auto-connects; the Linux service runs from boot, and
// the app starts at sign-in ("Автозапуск"). AutoConnect connects only from
// idle, so a VPN that is already up, or that the user turned off within
// the same app session, is left as it is. Closing the app never
// disconnects. Until ctx ends.
func autoConnectWithApp(ctx context.Context, svc appSession, log io.Writer) {
	for {
		if !svc.WaitAppAttached(ctx) {
			return
		}
		done := make(chan struct{})
		go func() {
			defer close(done)
			if err := svc.AutoConnect(ctx); err != nil && ctx.Err() == nil {
				fmt.Fprintln(log, "auto-connect:", err)
			}
		}()
		gone := svc.WaitAppGone(ctx, appGrace, appGrace)
		// One auto-connect at a time: its retries end within minutes.
		<-done
		if !gone {
			return
		}
	}
}
