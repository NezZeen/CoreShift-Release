// Command fakecore imitates a proxy core for supervisor tests. It is copied
// under the name of each core (xray, sing-box, mihomo) and reads its
// behaviour from FAKECORE_<NAME>, e.g. FAKECORE_SING_BOX:
//
//	ok (default)            serve forever
//	crash-start             print an error and exit before listening
//	crash-start-once:<file> crash-start unless <file> exists, then create it
//	crash-after:<duration>  serve, then exit with a panic message
//	unhealthy               accept SOCKS but answer health checks with 503
//	unhealthy-after:<dur>   healthy at first, 503 afterwards
//	eof-first:<n>           close the first n health requests unanswered (EOF)
//	hang-after:<dur>        serve, then stop taking connections but keep running
//	offline-file:<file>     answer health checks with 503 while <file> exists,
//	                        as every check fails while the device has no network
//	port-taken              say its port is taken (as mihomo does), serve anyway
//	port-taken-after:<dur>  serve, then say its port is taken
//
// FAKECORE_FORWARD=<zone>=<host:port> relays connections to names in the
// zone to host:port, as a real core reaches them, name resolved remotely.
//
// The SOCKS port, and the credentials it then requires, are read from the
// config the supervisor generated, so the real adapters and config files
// are exercised. With a Clash API address in
// the config (sing-box, mihomo) it also answers /connections with traffic
// that grows on every call. "version" and "-v" print a version.
package main

import (
	"bufio"
	"encoding/binary"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync/atomic"
	"time"
)

func main() {
	name := strings.TrimSuffix(filepath.Base(os.Args[0]), ".exe")
	if len(os.Args) == 2 && (os.Args[1] == "version" || os.Args[1] == "-v") {
		fmt.Println(name, "version 1.2.3")
		return
	}
	go serveStats()
	mode := os.Getenv("FAKECORE_" + strings.ToUpper(strings.ReplaceAll(name, "-", "_")))
	port, err := configPort()
	if err != nil {
		fmt.Println("fatal:", err)
		os.Exit(1)
	}
	readCreds()
	fmt.Printf("%s starting, socks port %s, mode %q, auth %v\n", name, port, mode, creds.user != "")

	verb, arg, _ := strings.Cut(mode, ":")
	healthyUntil := time.Time{} // zero: forever
	var hangAfter time.Duration
	offlineFile := ""
	switch verb {
	case "crash-start":
		fmt.Println("fatal: config rejected by fake core")
		os.Exit(1)
	case "crash-start-once":
		if _, err := os.Stat(arg); err != nil {
			os.WriteFile(arg, nil, 0o600)
			fmt.Println("fatal: config rejected by fake core (first run)")
			os.Exit(1)
		}
	case "crash-after":
		d, _ := time.ParseDuration(arg)
		go func() {
			time.Sleep(d)
			fmt.Println("panic: simulated crash")
			os.Exit(2)
		}()
	case "hang-after":
		hangAfter, _ = time.ParseDuration(arg)
	case "offline-file":
		offlineFile = arg
	case "port-taken":
		// What mihomo prints when its port is taken, and it keeps running;
		// the fake then serves anyway, as whatever holds the port would.
		fmt.Printf("level=error msg=\"Listener socks-in listen err: listen tcp 127.0.0.1:%s: bind: address already in use\"\n", port)
	case "port-taken-after":
		d, _ := time.ParseDuration(arg)
		time.AfterFunc(d, func() {
			fmt.Printf("level=error msg=\"Listener socks-in listen err: listen tcp 127.0.0.1:%s: bind: address already in use\"\n", port)
		})
	case "eof-first":
		n, _ := strconv.Atoi(arg)
		eofLeft.Store(int32(n))
	case "unhealthy":
		healthyUntil = time.Now()
	case "unhealthy-after":
		d, _ := time.ParseDuration(arg)
		healthyUntil = time.Now().Add(d)
	}

	ln, err := net.Listen("tcp", "127.0.0.1:"+port)
	if err != nil {
		fmt.Println("fatal:", err)
		os.Exit(1)
	}
	if hangAfter > 0 {
		time.AfterFunc(hangAfter, func() { ln.Close() })
	}
	for {
		c, err := ln.Accept()
		if err != nil {
			if hangAfter > 0 {
				time.Sleep(time.Hour) // hung: alive, but deaf
			}
			return
		}
		healthy := healthyUntil.IsZero() || time.Now().Before(healthyUntil)
		if offlineFile != "" {
			if _, err := os.Stat(offlineFile); err == nil {
				healthy = false
			}
		}
		go serve(c, healthy)
	}
}

var portRE = []*regexp.Regexp{
	regexp.MustCompile(`"(?:port|listen_port)":\s*(\d+)`), // xray, sing-box: the inbound sorts first
	regexp.MustCompile(`socks-port:\s*(\d+)`),             // mihomo
	regexp.MustCompile(`(?m)^[\s-]*port:\s*(\d+)`),        // mihomo's listener sorts before the proxies
}

// credsRE find the inbound's username and password: xray's account, the
// sing-box inbound's user (inbounds sort before outbounds) and mihomo's
// listener user, keys in sorted order.
var credsRE = []*regexp.Regexp{
	regexp.MustCompile(`"pass":\s*"([^"]*)",\s*"user":\s*"([^"]*)"`),
	regexp.MustCompile(`"password":\s*"([^"]*)",\s*"username":\s*"([^"]*)"`),
	regexp.MustCompile(`password:\s*(\S+)\s*\n\s*username:\s*(\S+)`),
}

// creds are the user and password the inbound requires; empty for none.
var creds struct{ user, pass string }

func readCreds() {
	b, err := os.ReadFile(configPath())
	if err != nil {
		return
	}
	for _, re := range credsRE {
		if m := re.FindSubmatch(b); m != nil {
			creds.pass, creds.user = string(m[1]), string(m[2])
			return
		}
	}
}

var statsRE = regexp.MustCompile(`"?external[-_]controller"?\s*:\s*"?([0-9.]+:[0-9]+)`)

// serveStats imitates the Clash API's traffic totals.
func serveStats() {
	b, err := os.ReadFile(configPath())
	if err != nil {
		return
	}
	m := statsRE.FindSubmatch(b)
	if m == nil {
		return
	}
	calls := 0
	http.ListenAndServe(string(m[1]), http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		fmt.Fprintf(w, `{"uploadTotal":%d,"downloadTotal":%d,"connections":[]}`, calls*100, calls*1000)
	}))
}

func configPath() string {
	for i, a := range os.Args {
		if (a == "-c" || a == "-f") && i+1 < len(os.Args) {
			return os.Args[i+1]
		}
	}
	return ""
}

func configPort() (string, error) {
	path := configPath()
	b, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	for _, re := range portRE {
		if m := re.FindSubmatch(b); m != nil {
			return string(m[1]), nil
		}
	}
	return "", fmt.Errorf("no socks port in %s", path)
}

// authenticate requires the username and password (RFC 1929), as the
// real cores do once the config has credentials.
func authenticate(c net.Conn, r *bufio.Reader, methods []byte) bool {
	offered := false
	for _, m := range methods {
		offered = offered || m == 2
	}
	if !offered {
		c.Write([]byte{5, 0xff})
		return false
	}
	c.Write([]byte{5, 2})
	ver, err := r.ReadByte()
	if err != nil || ver != 1 {
		return false
	}
	read := func() (string, bool) {
		n, err := r.ReadByte()
		if err != nil {
			return "", false
		}
		b := make([]byte, n)
		if _, err := io.ReadFull(r, b); err != nil {
			return "", false
		}
		return string(b), true
	}
	user, ok1 := read()
	pass, ok2 := read()
	if !ok1 || !ok2 || user != creds.user || pass != creds.pass {
		fmt.Println("socks: authentication failed")
		c.Write([]byte{1, 1})
		return false
	}
	c.Write([]byte{1, 0})
	return true
}

// forwardTo is where FAKECORE_FORWARD ("zone=host:port") sends names in
// the zone, the way a real core would reach them; empty for other names.
func forwardTo(name string) string {
	zone, to, ok := strings.Cut(os.Getenv("FAKECORE_FORWARD"), "=")
	if !ok || (name != zone && !strings.HasSuffix(name, "."+zone)) {
		return ""
	}
	return to
}

// forward relays c, whose reader r may hold buffered bytes, to addr.
func forward(c net.Conn, r *bufio.Reader, addr string) {
	up, err := net.Dial("tcp", addr)
	if err != nil {
		return
	}
	defer up.Close()
	c.SetDeadline(time.Time{})
	done := make(chan struct{})
	go func() {
		io.Copy(up, r)
		up.(*net.TCPConn).CloseWrite()
		close(done)
	}()
	io.Copy(c, up)
	<-done
}

// eofLeft is how many more health requests eof-first leaves unanswered.
var eofLeft atomic.Int32

// serve speaks just enough SOCKS5 for a CONNECT, then answers one HTTP
// request, or relays names in FAKECORE_FORWARD's zone.
func serve(c net.Conn, healthy bool) {
	defer c.Close()
	c.SetDeadline(time.Now().Add(5 * time.Second))
	r := bufio.NewReader(c)
	hdr := make([]byte, 2)
	if _, err := io.ReadFull(r, hdr); err != nil {
		return
	}
	methods := make([]byte, hdr[1])
	if _, err := io.ReadFull(r, methods); err != nil {
		return
	}
	if creds.user == "" {
		c.Write([]byte{5, 0})
	} else if !authenticate(c, r, methods) {
		return
	}
	req := make([]byte, 4)
	if _, err := io.ReadFull(r, req); err != nil {
		return
	}
	var addrLen int
	switch req[3] {
	case 1:
		addrLen = 4
	case 4:
		addrLen = 16
	case 3:
		l, err := r.ReadByte()
		if err != nil {
			return
		}
		addrLen = int(l)
	}
	target := make([]byte, addrLen+2)
	if _, err := io.ReadFull(r, target); err != nil {
		return
	}
	reply := []byte{5, 0, 0, 1, 0, 0, 0, 0, 0, 0}
	binary.BigEndian.PutUint16(reply[8:], 0)
	c.Write(reply)

	// A name, as clients send it to have the server resolve it.
	if req[3] == 3 {
		if to := forwardTo(string(target[:addrLen])); to != "" {
			forward(c, r, to)
			return
		}
	}

	if _, err := http.ReadRequest(r); err != nil {
		return
	}
	if eofLeft.Add(-1) >= 0 {
		return // closed unanswered: the client reads EOF
	}
	if healthy {
		io.WriteString(c, "HTTP/1.1 204 No Content\r\nConnection: close\r\n\r\n")
	} else {
		io.WriteString(c, "HTTP/1.1 503 Service Unavailable\r\nContent-Length: 0\r\nConnection: close\r\n\r\n")
	}
}
