package main

import (
	"bufio"
	"bytes"
	"strconv"
	"strings"
)

// capNetAdmin is CAP_NET_ADMIN's bit: creating a TUN interface, routes,
// and changing systemd-resolved's per-link DNS need it.
const capNetAdmin = 12

// effectiveCaps parses the CapEff line of a Linux /proc/<pid>/status.
func effectiveCaps(status []byte) (uint64, bool) {
	sc := bufio.NewScanner(bytes.NewReader(status))
	for sc.Scan() {
		v, ok := strings.CutPrefix(sc.Text(), "CapEff:")
		if !ok {
			continue
		}
		caps, err := strconv.ParseUint(strings.TrimSpace(v), 16, 64)
		return caps, err == nil
	}
	return 0, false
}

func hasCap(caps uint64, bit uint) bool { return caps&(1<<bit) != 0 }
