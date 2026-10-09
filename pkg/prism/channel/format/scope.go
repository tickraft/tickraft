// Copyright © 2026 Beijing Zhichuan Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

package format

import (
	"net/url"
	"strings"

	"github.com/tickraft/tickraft/pkg/prism/alert/template"
)

// Network scope aliases mirroring the template package constants (M5
// private-deployment alerting). They are re-exported so channel adapters
// and composition roots can reference one package.
const (
	// ScopeIntranet renders the full form for intranet channels.
	ScopeIntranet = template.ScopeIntranet
	// ScopeExtranet renders the lightweight, masked form for public
	// channels.
	ScopeExtranet = template.ScopeExtranet
)

// Masker masks sensitive content (intranet IPs, hostnames, custom patterns)
// from a rendered Message before it leaves the deployment. The kernel
// defines the seam only; deployments inject the rule set.
type Masker interface {
	// Mask returns msg with sensitive content replaced. Implementations
	// must be safe for concurrent use.
	Mask(msg Message) Message
}

// MaskFunc adapts a plain function to the Masker interface.
type MaskFunc func(Message) Message

// Mask implements Masker.
func (f MaskFunc) Mask(msg Message) Message { return f(msg) }

// LinkAdapter adapts a resource link for extranet delivery — typically
// rebasing it onto the public (DMZ) domain and appending a time-limited
// signature. ok reports whether the link was adapted; when false the
// caller keeps the original intranet link and annotates it instead.
type LinkAdapter func(link string) (adapted string, ok bool)

// ScopeOptions carries the deployment-wide network-scope rendering policy
// (M5 L1: dual templates, dual domains, content masking). The zero value
// disables every adaptation: full rendering, no masking, original links.
type ScopeOptions struct {
	// NetworkScope is the network environment rendered messages target:
	// empty (policy off), ScopeIntranet, or ScopeExtranet. Extranet
	// triggers masking, link adaptation, and the scoped template variant;
	// intranet only selects the intranet template variant.
	NetworkScope string
	// ExtranetBaseURL is the optional public (DMZ) base URL resource links
	// are rebased onto when rendering for ScopeExtranet and no LinkAdapter
	// applies. The rebased link carries no signature.
	ExtranetBaseURL string
	// LinkAdapter, when non-nil, produces the final public link for
	// ScopeExtranet rendering (rebase plus time-limited signature). It
	// wins over ExtranetBaseURL.
	LinkAdapter LinkAdapter
	// Masker, when non-nil, masks sensitive content from messages rendered
	// for ScopeExtranet.
	Masker Masker
}

// Enabled reports whether a scope policy is active (a concrete
// intranet/extranet scope is set).
func (s ScopeOptions) Enabled() bool {
	return s.NetworkScope == ScopeIntranet || s.NetworkScope == ScopeExtranet
}

// applyScope post-processes a rendered Message under the scope policy:
// extranet rendering masks the content first and then adapts the asset
// link, intranet rendering returns the message unchanged. It mutates and
// returns msg.
func applyScope(msg Message, opts RenderOptions) Message {
	if opts.Scope.NetworkScope != ScopeExtranet {
		return msg
	}
	if opts.Scope.Masker != nil {
		msg = opts.Scope.Masker.Mask(msg)
	}
	if msg.AssetLink != "" {
		link, hint := ExtranetLink(msg.AssetLink, opts.Scope)
		msg.AssetLink = link
		if hint {
			msg.LinkNote = IntranetLinkHint(opts.Registry, msg.Locale)
		}
	}
	return msg
}

// ExtranetLink adapts link for extranet delivery: the LinkAdapter first,
// then an unsigned rebase onto ExtranetBaseURL, and finally the original
// intranet link with hint=true so callers annotate it as intranet-only
// (no public domain configured).
func ExtranetLink(link string, scope ScopeOptions) (adapted string, hint bool) {
	if scope.LinkAdapter != nil {
		if out, ok := scope.LinkAdapter(link); ok {
			return out, false
		}
	}
	if base := strings.TrimRight(scope.ExtranetBaseURL, "/"); base != "" {
		if out, ok := rebaseLink(link, base); ok {
			return out, false
		}
	}
	return link, true
}

// rebaseLink replaces the scheme and host of link with base, preserving
// the path and query. Relative links starting with "/" are appended to
// base directly. It reports ok=false when link cannot be parsed or is
// neither absolute nor root-relative.
func rebaseLink(link, base string) (string, bool) {
	if strings.HasPrefix(link, "/") {
		return base + link, true
	}
	u, err := url.Parse(link)
	if err != nil || !u.IsAbs() {
		return "", false
	}
	out := base + u.RequestURI()
	if u.Fragment != "" {
		out += "#" + u.Fragment
	}
	return out, true
}
