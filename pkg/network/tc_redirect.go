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
	"strings"

	"github.com/vishvananda/netlink"
	"golang.org/x/sys/unix"
)

// redirectClassifiers lists the tc classifiers the redirect filters are
// tried with, in order. The first one the kernel accepts is used for both
// directions.
var redirectClassifiers = []string{"u32"}

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

// netlinkRedirectFilterAdd installs the redirect filter through netlink.
func netlinkRedirectFilterAdd(kind string, source, target netlink.Link) error {
	attrs := redirectFilterAttrs(source)
	actions := redirectActions(target)
	switch kind {
	case "u32":
		return netlink.FilterAdd(&netlink.U32{
			FilterAttrs: attrs,
			Actions:     actions,
		})
	default:
		return fmt.Errorf("unknown tc classifier %q", kind)
	}
}
