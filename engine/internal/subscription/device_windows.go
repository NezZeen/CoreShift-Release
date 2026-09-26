package subscription

import (
	"fmt"
	"strings"

	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/registry"
)

// platformDevice reads the machine GUID Windows creates at setup, the OS
// build and the board's make and model.
func platformDevice() (id, version, model string) {
	id = regString(`SOFTWARE\Microsoft\Cryptography`, "MachineGuid")
	v := windows.RtlGetVersion()
	version = fmt.Sprintf("%d.%d.%d", v.MajorVersion, v.MinorVersion, v.BuildNumber)
	bios := `HARDWARE\DESCRIPTION\System\BIOS`
	model = strings.TrimSpace(regString(bios, "SystemManufacturer") + " " + regString(bios, "SystemProductName"))
	return id, version, model
}

func regString(path, name string) string {
	k, err := registry.OpenKey(registry.LOCAL_MACHINE, path, registry.QUERY_VALUE|registry.WOW64_64KEY)
	if err != nil {
		return ""
	}
	defer k.Close()
	s, _, err := k.GetStringValue(name)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(s)
}
