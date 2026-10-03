package subscription

import (
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/url"
	"runtime"
	"strings"
	"sync"
)

// Device identifies this machine to panels that limit the number of devices
// per subscription (Remnawave's HWID limit). Such a panel answers a request
// without x-hwid with a placeholder server instead of the real ones.
type Device struct {
	// HWID is the machine-wide id earlier versions sent to every panel;
	// empty when unknown. ForPanel derives the id a panel gets now.
	HWID      string
	OS        string
	OSVersion string
	Model     string

	machine string // the machine's own id, never sent
}

var (
	deviceOnce  sync.Once
	device      Device
	deviceID    string
	deviceOSVer string
	deviceModel string
	deviceMu    sync.Mutex
)

// SetDeviceID sets the machine identity where the engine cannot read one
// itself (Android passes its ANDROID_ID). Call it before the first fetch.
func SetDeviceID(id string) {
	deviceMu.Lock()
	deviceID = strings.TrimSpace(id)
	deviceMu.Unlock()
}

// SetDeviceInfo sets the identity, OS version and model where the engine
// cannot read them itself: Android passes its ANDROID_ID, the Android
// version and the phone's maker and model. Call it before the first fetch.
func SetDeviceInfo(id, osVersion, model string) {
	deviceMu.Lock()
	deviceID, deviceOSVer, deviceModel = strings.TrimSpace(id), strings.TrimSpace(osVersion), strings.TrimSpace(model)
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
		if deviceOSVer != "" {
			device.OSVersion = deviceOSVer
		}
		if deviceModel != "" {
			device.Model = deviceModel
		}
		deviceMu.Unlock()
		if id != "" {
			// The panel gets a hash, never the machine's own ID.
			sum := sha256.Sum256([]byte("coreshift-hwid:" + id))
			device.HWID = hex.EncodeToString(sum[:16])
			device.machine = id
		}
	})
	return device
}

// ForPanel returns the device as the panel serving rawURL sees it: its
// HWID is a hash of the machine's id and the panel's host, stable for that
// panel, so that different panels cannot tell they serve the same machine.
// With legacy set it keeps the machine-wide HWID earlier versions sent to
// every panel: a panel that counted the device under it would otherwise
// count it again, as a new device, against the subscription's limit.
func (d Device) ForPanel(rawURL string, legacy bool) Device {
	if legacy || d.machine == "" {
		return d
	}
	d.HWID = panelHWID(d.machine, rawURL)
	return d
}

// panelHWID derives the HWID the panel at rawURL gets from the machine's id.
func panelHWID(machine, rawURL string) string {
	sum := sha256.Sum256([]byte("coreshift-hwid-panel:" + machine + "|" + PanelHost(rawURL)))
	return hex.EncodeToString(sum[:16])
}

// PanelHost is the host (and port, if any) of a subscription URL in lower
// case: what tells one panel from another.
func PanelHost(rawURL string) string {
	u, err := url.Parse(strings.TrimSpace(rawURL))
	if err != nil {
		return ""
	}
	return strings.ToLower(u.Host)
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
