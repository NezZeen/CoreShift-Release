package main

import (
	"bytes"
	"encoding/xml"
	"fmt"
	"os"
	"unicode/utf16"
)

// recoveryTaskName is the scheduled task that undoes the DNS changes of a
// crashed run when Windows starts.
//
// The service starts only when the app does, and a power cut or a blue
// screen leaves the tunnel's DNS rule in the registry with no tunnel behind
// it: no name resolves until someone opens the app. The task runs
// "coreshiftd dns recover" as SYSTEM at boot, before anyone signs in.
const recoveryTaskName = "CoreShift DNS recovery"

// bootRecoveryTaskXML is the task's definition. It is XML, not schtasks
// flags, because tasks made by flags do not start on battery power, which
// is when a laptop that lost power boots.
func bootRecoveryTaskXML(exe string) string {
	var path bytes.Buffer
	xml.EscapeText(&path, []byte(exe))
	return `<?xml version="1.0" encoding="UTF-16"?>
<Task version="1.2" xmlns="http://schemas.microsoft.com/windows/2004/02/mit/task">
  <RegistrationInfo>
    <Description>Undoes the DNS changes a crashed CoreShift run left behind, so the network works before CoreShift is opened.</Description>
  </RegistrationInfo>
  <Triggers>
    <BootTrigger>
      <Enabled>true</Enabled>
    </BootTrigger>
  </Triggers>
  <Principals>
    <Principal id="Author">
      <UserId>S-1-5-18</UserId>
      <RunLevel>HighestAvailable</RunLevel>
    </Principal>
  </Principals>
  <Settings>
    <MultipleInstancesPolicy>IgnoreNew</MultipleInstancesPolicy>
    <DisallowStartIfOnBatteries>false</DisallowStartIfOnBatteries>
    <StopIfGoingOnBatteries>false</StopIfGoingOnBatteries>
    <AllowHardTerminate>true</AllowHardTerminate>
    <StartWhenAvailable>true</StartWhenAvailable>
    <ExecutionTimeLimit>PT5M</ExecutionTimeLimit>
    <Enabled>true</Enabled>
  </Settings>
  <Actions Context="Author">
    <Exec>
      <Command>` + path.String() + `</Command>
      <Arguments>dns recover</Arguments>
    </Exec>
  </Actions>
</Task>
`
}

// utf16File writes text as UTF-16 with a byte order mark, which is what the
// Task Scheduler reads without guessing.
func utf16File(path, text string) error {
	units := utf16.Encode([]rune(text))
	b := make([]byte, 0, 2+2*len(units))
	b = append(b, 0xFF, 0xFE)
	for _, u := range units {
		b = append(b, byte(u), byte(u>>8))
	}
	return os.WriteFile(path, b, 0o600)
}

// rotateLog keeps the last keep logs next to path as path.1 (the newest)
// to path.<keep>, and leaves path free for a new one. The service restarts
// with the app, so one old copy would be gone by the second start after a
// crash, the very log that says what happened.
func rotateLog(path string, keep int) {
	if fi, err := os.Stat(path); err != nil || fi.Size() == 0 {
		return
	}
	os.Remove(fmt.Sprintf("%s.%d", path, keep))
	for i := keep - 1; i >= 1; i-- {
		os.Rename(fmt.Sprintf("%s.%d", path, i), fmt.Sprintf("%s.%d", path, i+1))
	}
	os.Rename(path, path+".1")
}
