#!/bin/sh
# Runs TestRefuseIPv6Live (refuse_live_linux_test.go) in a pair of throwaway
# network namespaces, as root:
#
#   cd engine && GOOS=linux go test -c -o /tmp/tunlayer.test ./internal/tunlayer
#   sudo sh internal/tunlayer/testdata/refuse-ipv6-netns.sh /tmp/tunlayer.test /path/to/sing-box
#
# "csr6a" is the machine under test: IPv4, and IPv6 with a default route
# through "csr6b", which plays both a host on the local network
# (fd00:1::2) and one on the internet (2001:db8:2::2, behind the route),
# listening on port 8080. Nothing outside the two namespaces is touched.
set -eu
test_bin=$1
singbox=$2
A="ip netns exec csr6a"
B="ip netns exec csr6b"

cleanup() {
	[ -n "${server:-}" ] && kill "$server" 2>/dev/null || true
	ip netns del csr6a 2>/dev/null || true
	ip netns del csr6b 2>/dev/null || true
}
trap cleanup EXIT INT TERM
cleanup

ip netns add csr6a
ip netns add csr6b
ip link add csr6va netns csr6a type veth peer name csr6vb netns csr6b
$A ip link set lo up
$B ip link set lo up
$A ip link set csr6va up
$B ip link set csr6vb up
$A ip addr add 10.200.0.1/24 dev csr6va
$B ip addr add 10.200.0.2/24 dev csr6vb
$A ip route add default via 10.200.0.2
$A ip -6 addr add fd00:1::1/64 dev csr6va nodad
$B ip -6 addr add fd00:1::2/64 dev csr6vb nodad
$B ip -6 addr add 2001:db8:2::2/128 dev lo
$A ip -6 route add default via fd00:1::2 dev csr6va

$B python3 -m http.server --bind :: 8080 >/dev/null 2>&1 &
server=$!
sleep 1

$A env SINGBOX_BIN="$singbox" REFUSE_GLOBAL6='[2001:db8:2::2]:8080' REFUSE_LAN6='[fd00:1::2]:8080' \
	"$test_bin" -test.run 'TestRefuseIPv6Live' -test.v -test.count=1
