package proc

import (
	"bufio"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"io/fs"
	"net/netip"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// listens finds the listening sockets for addr in /proc/net/tcp and tcp6
// and looks for their inodes among pid's open files. Android apps may not
// read /proc/net/tcp*: ErrOwnerUnknown there.
func listens(pid int, addr netip.AddrPort) (bool, error) {
	var inodes []string
	for _, f := range []string{"/proc/net/tcp", "/proc/net/tcp6"} {
		in, err := listenInodes(f, addr)
		if errors.Is(err, fs.ErrPermission) {
			return false, ErrOwnerUnknown
		}
		if err != nil && !errors.Is(err, fs.ErrNotExist) {
			return false, err
		}
		inodes = append(inodes, in...)
	}
	if len(inodes) == 0 {
		return false, nil
	}
	dir := filepath.Join("/proc", strconv.Itoa(pid), "fd")
	fds, err := os.ReadDir(dir)
	if err != nil {
		return false, err
	}
	own := make(map[string]bool, len(fds))
	for _, fd := range fds {
		if l, err := os.Readlink(filepath.Join(dir, fd.Name())); err == nil {
			if in, ok := strings.CutPrefix(l, "socket:["); ok {
				own[strings.TrimSuffix(in, "]")] = true
			}
		}
	}
	for _, in := range inodes {
		if !own[in] {
			return false, nil
		}
	}
	return true, nil
}

// tcpListen is the state of a listening socket in /proc/net/tcp.
const tcpListen = "0A"

// listenInodes returns the inodes of the listening sockets in the table
// at path (/proc/net/tcp or tcp6) that take connections to addr.
func listenInodes(path string, addr netip.AddrPort) ([]string, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	var out []string
	sc := bufio.NewScanner(f)
	sc.Scan() // the header
	for sc.Scan() {
		// sl local_address rem_address st tx_queue:rx_queue tr:tm->when retrnsmt uid timeout inode
		fields := strings.Fields(sc.Text())
		if len(fields) < 10 || fields[3] != tcpListen {
			continue
		}
		local, ok := parseProcAddr(fields[1])
		if ok && matches(local, addr) {
			out = append(out, fields[9])
		}
	}
	return out, sc.Err()
}

// parseProcAddr reads "0100007F:4E22": the address as the kernel's 32-bit
// words in host order (little-endian on every platform CoreShift runs
// on), the port in hex.
func parseProcAddr(s string) (netip.AddrPort, bool) {
	h, p, ok := strings.Cut(s, ":")
	if !ok {
		return netip.AddrPort{}, false
	}
	port, err := strconv.ParseUint(p, 16, 16)
	if err != nil {
		return netip.AddrPort{}, false
	}
	raw, err := hex.DecodeString(h)
	if err != nil || (len(raw) != 4 && len(raw) != 16) {
		return netip.AddrPort{}, false
	}
	b := make([]byte, len(raw))
	for i := 0; i < len(raw); i += 4 {
		binary.LittleEndian.PutUint32(b[i:], binary.BigEndian.Uint32(raw[i:]))
	}
	a, _ := netip.AddrFromSlice(b)
	return netip.AddrPortFrom(a, uint16(port)), true
}
