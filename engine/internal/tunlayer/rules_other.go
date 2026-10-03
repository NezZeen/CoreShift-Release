//go:build !linux || android

package tunlayer

import "context"

// CleanupRoutes has nothing to do here: no policy routing outside Linux,
// and Android's VPN service owns its routes.
func CleanupRoutes(context.Context, string) error { return nil }
