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

package network

import (
	"errors"
	"fmt"
	"math"
	"strings"

	"github.com/vishvananda/netlink"
	"github.com/vishvananda/netlink/nl"
	"golang.org/x/sys/unix"
)

// tcaBasicAct is TCA_BASIC_ACT from linux/pkt_cls.h, the attribute that
// carries the actions of a basic filter.
const tcaBasicAct = 3

// redirectClassifiers lists the tc classifiers the redirect filters are
// tried with, in order. The first one the kernel accepts is used for both
// directions. u32 stays first, so nodes that have it keep the filter they
// always had.
//
// On RTM_NEWTFILTER the kernel loads cls_<kind> on demand, and answers ENOENT
// when that classifier does not exist. A kernel built without some of them
// still gets a redirect: on jetson6 (`# CONFIG_NET_CLS_U32 is not set`, no
// cls_matchall, cls_flower and cls_basic as modules) the path is u32 ->
// matchall -> flower.
var redirectClassifiers = []string{"u32", "matchall", "flower", "basic"}

// redirectFilterAddFunc installs, on the ingress of source, a filter of the
// given classifier kind that redirects every packet to the egress of target.
type redirectFilterAddFunc func(kind string, source, target netlink.Link) error

// redirectFilterAdd is the filter installer addRedirectFilter uses. Tests
// replace it to record the attempts and refuse chosen kinds.
var redirectFilterAdd redirectFilterAddFunc = netlinkRedirectFilterAdd

// addRedirectFilter installs a redirect filter from source to target with
// the first classifier in kinds that the kernel accepts, and returns that
// kind. When every kind is refused, the error names all of them and wraps
// each refusal.
func addRedirectFilter(source, target netlink.Link, kinds []string) (string, error) {
	if len(kinds) == 0 {
		return "", errors.New("no tc classifier configured")
	}
	src := source.Attrs().Name
	dst := target.Attrs().Name
	var errs []error
	for _, kind := range kinds {
		err := redirectFilterAdd(kind, source, target)
		if err == nil {
			netlog.Debugf("tc redirect %s->%s uses the %s classifier", src, dst, kind)
			return kind, nil
		}
		netlog.Warnf("tc classifier %s unavailable for %s->%s: %v", kind, src, dst, err)
		errs = append(errs, err)
	}
	return "", fmt.Errorf("no usable tc classifier for %s->%s (tried %s): %w",
		src, dst, strings.Join(kinds, ", "), errors.Join(errs...))
}

// addRedirectFilters redirects tap to eth and eth to tap. The tap->eth filter
// picks a classifier from kinds; the eth->tap filter uses the same one and
// tries nothing else, so both directions carry the same match and action.
func addRedirectFilters(tap, eth netlink.Link, kinds []string) (string, error) {
	kind, err := addRedirectFilter(tap, eth, kinds)
	if err != nil {
		return "", fmt.Errorf("addRedirectFilter(%s->%s) failed: %w",
			tap.Attrs().Name, eth.Attrs().Name, err)
	}
	if err = redirectFilterAdd(kind, eth, tap); err != nil {
		return "", fmt.Errorf("addRedirectFilter(%s->%s) failed: %w",
			eth.Attrs().Name, tap.Attrs().Name, err)
	}
	return kind, nil
}

// redirectFilterAttrs matches every protocol on the ingress qdisc of source.
func redirectFilterAttrs(source netlink.Link) netlink.FilterAttrs {
	return netlink.FilterAttrs{
		LinkIndex: source.Attrs().Index,
		Parent:    netlink.MakeHandle(0xffff, 0),
		Protocol:  unix.ETH_P_ALL,
	}
}

// redirectActions steals the packet and redirects it to the egress of
// target.
func redirectActions(target netlink.Link) []netlink.Action {
	return []netlink.Action{
		&netlink.MirredAction{
			ActionAttrs: netlink.ActionAttrs{
				Action: netlink.TC_ACT_STOLEN,
			},
			MirredAction: netlink.TCA_EGRESS_REDIR,
			Ifindex:      target.Attrs().Index,
		},
	}
}

// redirectFilter builds the typed filter of kind that matches every packet
// on the ingress of source and carries the redirect to target. Every kind
// has the same attributes and the same mirred action, so the packet path is
// the same whichever classifier the kernel accepts. basic has no typed
// netlink filter and is built by basicRedirectRequest.
func redirectFilter(kind string, source, target netlink.Link) (netlink.Filter, error) {
	attrs := redirectFilterAttrs(source)
	actions := redirectActions(target)
	switch kind {
	case "u32":
		return &netlink.U32{FilterAttrs: attrs, Actions: actions}, nil
	case "matchall":
		return &netlink.MatchAll{FilterAttrs: attrs, Actions: actions}, nil
	case "flower":
		// With no keys set, netlink only sends TCA_FLOWER_FLAGS and
		// TCA_FLOWER_ACT, and a flower filter without keys matches
		// everything.
		return &netlink.Flower{FilterAttrs: attrs, Actions: actions}, nil
	default:
		return nil, fmt.Errorf("unknown tc classifier %q", kind)
	}
}

// netlinkRedirectFilterAdd installs the redirect filter through netlink.
func netlinkRedirectFilterAdd(kind string, source, target netlink.Link) error {
	if kind == "basic" {
		req, err := basicRedirectRequest(redirectFilterAttrs(source), redirectActions(target))
		if err != nil {
			return err
		}
		_, err = req.Execute(unix.NETLINK_ROUTE, 0)
		return err
	}
	filter, err := redirectFilter(kind, source, target)
	if err != nil {
		return err
	}
	return netlink.FilterAdd(filter)
}

// tcIfindex converts a link index to the int32 a tc message carries.
func tcIfindex(index int) (int32, error) {
	if index < 0 || index > math.MaxInt32 {
		return 0, fmt.Errorf("interface index %d does not fit a tc ifindex", index)
	}
	return int32(index), nil
}

// basicRedirectRequest builds the RTM_NEWTFILTER request of a basic filter
// with no match rule, which matches every packet, carrying actions. netlink
// v1.3.1 has no typed basic filter, and its GenericFilter sends empty options
// and no action, so the request is built here the way FilterAdd builds the
// others.
func basicRedirectRequest(attrs netlink.FilterAttrs, actions []netlink.Action) (*nl.NetlinkRequest, error) {
	ifindex, err := tcIfindex(attrs.LinkIndex)
	if err != nil {
		return nil, err
	}
	req := nl.NewNetlinkRequest(unix.RTM_NEWTFILTER,
		unix.NLM_F_CREATE|unix.NLM_F_EXCL|unix.NLM_F_ACK)
	req.AddData(&nl.TcMsg{
		Family:  nl.FAMILY_ALL,
		Ifindex: ifindex,
		Parent:  attrs.Parent,
		Info:    netlink.MakeHandle(attrs.Priority, nl.Swap16(attrs.Protocol)),
	})
	req.AddData(nl.NewRtAttr(nl.TCA_KIND, nl.ZeroTerminated("basic")))
	options := nl.NewRtAttr(nl.TCA_OPTIONS, nil)
	if err := netlink.EncodeActions(options.AddRtAttr(tcaBasicAct, nil), actions); err != nil {
		return nil, err
	}
	req.AddData(options)
	return req, nil
}
