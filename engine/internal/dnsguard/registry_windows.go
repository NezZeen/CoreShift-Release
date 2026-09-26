package dnsguard

import (
	"errors"

	winreg "golang.org/x/sys/windows/registry"
)

// hklm implements registry on HKEY_LOCAL_MACHINE.
type hklm struct{}

func notExist(err error) bool { return errors.Is(err, winreg.ErrNotExist) }

func (hklm) CreateKey(path string) error {
	k, _, err := winreg.CreateKey(winreg.LOCAL_MACHINE, path, winreg.ALL_ACCESS)
	if err != nil {
		return err
	}
	return k.Close()
}

func (hklm) DeleteKey(path string) error {
	if err := winreg.DeleteKey(winreg.LOCAL_MACHINE, path); err != nil && !notExist(err) {
		return err
	}
	return nil
}

func (hklm) SubKeys(path string) ([]string, error) {
	k, err := winreg.OpenKey(winreg.LOCAL_MACHINE, path, winreg.ENUMERATE_SUB_KEYS|winreg.QUERY_VALUE)
	if notExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	defer k.Close()
	return k.ReadSubKeyNames(0)
}

func (hklm) GetString(path, name string) (string, bool, error) {
	k, err := winreg.OpenKey(winreg.LOCAL_MACHINE, path, winreg.QUERY_VALUE)
	if notExist(err) {
		return "", false, nil
	}
	if err != nil {
		return "", false, err
	}
	defer k.Close()
	v, _, err := k.GetStringValue(name)
	if notExist(err) {
		return "", false, nil
	}
	return v, err == nil, err
}

func (hklm) SetString(path, name, val string) error {
	return withKey(path, func(k winreg.Key) error { return k.SetStringValue(name, val) })
}

func (hklm) SetStrings(path, name string, val []string) error {
	return withKey(path, func(k winreg.Key) error { return k.SetStringsValue(name, val) })
}

func (hklm) GetDWORD(path, name string) (uint32, bool, error) {
	k, err := winreg.OpenKey(winreg.LOCAL_MACHINE, path, winreg.QUERY_VALUE)
	if notExist(err) {
		return 0, false, nil
	}
	if err != nil {
		return 0, false, err
	}
	defer k.Close()
	v, _, err := k.GetIntegerValue(name)
	if notExist(err) {
		return 0, false, nil
	}
	return uint32(v), err == nil, err
}

func (hklm) SetDWORD(path, name string, val uint32) error {
	return withKey(path, func(k winreg.Key) error { return k.SetDWordValue(name, val) })
}

func (hklm) DeleteValue(path, name string) error {
	err := withKey(path, func(k winreg.Key) error { return k.DeleteValue(name) })
	if err != nil && !notExist(err) {
		return err
	}
	return nil
}

func withKey(path string, fn func(winreg.Key) error) error {
	k, err := winreg.OpenKey(winreg.LOCAL_MACHINE, path, winreg.SET_VALUE)
	if err != nil {
		return err
	}
	defer k.Close()
	return fn(k)
}
