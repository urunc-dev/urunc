//go:build linux

// Copyright (c) 2023-2026, Nubificus LTD
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package unikontainers

import (
	"errors"
	"net"
	"os"
	"path/filepath"
	"testing"

	"github.com/opencontainers/runtime-spec/specs-go"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/urunc-dev/urunc/internal/constants"
	"github.com/urunc-dev/urunc/pkg/network"
	"github.com/urunc-dev/urunc/pkg/unikontainers/types"
)

func TestGatewayNet(t *testing.T) {
	sock := filepath.Join(t.TempDir(), "gw.sock")
	l, err := net.Listen("unix", sock)
	require.NoError(t, err)
	t.Cleanup(func() { _ = l.Close() })

	u := &Unikontainer{State: &specs.State{ID: "abc123", Annotations: map[string]string{
		annotNetSocket:  sock,
		annotNetAddress: "10.87.0.10/24",
		annotNetGateway: "10.87.0.1",
	}}}
	got, err := u.gatewayNet()
	require.NoError(t, err)
	require.NotNil(t, got)
	assert.Equal(t, types.NetDevParams{
		IP:         "10.87.0.10",
		Mask:       "255.255.255.0",
		Gateway:    "10.87.0.1",
		MAC:        gatewayMAC("abc123"),
		UnixSocket: constants.NetGatewaySocketPath,
	}, *got)

	// Without the annotation there is no gateway.
	none := &Unikontainer{State: &specs.State{ID: "abc123", Annotations: map[string]string{}}}
	got, err = none.gatewayNet()
	assert.NoError(t, err)
	assert.Nil(t, got)

	// hvi would boot a guest on a missing socket with no way out, so the
	// create fails, and on a path that is no socket as well.
	u.State.Annotations[annotNetSocket] = filepath.Join(t.TempDir(), "missing.sock")
	_, err = u.gatewayNet()
	assert.ErrorIs(t, err, os.ErrNotExist)
	file := filepath.Join(t.TempDir(), "file")
	require.NoError(t, os.WriteFile(file, nil, 0o600))
	u.State.Annotations[annotNetSocket] = file
	_, err = u.gatewayNet()
	assert.ErrorContains(t, err, "is not a socket")
}

// Every guest on a gateway gets a MAC of its own, locally administered and
// unicast, and the same one for the same container.
func TestGatewayMAC(t *testing.T) {
	a, b := gatewayMAC("abc123"), gatewayMAC("abc124")
	assert.NotEqual(t, a, b)
	assert.Equal(t, a, gatewayMAC("abc123"))
	for _, s := range []string{a, b} {
		mac, err := net.ParseMAC(s)
		require.NoError(t, err)
		assert.Equal(t, byte(0x02), mac[0]&0x03, "%s must be locally administered and unicast", s)
	}
}

// guestNet hands back a gateway's network as it is: it sets nothing up in
// the network namespace, and only warns about an interface it finds there.
func TestGuestNetOnAGateway(t *testing.T) {
	stubNetworkManager(t, nil, errors.New("the network manager must not be used on a gateway"))
	orig := containerIface
	t.Cleanup(func() { containerIface = orig })
	looked := false
	containerIface = func() (network.Interface, error) {
		looked = true
		return network.Interface{Interface: "eth0", IP: "10.44.0.5"}, nil
	}

	gw := &types.NetDevParams{IP: "10.87.0.10", UnixSocket: constants.NetGatewaySocketPath}
	got, err := guestNet(gw, true, "static", 0, 0)
	require.NoError(t, err)
	assert.Equal(t, *gw, got)
	assert.True(t, looked, "guestNet should look for an interface to warn about")

	// Without a gateway, the network manager sets the network up.
	_, err = guestNet(nil, true, "static", 0, 0)
	assert.ErrorContains(t, err, "must not be used on a gateway")
}

// A socket's place taken by a symlink is refused at create, and so, at
// mount time, is anything that is not a socket, the symlink included,
// before anything is mounted.
func TestGatewaySocketRefusesASymlink(t *testing.T) {
	dir := t.TempDir()
	sock := filepath.Join(dir, "gw.sock")
	l, err := net.Listen("unix", sock)
	require.NoError(t, err)
	t.Cleanup(func() { _ = l.Close() })
	link := filepath.Join(dir, "link.sock")
	require.NoError(t, os.Symlink(sock, link))

	u := &Unikontainer{State: &specs.State{ID: "abc123", Annotations: map[string]string{
		annotNetSocket:  link,
		annotNetAddress: "10.87.0.10/24",
		annotNetGateway: "10.87.0.1",
	}}}
	_, err = u.gatewayNet()
	assert.ErrorContains(t, err, "is not a socket")

	monRootfs := t.TempDir()
	assert.ErrorContains(t, mountGatewaySocket(monRootfs, link), "is not a socket")
	file := filepath.Join(dir, "file")
	require.NoError(t, os.WriteFile(file, nil, 0o600))
	assert.ErrorContains(t, mountGatewaySocket(monRootfs, file), "is not a socket")
	assert.ErrorIs(t, mountGatewaySocket(monRootfs, filepath.Join(dir, "missing.sock")), os.ErrNotExist)
	_, err = os.Stat(filepath.Join(monRootfs, constants.NetGatewaySocketPath))
	assert.ErrorIs(t, err, os.ErrNotExist, "nothing is mounted for a refused socket")
}

// The monitor spec of both launch paths carries the gateway's network, and
// libcontainer's spec mounts the socket read-only at the path hvi is given.
func TestMonitorSpecsCarryTheGateway(t *testing.T) {
	gw := &types.NetDevParams{IP: "10.87.0.10", UnixSocket: constants.NetGatewaySocketPath}
	monRes := monitorResources{GatewayNet: gw, GatewaySocket: "/run/user/1000/gw.sock.qemu"}

	u, rootfsParams := newSpecUnikontainer(t, t.TempDir())
	got := u.buildMonitorSpec(rootfsParams, monRes)
	assert.Equal(t, gw, got.GatewayNet)
	assert.Nil(t, u.buildMonitorSpec(rootfsParams, monitorResources{}).GatewayNet)

	monSpec := monitorOCISpec(u.Spec, monRes, rootfsParams)
	want := specs.Mount{
		Type:        "bind",
		Source:      "/run/user/1000/gw.sock.qemu",
		Destination: constants.NetGatewaySocketPath,
		Options:     []string{"bind", "private", "ro", "nosuid", "nodev", "noexec"},
	}
	assert.Contains(t, monSpec.Mounts, want)
	assert.NotContains(t, monitorOCISpec(u.Spec, monitorResources{}, rootfsParams).Mounts, want)
}

// A monitor without a tap gets neither the tun device nor the capability a
// tap needs, which is what InitialSetup asks for when the guest is on a
// gateway.
func TestNoTapNoTunNoNetAdmin(t *testing.T) {
	devs, err := getMonitorDevices(false, false)
	require.NoError(t, err)
	for _, d := range devs {
		assert.NotEqual(t, "/dev/net/tun", d.Path)
	}
	caps := monitorCapabilities(&specs.Spec{Process: &specs.Process{Capabilities: &specs.LinuxCapabilities{
		Bounding: []string{"CAP_CHOWN"}, Effective: []string{"CAP_CHOWN"}, Permitted: []string{"CAP_CHOWN"},
	}}}, false)
	require.NotNil(t, caps)
	assert.NotContains(t, caps.Ambient, "CAP_NET_ADMIN")
	assert.NotContains(t, caps.Bounding, "CAP_NET_ADMIN")
}
