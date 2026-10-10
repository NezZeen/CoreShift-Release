package socksgate

import (
	"bufio"
	"net"
	"net/http"
	"time"
)

// The gate's answer to "is it you?", for the guard of the system proxy
// (sysproxy): a port that merely answers may be another program's, which
// took it while CoreShift was down, and the proxy of the system would
// then be left pointing at it. A request in origin form for ProbePath
// gets 204 with ProbeHeader; it carries nothing but that the port is a
// CoreShift gate, and no core is used.
const (
	ProbePath   = "/.coreshift/gate"
	ProbeHeader = "X-Coreshift-Gate"
)

// isProbe reports whether req asks ProbePath.
func isProbe(req *http.Request) bool {
	return req.Method == http.MethodGet && !req.URL.IsAbs() && req.URL.Path == ProbePath
}

// answerProbe answers a probe on c.
func answerProbe(c net.Conn) {
	c.Write([]byte("HTTP/1.1 204 No Content\r\n" + ProbeHeader + ": 1\r\nContent-Length: 0\r\nConnection: close\r\n\r\n"))
}

// IsGate reports whether a CoreShift gate serving HTTP listens at addr,
// host:port, answering within timeout.
func IsGate(addr string, timeout time.Duration) bool {
	c, err := net.DialTimeout("tcp", addr, timeout)
	if err != nil {
		return false
	}
	defer c.Close()
	c.SetDeadline(time.Now().Add(timeout))
	if _, err := c.Write([]byte("GET " + ProbePath + " HTTP/1.1\r\nHost: coreshift\r\nConnection: close\r\n\r\n")); err != nil {
		return false
	}
	resp, err := http.ReadResponse(bufio.NewReader(c), nil)
	if err != nil {
		return false
	}
	resp.Body.Close()
	return resp.StatusCode == http.StatusNoContent && resp.Header.Get(ProbeHeader) == "1"
}
