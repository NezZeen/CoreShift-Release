package service

import "testing"

func TestRuPlural(t *testing.T) {
	for n, want := range map[int]string{
		0: "раз", 1: "раз", 2: "раза", 4: "раза", 5: "раз", 11: "раз", 12: "раз", 14: "раз",
		21: "раз", 22: "раза", 25: "раз", 101: "раз", 111: "раз", 112: "раз", 122: "раза",
	} {
		if got := ruPlural(n, "раз", "раза", "раз"); got != want {
			t.Errorf("ruPlural(%d) = %q, want %q", n, got, want)
		}
	}
	for n, want := range map[int]string{
		1: "запись", 2: "записи", 4: "записи", 5: "записей", 11: "записей", 12: "записей", 14: "записей",
		21: "запись", 22: "записи", 25: "записей", 101: "запись", 111: "записей", -3: "записи",
	} {
		if got := ruPlural(n, "запись", "записи", "записей"); got != want {
			t.Errorf("ruPlural(%d) = %q, want %q", n, got, want)
		}
	}
}
