package service

import (
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"golang.org/x/sys/windows"
)

const (
	testGroup  = "S-1-5-21-1111111111-2222222222-3333333333-1001"
	testMember = "S-1-5-21-1111111111-2222222222-3333333333-1002"
)

// api.json is for SYSTEM, Administrators, the group and its members: not
// for every user (BU), interactive user (IU), authenticated user (AU) or
// everyone (WD), as it was before.
func TestSharedSDDL(t *testing.T) {
	got := sharedSDDL(testGroup, []string{testMember, testMember, testGroup, "S-1-5-21-1-2-3-4)(A;;FA;;;WD", "BU", ""})
	want := "D:P(A;;FA;;;SY)(A;;FA;;;BA)(A;;FR;;;" + testGroup + ")(A;;FR;;;" + testMember + ")"
	if got != want {
		t.Errorf("sharedSDDL = %s\nwant          %s", got, want)
	}
	if none := sharedSDDL("", nil); none != "D:P(A;;FA;;;SY)(A;;FA;;;BA)" {
		t.Errorf("without a group: %s", none)
	}
	if bad := sharedSDDL("BU", []string{"WD"}); bad != "D:P(A;;FA;;;SY)(A;;FA;;;BA)" {
		t.Errorf("aliases taken for SIDs: %s", bad)
	}
	for _, s := range []string{got, sharedSDDL("", nil)} {
		sd, err := windows.SecurityDescriptorFromString(s)
		if err != nil {
			t.Fatalf("%s: %v", s, err)
		}
		text := sd.String()
		for _, broad := range []string{";BU)", ";IU)", ";AU)", ";WD)"} {
			if strings.Contains(text, broad) {
				t.Errorf("%s grants %s", text, broad)
			}
		}
		if ctl, _, _ := sd.Control(); ctl&windows.SE_DACL_PROTECTED == 0 {
			t.Errorf("%s inherits from the directory", text)
		}
	}
}

func TestIsSIDString(t *testing.T) {
	for s, want := range map[string]bool{
		testGroup: true, "S-1-5-32-544": true, "S-1-5-18": true,
		"": false, "BU": false, "S-1-": false, "S-1-5-x": false, "S-1-5-21)(A;;FA;;;WD": false, "s-1-5-18": false,
	} {
		if got := isSIDString(s); got != want {
			t.Errorf("isSIDString(%q) = %v", s, got)
		}
	}
}

// A member named in the DACL reads the file although the group is not in
// the token of the current sign-in; a user who is neither is refused.
func TestSharedSDDLLetsMembersIn(t *testing.T) {
	me, err := windows.GetCurrentProcessToken().GetTokenUser()
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "api.json")
	if err := os.WriteFile(path, []byte(`{"token":"x"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := setDACL(path, sharedSDDL(testGroup, []string{me.User.Sid.String()})); err != nil {
		t.Fatal(err)
	}
	if _, err := os.ReadFile(path); err != nil {
		t.Errorf("a member named in the DACL: %v", err)
	}
	if err := setDACL(path, sharedSDDL(testGroup, nil)); err != nil {
		t.Fatal(err)
	}
	_, err = os.ReadFile(path)
	if elevated() {
		t.Log("elevated: Administrators may read it, skipping the refusal")
	} else if !errors.Is(err, windows.ERROR_ACCESS_DENIED) {
		t.Errorf("a user outside the group: %v, want access denied", err)
	}
	// Given back, so that the directory can be removed.
	setDACL(path, "D:P(A;;FA;;;"+me.User.Sid.String()+")")
}

// The user signed in to this session is who ran setup.
func TestSessionUser(t *testing.T) {
	var session uint32
	if err := windows.ProcessIdToSessionId(windows.GetCurrentProcessId(), &session); err != nil || session == 0 {
		t.Skip("not in a user's session")
	}
	me, err := windows.GetCurrentProcessToken().GetTokenUser()
	if err != nil {
		t.Fatal(err)
	}
	got, err := sessionUser(session)
	if err != nil {
		t.Fatal(err)
	}
	if !got.Equals(me.User.Sid) {
		t.Errorf("session user %s, want %s", got, me.User.Sid)
	}
	if _, err := sessionUser(0); err == nil {
		t.Error("a user in session 0")
	}
}

// The members are read as the service reads the API group's, here from
// the built-in Users group (its name depends on the language), which
// holds INTERACTIVE and Authenticated Users on every Windows.
func TestLocalGroupMembers(t *testing.T) {
	users, err := windows.CreateWellKnownSid(windows.WinBuiltinUsersSid)
	if err != nil {
		t.Fatal(err)
	}
	name, _, _, err := users.LookupAccount("")
	if err != nil {
		t.Fatal(err)
	}
	members, err := localGroupMembers(name)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"S-1-5-4", "S-1-5-11"} {
		if !slices.Contains(members, want) {
			t.Errorf("members of %s: %v, want %s among them", name, members, want)
		}
	}
	if _, err := localGroupMembers("CoreShift no such group"); err == nil {
		t.Error("members of a group that does not exist")
	}
}

func TestPendingSessions(t *testing.T) {
	dir := t.TempDir()
	if s := pendingSessions(dir); s != nil {
		t.Errorf("no update: %v", s)
	}
	os.MkdirAll(filepath.Join(dir, "updates"), 0o700)
	os.WriteFile(filepath.Join(dir, "updates", "pending.json"), []byte(`{"to":"0.8.0+1","sessions":[1,3]}`), 0o600)
	if s := pendingSessions(dir); len(s) != 2 || s[0] != 1 || s[1] != 3 {
		t.Errorf("sessions %v, want [1 3]", s)
	}
}

// Without the group (as on a computer where CoreShift is not installed)
// api.json is for SYSTEM and Administrators only, never for all users.
func TestCurrentSharedSDDLWithoutGroup(t *testing.T) {
	if _, err := apiGroupSID(); err == nil {
		t.Skip("the group exists here")
	}
	if got := currentSharedSDDL(); got != sharedSDDL("", nil) {
		t.Errorf("without the group: %s", got)
	}
}
