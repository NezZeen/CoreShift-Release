package service

import (
	"net/netip"
	"slices"
	"testing"
)

func TestParseGetent(t *testing.T) {
	out := "198.18.0.7      STREAM 1.abcd.bash.ws\n198.18.0.7      DGRAM  \n198.18.0.7      RAW    \nfc00::7  STREAM \n\n"
	want := []netip.Addr{netip.MustParseAddr("198.18.0.7"), netip.MustParseAddr("fc00::7")}
	if got := parseGetent(out); !slices.Equal(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}
