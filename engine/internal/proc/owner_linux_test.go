package proc

import "testing"

func TestParseProcAddr(t *testing.T) {
	for in, want := range map[string]string{
		"0100007F:4E22":                         "127.0.0.1:20002",
		"00000000:0050":                         "0.0.0.0:80",
		"00000000000000000000000000000000:1F90": "[::]:8080",
		"0000000000000000FFFF00000100007F:4E22": "[::ffff:127.0.0.1]:20002",
	} {
		got, ok := parseProcAddr(in)
		if !ok || got.String() != want {
			t.Errorf("%s: %q %v, want %q", in, got, ok, want)
		}
	}
}
