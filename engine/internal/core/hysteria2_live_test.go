package core

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"io"
	"math/big"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"coreshift/engine/internal/node"
)

// TestHysteria2Loopback runs real Hysteria2 servers (sing-box and Xray, each
// plain and with Salamander) on 127.0.0.1 with a self-signed certificate, and
// every core as a client of each through its SOCKS inbound: a download must
// get through, and a wrong password or certificate pin must not. Needs
// XRAY_BIN, SINGBOX_BIN and MIHOMO_BIN, like TestCoresAcceptConfigs; the
// cores whose binary is not set are skipped.
func TestHysteria2Loopback(t *testing.T) {
	bins := map[Kind]string{Xray: os.Getenv("XRAY_BIN"), SingBox: os.Getenv("SINGBOX_BIN"), Mihomo: os.Getenv("MIHOMO_BIN")}
	if bins[SingBox] == "" && bins[Xray] == "" {
		t.Skip("neither SINGBOX_BIN nor XRAY_BIN set: no Hysteria2 server to run")
	}
	const (
		size     = 256 << 10
		auth     = "hy2-auth-secret"
		obfsPass = "salamander-secret"
		sni      = "hy.test"
	)
	web := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(make([]byte, size))
	}))
	defer web.Close()
	dir := t.TempDir()
	certFile, keyFile, pin := selfSignedCert(t, dir, sni)

	type server struct {
		name string
		port uint16
		obfs bool
	}
	var servers []server
	if bin := bins[SingBox]; bin != "" {
		plain, obfs := freeUDPPort(t), freeUDPPort(t)
		tls := obj{"enabled": true, "alpn": []string{"h3"}, "certificate_path": certFile, "key_path": keyFile}
		users := []any{obj{"password": auth}}
		startServer(t, bin, dir, "sing-box-server.json", obj{
			"log": obj{"level": "warn"},
			"inbounds": []any{
				obj{"type": "hysteria2", "listen": "127.0.0.1", "listen_port": plain, "users": users, "tls": tls},
				obj{"type": "hysteria2", "listen": "127.0.0.1", "listen_port": obfs, "users": users, "tls": tls,
					"obfs": obj{"type": "salamander", "password": obfsPass}},
			},
			"outbounds": []any{obj{"type": "direct"}},
		}, func(path string) []string { return []string{"run", "-c", path, "-D", dir} })
		servers = append(servers, server{"sing-box", plain, false}, server{"sing-box+salamander", obfs, true})
	}
	if bin := bins[Xray]; bin != "" {
		// The server side of Remnawave's Hysteria template.
		plain, obfs := freeUDPPort(t), freeUDPPort(t)
		inbound := func(port uint16, mask obj) obj {
			ss := obj{
				"network": "hysteria", "security": "tls", "hysteriaSettings": obj{"version": 2},
				"tlsSettings": obj{"alpn": []string{"h3"}, "certificates": []any{obj{"certificateFile": certFile, "keyFile": keyFile}}},
			}
			if mask != nil {
				ss["finalmask"] = mask
			}
			return obj{"listen": "127.0.0.1", "port": port, "protocol": "hysteria",
				"settings": obj{"version": 2, "clients": []any{obj{"auth": auth}}}, "streamSettings": ss}
		}
		startServer(t, bin, dir, "xray-server.json", obj{
			"log": obj{"loglevel": "warning"},
			"inbounds": []any{
				inbound(plain, nil),
				inbound(obfs, obj{"udp": []any{obj{"type": "salamander", "settings": obj{"password": obfsPass}}}}),
			},
			"outbounds": []any{obj{"protocol": "freedom"}},
		}, func(path string) []string { return []string{"run", "-c", path} })
		servers = append(servers, server{"xray", plain, false}, server{"xray+salamander", obfs, true})
	}

	// client is the node for a server, with the certificate pinned where the
	// core can pin it and unchecked where it cannot.
	client := func(a Adapter, s server) node.Node {
		n := node.Node{Name: "loopback", Protocol: node.Hysteria2, Server: "127.0.0.1", Port: s.port, Password: auth,
			TLS: &node.TLS{ServerName: sni, ALPN: []string{"h3"}, PinSHA256: pin}}
		if s.obfs {
			n.Hysteria2 = &node.Hysteria2Options{Obfs: "salamander", ObfsPassword: obfsPass, UpMbps: 100, DownMbps: 100}
		}
		if a.Supports(&n) != nil {
			n.TLS.PinSHA256, n.TLS.Insecure = "", true
		}
		return n
	}
	for _, a := range Adapters() {
		bin := bins[a.Kind()]
		t.Run(string(a.Kind()), func(t *testing.T) {
			if bin == "" {
				t.Skip("binary not set")
			}
			for _, s := range servers {
				t.Run(s.name, func(t *testing.T) {
					n := client(a, s)
					if err := fetchThrough(t, a, bin, n, web.URL, 10*time.Second); err != nil {
						t.Fatal(err)
					}
				})
			}
			s := servers[0]
			t.Run(s.name+"/port-hopping", func(t *testing.T) {
				n := client(a, s)
				n.Hysteria2 = &node.Hysteria2Options{Ports: fmt.Sprintf("%d-%d", s.port, s.port)}
				if err := a.Supports(&n); err != nil {
					t.Skip(err)
				}
				if err := fetchThrough(t, a, bin, n, web.URL, 10*time.Second); err != nil {
					t.Fatal(err)
				}
			})
			t.Run(s.name+"/wrong-auth", func(t *testing.T) {
				n := client(a, s)
				n.Password = "wrong"
				if err := fetchThrough(t, a, bin, n, web.URL, 3*time.Second); err == nil {
					t.Fatal("the server took a wrong password")
				}
			})
			t.Run(s.name+"/wrong-pin", func(t *testing.T) {
				n := client(a, s)
				if n.TLS.PinSHA256 == "" {
					t.Skip("this core does not pin certificates")
				}
				n.TLS.PinSHA256 = hex.EncodeToString(make([]byte, 32))
				if err := fetchThrough(t, a, bin, n, web.URL, 3*time.Second); err == nil {
					t.Fatal("a certificate that does not match the pin was accepted")
				}
			})
		})
	}
}

// fetchThrough runs the core as a client for n and downloads url through
// it, retrying until the deadline: the servers' UDP ports cannot be polled.
func fetchThrough(t *testing.T, a Adapter, bin string, n node.Node, url string, within time.Duration) error {
	t.Helper()
	dir := t.TempDir()
	o := Options{Listen: freePort(t), Auth: NewSOCKSAuth(), LogLevel: "debug"}
	cfg, err := a.Render(&n, o)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, a.ConfigName())
	if err := os.WriteFile(path, cfg, 0o600); err != nil {
		t.Fatal(err)
	}
	startCore(t, bin, a.RunArgs(path, dir), dir)
	waitPort(t, o.Listen)
	client := &http.Client{Timeout: 3 * time.Second, Transport: &http.Transport{Proxy: http.ProxyURL(o.Auth.ProxyURL(o.Listen))}}
	deadline := time.Now().Add(within)
	for {
		resp, err := client.Get(url)
		if err == nil {
			got, _ := io.Copy(io.Discard, resp.Body)
			resp.Body.Close()
			if resp.StatusCode == http.StatusOK && got > 0 {
				return nil
			}
			err = fmt.Errorf("status %d, %d bytes", resp.StatusCode, got)
		}
		if time.Now().After(deadline) {
			return err
		}
		time.Sleep(300 * time.Millisecond)
	}
}

func startServer(t *testing.T, bin, dir, name string, cfg obj, args func(path string) []string) {
	t.Helper()
	b, err := json.Marshal(cfg)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, b, 0o600); err != nil {
		t.Fatal(err)
	}
	startCore(t, bin, args(path), dir)
}

func freeUDPPort(t *testing.T) uint16 {
	t.Helper()
	c, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	return uint16(c.LocalAddr().(*net.UDPAddr).Port)
}

// selfSignedCert writes a certificate for name and its key, and returns
// their paths and the certificate's SHA-256, as a panel pins it.
func selfSignedCert(t *testing.T, dir, name string) (certFile, keyFile, pin string) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: name},
		DNSNames:     []string{name},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(24 * time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	keyDER, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		t.Fatal(err)
	}
	certFile, keyFile = filepath.Join(dir, "cert.pem"), filepath.Join(dir, "key.pem")
	if err := os.WriteFile(certFile, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(keyFile, pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER}), 0o600); err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(der)
	return certFile, keyFile, hex.EncodeToString(sum[:])
}
