package subscription

import (
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"runtime"
	"strings"
	"sync"
)

// Device identifies this machine to panels that limit the number of devices
// per subscription (Remnawave's HWID limit). Such a panel answers a request
// without x-hwid with a placeholder server instead of the real ones.
type Device struct {
	HWID      string // stable per machine, empty when unknown
	OS        string
	OSVersion string
	Model     string
}

var (
	deviceOnce sync.Once
	device     Device
	deviceID   string
	deviceMu   sync.Mutex
)

// SetDeviceID sets the machine identity where the engine cannot read one
// itself (Android passes its ANDROID_ID). Call it before the first fetch.
func SetDeviceID(id string) {
	deviceMu.Lock()
	deviceID = strings.TrimSpace(id)
	deviceMu.Unlock()
}

// ThisDevice returns what is sent to panels about this machine.
func ThisDevice() Device {
	deviceOnce.Do(func() {
		device = Device{OS: osName()}
		var id string
		id, device.OSVersion, device.Model = platformDevice()
		deviceMu.Lock()
		if deviceID != "" {
			id = deviceID
		}
		deviceMu.Unlock()
		if id != "" {
			// The panel gets a hash, never the machine's own ID.
			sum := sha256.Sum256([]byte("coreshift-hwid:" + id))
			device.HWID = hex.EncodeToString(sum[:16])
		}
	})
	return device
}

func (d Device) setHeaders(h http.Header) {
	if d.HWID == "" {
		return
	}
	h.Set("x-hwid", d.HWID)
	h.Set("x-device-os", d.OS)
	if d.OSVersion != "" {
		h.Set("x-ver-os", d.OSVersion)
	}
	if d.Model != "" {
		h.Set("x-device-model", d.Model)
	}
}

func osName() string {
	switch runtime.GOOS {
	case "windows":
		return "Windows"
	case "linux":
		return "Linux"
	case "android":
		return "Android"
	case "darwin":
		return "macOS"
	}
	return runtime.GOOS
}
