---
layout: default
title: "Network"
description: "Network handling in urunc"
---

# Network Handling in Urunc

## Overview

Due to urunc's execution model, where unikernels run inside Virtual Machines (VMs) or sandboxes, special network handling is required to connect the VM's virtual network interface to the container's network namespace. urunc achieves this by creating a TAP (Test Access Point) device for each VM and bridging it with the virtual ethernet interface created by the Container Network Interface (CNI) plugin.

## Network Architecture

When a unikernel container is created, urunc operates within a network namespace that has been configured by CNI. This namespace typically contains a virtual ethernet interface (commonly named `eth0`) that provides connectivity to the pod's network. However, since the unikernel runs inside a VM, it cannot directly use this interface. Instead, urunc creates a TAP device that acts as a virtual network interface for the VM.

### Components

1. **CNI-created virtual ethernet interface (`eth0`)**: This is the interface created by the CNI plugin in the network namespace, providing the container's network connectivity.

2. **TAP device (`tapX_urunc`)**: A Layer 2 (L2) virtual network interface created by urunc for each VM. The TAP device serves as the network interface for the VM running the unikernel.

3. **Traffic Control (TC) rules**: Linux Traffic Control rules used to bridge traffic between the TAP device and the CNI-created interface.

## Network Modes

urunc supports two network modes, each designed for different deployment scenarios:

### Dynamic Network Mode

Dynamic network mode is the default mode used for standard container deployments. In this mode:

- A TAP device is created in the network namespace
- TC ingress qdiscs are added to both the TAP device and the `eth0` interface
- Bidirectional redirect filters are configured using TC rules to bridge traffic between the TAP device and `eth0`
- No static IP address is assigned to the TAP device
- The unikernel uses the same network configuration as `eth0` (IP address, gateway, subnet mask)

#### How it works

1. urunc creates a TAP device (e.g., `tap0_urunc`) with the same MTU as `eth0`
2. Both interfaces are brought up
3. An ingress qdisc is added to both the TAP device and `eth0`
4. Two redirect filters are created:
   - From TAP device to `eth0`: Redirects traffic from the VM to the network namespace
   - From `eth0` to TAP device: Redirects traffic from the network namespace to the VM

The TC rules use `U32` filters with `MirredAction` to redirect all traffic (`ETH_P_ALL` protocol) between the two interfaces, effectively creating a transparent bridge.

#### Limitations

Currently, dynamic network mode supports only one unikernel per network namespace. This limitation exists because multiple TAP devices in the same namespace would require more sophisticated routing logic to determine which TAP device should receive incoming traffic.

### Static Network Mode

Static network mode is designed for deployments where you need to avoid intercepting network traffic to sidecar containers, such as in Knative deployments with queue proxy sidecars.

In this mode:

- A TAP device is created with a static IP address (`172.16.1.1/24` by default)
- **No TC rules are applied** - the TAP device and `eth0` are not bridged via TC
- NAT (Network Address Translation) rules are configured using iptables
- IP forwarding is enabled in the kernel
- The unikernel is configured with a different IP address (`172.16.1.2` by default) and uses the TAP device's IP as its gateway

#### How it works

1. urunc creates a TAP device (e.g., `tap0_urunc`) and assigns it a static IP address (`172.16.1.1/24`)
2. IP forwarding is enabled by writing `1` to `/proc/sys/net/ipv4/ip_forward`
3. An iptables NAT rule is added:
   ```
   iptables -t nat -A POSTROUTING -o eth0 -s 172.16.1.1/24 -j MASQUERADE
   ```
4. The unikernel is configured with:
   - IP address: `172.16.1.2`
   - Gateway: `172.16.1.1` (the TAP device's IP)
   - Subnet mask: `255.255.255.0`

This setup allows the unikernel to communicate with the outside world through NAT, while sidecar containers (like Knative's queue proxy) can continue to use `eth0` directly without their traffic being intercepted by the TAP device.

#### Use Case: Knative

In Knative deployments, each pod typically contains:
- A user container (the application)
- A queue proxy sidecar container

The queue proxy needs to intercept and manage traffic to the user container. However, if urunc were to use dynamic network mode with TC rules, all traffic to `eth0` would be redirected to the TAP device, breaking the queue proxy's functionality.

By using static network mode:
- The queue proxy sidecar continues to use `eth0` normally
- The unikernel (user container) uses the TAP device with a separate IP address
- Traffic routing is handled by the kernel's IP forwarding and NAT, allowing both to coexist

## Network Setup Process

The network setup process in urunc follows these steps:

1. **Network Type Detection**: urunc determines the network mode based on container annotations. If the container name annotation (`io.kubernetes.cri.container-name`) is `"user-container"`, static mode is used; otherwise, dynamic mode is used.

2. **Interface Discovery**: urunc locates the `eth0` interface in the current network namespace.

3. **TAP Device Creation**: A TAP device is created with:
   - Name: `tapX_urunc` (where X is an index, typically 0)
   - Mode: TAP (Layer 2)
   - Single queue (required for some VMMs like Firecracker)
   - VNET header support (for virtio_net compatibility)
   - MTU matching the `eth0` interface
   - Ownership set to the container's UID/GID

4. **Interface Configuration**:
   - **Dynamic mode**: TC rules are applied to bridge TAP and `eth0`
   - **Static mode**: Static IP is assigned to TAP, NAT rules are configured

5. **Network Information**: urunc collects network configuration (IP, gateway, mask, MAC) and passes it to the VMM to configure the VM's network interface.

## Technical Details

### TAP Device Creation

The TAP device is created using Linux's TUN/TAP interface with the following characteristics:

- **Type**: TAP (Layer 2, Ethernet frames) as opposed to TUN (Layer 3, IP packets)
- **Queues**: Single queue (multiqueue not supported by all VMMs)
- **Flags**: 
  - `TUNTAP_ONE_QUEUE`: Single queue operation
  - `TUNTAP_VNET_HDR`: Enables parsing of vnet headers added by the VM's virtio_net implementation

### Traffic Control Rules

In dynamic mode, urunc uses Linux Traffic Control (TC) to create a transparent bridge:

1. **Ingress Qdisc**: Added to both TAP and `eth0` interfaces
   - Type: `ingress`
   - Parent: `HANDLE_INGRESS` (0xfffffff0)

2. **Redirect Filters**: U32 filters with MirredAction
   - Protocol: `ETH_P_ALL` (captures all Ethernet frames)
   - Action: `TCA_EGRESS_REDIR` (redirects to target interface)
   - Bidirectional: Two filters create a full-duplex bridge

### NAT Configuration

In static mode, urunc configures NAT using iptables:

- **Table**: `nat`
- **Chain**: `POSTROUTING`
- **Rule**: Masquerade traffic from the TAP device's subnet when exiting via `eth0`
- **IP Forwarding**: Enabled via `/proc/sys/net/ipv4/ip_forward`

## Network Cleanup

When a container is deleted, urunc performs network cleanup:

1. All TC filters are removed from both the TAP device and `eth0`
2. All qdiscs are removed from both interfaces
3. The TAP device is brought down and deleted

This ensures no network artifacts remain after container termination.

## Future Improvements

Several areas for improvement have been identified:

1. **Multiple Unikernels per Namespace**: Currently, only one unikernel per network namespace is supported in dynamic mode. Future work may enable multiple TAP devices with proper routing.

2. **Interface Discovery**: Currently, urunc assumes the CNI-created interface is named `eth0`. Future versions may dynamically discover the actual interface name.

3. **Network Type Configuration**: Currently, network type is determined by container annotations. Future versions may allow explicit configuration via annotations or configuration files.

