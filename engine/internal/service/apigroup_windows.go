package service

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

// Who may use the service on Windows.
//
// The token in api.json lets its reader do whatever the app does: read the
// subscription links, add a subscription or a DNS server and send the
// traffic of everyone on the computer through it. So, as with the coreshift
// group on Linux, only the members of a local group may read the file,
// besides SYSTEM and Administrators (an administrator's non-elevated app is
// not one of them: UAC leaves the group out of its token).
//
// The installer creates the group and adds the user who ran it, the user
// signed in to its session even when an administrator's password elevated
// it ("coreshiftd service install", SetupAPIGroup). The first version with
// the group, installed by a self-update as SYSTEM, adds the users whose
// app was running (pending.json). An administrator adds others:
//
//	net localgroup "CoreShift Users" <user> /add
//
// Windows puts a group into a user's token at sign-in only, so api.json
// names each member as well as the group (sharedSDDL): a member added now
// may read it once the service next writes its permissions, which it does
// every half minute (KeepShared), without signing out and in. Uninstalling
// removes the group ("coreshiftd service remove-group").
const APIGroupName = "CoreShift Users"

const apiGroupComment = "Users who may use the CoreShift VPN service"

var (
	netapi32                    = windows.NewLazySystemDLL("netapi32.dll")
	procNetLocalGroupAdd        = netapi32.NewProc("NetLocalGroupAdd")
	procNetLocalGroupDel        = netapi32.NewProc("NetLocalGroupDel")
	procNetLocalGroupAddMembers = netapi32.NewProc("NetLocalGroupAddMembers")
	procNetLocalGroupGetMembers = netapi32.NewProc("NetLocalGroupGetMembers")

	wtsapi32                        = windows.NewLazySystemDLL("wtsapi32.dll")
	procWTSQuerySessionInformationW = wtsapi32.NewProc("WTSQuerySessionInformationW")
)

// Results of the NetLocalGroup functions.
const (
	nerrSuccess        = 0
	errorNoSuchAlias   = 1376
	errorMemberInAlias = 1378
	errorAliasExists   = 1379
	nerrGroupNotFound  = 2220
	nerrGroupExists    = 2223
	maxPreferredLength = 0xFFFFFFFF
)

// WTS_INFO_CLASS values.
const (
	wtsUserName   = 5
	wtsDomainName = 7
)

type localGroupInfo1 struct {
	name, comment *uint16
}

type localGroupMembersInfo0 struct {
	sid *windows.SID
}

func netErr(op string, r uintptr) error {
	return fmt.Errorf("%s: %w", op, syscall.Errno(r))
}

// computerName is the name local accounts are qualified with.
func computerName() (string, error) {
	buf := make([]uint16, windows.MAX_COMPUTERNAME_LENGTH+1)
	n := uint32(len(buf))
	if err := windows.GetComputerName(&buf[0], &n); err != nil {
		return "", err
	}
	return windows.UTF16ToString(buf[:n]), nil
}

// apiGroupSID is the SID of the local group, or an error when there is
// none. The name is qualified with the computer's: a domain group of the
// same name is not it.
func apiGroupSID() (*windows.SID, error) {
	host, err := computerName()
	if err != nil {
		return nil, err
	}
	sid, domain, kind, err := windows.LookupSID("", host+`\`+APIGroupName)
	if err != nil {
		return nil, err
	}
	if kind != windows.SidTypeAlias || !strings.EqualFold(domain, host) {
		return nil, fmt.Errorf("%s\\%s is not a local group", domain, APIGroupName)
	}
	return sid, nil
}

// EnsureAPIGroup creates the local group when it is missing; created
// reports that it was.
func EnsureAPIGroup() (created bool, err error) {
	name, err := windows.UTF16PtrFromString(APIGroupName)
	if err != nil {
		return false, err
	}
	comment, _ := windows.UTF16PtrFromString(apiGroupComment)
	info := localGroupInfo1{name: name, comment: comment}
	r, _, _ := procNetLocalGroupAdd.Call(0, 1, uintptr(unsafe.Pointer(&info)), 0)
	switch r {
	case nerrSuccess:
		return true, nil
	case errorAliasExists, nerrGroupExists:
		return false, nil
	}
	return false, netErr("create group "+APIGroupName, r)
}

// RemoveAPIGroup deletes the local group; a missing one is no error.
func RemoveAPIGroup() error {
	name, err := windows.UTF16PtrFromString(APIGroupName)
	if err != nil {
		return err
	}
	r, _, _ := procNetLocalGroupDel.Call(0, uintptr(unsafe.Pointer(name)))
	switch r {
	case nerrSuccess, errorNoSuchAlias, nerrGroupNotFound:
		return nil
	}
	return netErr("remove group "+APIGroupName, r)
}

// addToAPIGroup adds sid to the group; a member already is no error.
func addToAPIGroup(sid *windows.SID) error {
	name, err := windows.UTF16PtrFromString(APIGroupName)
	if err != nil {
		return err
	}
	m := localGroupMembersInfo0{sid: sid}
	r, _, _ := procNetLocalGroupAddMembers.Call(0, uintptr(unsafe.Pointer(name)), 0, uintptr(unsafe.Pointer(&m)), 1)
	if r == nerrSuccess || r == errorMemberInAlias {
		return nil
	}
	return netErr("add "+sid.String()+" to "+APIGroupName, r)
}

// localGroupMembers returns the SIDs of the members of a local group, as
// strings.
func localGroupMembers(group string) ([]string, error) {
	name, err := windows.UTF16PtrFromString(group)
	if err != nil {
		return nil, err
	}
	var buf *byte
	var read, total uint32
	r, _, _ := procNetLocalGroupGetMembers.Call(0, uintptr(unsafe.Pointer(name)), 0, uintptr(unsafe.Pointer(&buf)),
		maxPreferredLength, uintptr(unsafe.Pointer(&read)), uintptr(unsafe.Pointer(&total)), 0)
	if buf != nil {
		defer windows.NetApiBufferFree(buf)
	}
	if r != nerrSuccess {
		return nil, netErr("list "+group, r)
	}
	members := unsafe.Slice((*localGroupMembersInfo0)(unsafe.Pointer(buf)), read)
	out := make([]string, 0, read)
	for _, m := range members {
		if m.sid != nil && m.sid.IsValid() {
			out = append(out, m.sid.String())
		}
	}
	return out, nil
}

// sessionUser returns the SID of the user signed in to a session; an error
// when nobody is (session 0, the services').
func sessionUser(session uint32) (*windows.SID, error) {
	query := func(class uint32) (string, error) {
		var p *uint16
		var n uint32
		r, _, err := procWTSQuerySessionInformationW.Call(0, uintptr(session), uintptr(class), uintptr(unsafe.Pointer(&p)), uintptr(unsafe.Pointer(&n)))
		if r == 0 {
			return "", err
		}
		defer windows.WTSFreeMemory(uintptr(unsafe.Pointer(p)))
		return windows.UTF16PtrToString(p), nil
	}
	user, err := query(wtsUserName)
	if err != nil {
		return nil, err
	}
	if user == "" {
		return nil, fmt.Errorf("nobody is signed in to session %d", session)
	}
	domain, err := query(wtsDomainName)
	if err != nil {
		return nil, err
	}
	sid, _, kind, err := windows.LookupSID("", domain+`\`+user)
	if err != nil {
		return nil, err
	}
	if kind != windows.SidTypeUser {
		return nil, fmt.Errorf("%s\\%s is not a user", domain, user)
	}
	return sid, nil
}

// SetupAPIGroup is what the installer does ("coreshiftd service install"):
// it creates the group when missing and adds the user who ran setup, the
// one signed in to this process's session. When the group is new (a first
// install, or the first update from a version without it) it also adds
// the users whose app the update interrupted (dataDir/updates/
// pending.json), so that a self-update, which runs as SYSTEM where nobody
// is signed in, keeps CoreShift working for them; failing those, the user
// at the console. It returns the accounts it added, for the log.
func SetupAPIGroup(dataDir string) (added []string, err error) {
	created, err := EnsureAPIGroup()
	if err != nil {
		return nil, err
	}
	var sessions []uint32
	var own uint32
	if windows.ProcessIdToSessionId(windows.GetCurrentProcessId(), &own) == nil && own != 0 {
		sessions = append(sessions, own)
	}
	if created {
		sessions = append(sessions, pendingSessions(dataDir)...)
		if len(sessions) == 0 {
			if console := windows.WTSGetActiveConsoleSessionId(); console != 0xFFFFFFFF && console != 0 {
				sessions = append(sessions, console)
			}
		}
	}
	var errs []error
	var seen []string
	for _, s := range sessions {
		sid, err := sessionUser(s)
		if err != nil {
			continue // signed out meanwhile, or nobody there
		}
		if slices.Contains(seen, sid.String()) {
			continue
		}
		seen = append(seen, sid.String())
		if err := addToAPIGroup(sid); err != nil {
			errs = append(errs, err)
			continue
		}
		name := sid.String()
		if account, domain, _, err := sid.LookupAccount(""); err == nil {
			name = domain + `\` + account
		}
		added = append(added, name)
	}
	return added, errors.Join(errs...)
}

// pendingSessions are the sessions whose app a self-update interrupted.
func pendingSessions(dataDir string) []uint32 {
	b, err := os.ReadFile(filepath.Join(dataDir, "updates", "pending.json"))
	if err != nil {
		return nil
	}
	var p pendingUpdate
	if json.Unmarshal(b, &p) != nil {
		return nil
	}
	return p.Sessions
}

// sharedSDDL is the DACL of api.json: SYSTEM and Administrators in full,
// and read for the group and for each of its members by name, so that a
// member added during the current sign-in gets in. Without a group only
// SYSTEM and Administrators. Anything in members that is not a SID is
// left out.
func sharedSDDL(group string, members []string) string {
	var b strings.Builder
	b.WriteString("D:P(A;;FA;;;SY)(A;;FA;;;BA)")
	if isSIDString(group) {
		b.WriteString("(A;;FR;;;" + group + ")")
	}
	ms := slices.Clone(members)
	slices.Sort(ms)
	for _, m := range slices.Compact(ms) {
		if isSIDString(m) && m != group {
			b.WriteString("(A;;FR;;;" + m + ")")
		}
	}
	return b.String()
}

// isSIDString reports whether s is a SID in its string form: "S-1-",
// then numbers and dashes. Nothing else may go into an SDDL string.
func isSIDString(s string) bool {
	rest, ok := strings.CutPrefix(s, "S-1-")
	if !ok || rest == "" {
		return false
	}
	for _, c := range rest {
		if (c < '0' || c > '9') && c != '-' {
			return false
		}
	}
	_, err := windows.StringToSid(s)
	return err == nil
}

// currentSharedSDDL is sharedSDDL for the group as it is now.
func currentSharedSDDL() string {
	sid, err := apiGroupSID()
	if err != nil {
		return sharedSDDL("", nil)
	}
	members, _ := localGroupMembers(APIGroupName)
	return sharedSDDL(sid.String(), members)
}
