// Copyright © 2026 Beijing Zhichuan Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

package channel

// Config is the wire shape of the webhook and email channel config JSON
// stored in the sys_prism_channel rows of those two types. The generic
// channels carry a flat key set; every other channel type defines its own
// config shape (see the type's Validate and Build functions registered
// via RegisterType).
type Config struct {
	// URL is the target endpoint for webhook channels.
	URL string `json:"url"`
	// Timeout is the request timeout for webhook channels, parsed as a
	// time.Duration string (e.g. "10s").
	Timeout string `json:"timeout,omitempty"`
	// Headers carries custom HTTP headers for webhook channels.
	Headers map[string]string `json:"headers,omitempty"`
	// Host is the SMTP server hostname for email channels.
	Host string `json:"host,omitempty"`
	// Port is the SMTP server port for email channels.
	Port int `json:"port,omitempty"`
	// Username is the SMTP authentication username for email channels.
	Username string `json:"username,omitempty"`
	// Password is the SMTP authentication password for email channels.
	Password string `json:"password,omitempty"`
	// From is the sender address for email channels.
	From string `json:"from,omitempty"`
	// To is the list of recipient addresses for email channels.
	To []string `json:"to,omitempty"`
	// TLSMode selects the TLS mode for email channels: "none",
	// "implicit", or "starttls".
	TLSMode string `json:"tls_mode,omitempty"`
	// AuthType selects the SMTP auth type for email channels: "plain",
	// "login", or "cram-md5".
	AuthType string `json:"auth_type,omitempty"`
	// HTMLMode sends email bodies as HTML when true.
	HTMLMode bool `json:"html_mode,omitempty"`
	// ProxyURL is the optional per-channel outbound proxy for webhook and
	// email channels. Supported schemes are http, https, and socks5;
	// email supports socks5 only. When empty the deployment-wide egress
	// proxy applies (when configured).
	ProxyURL string `json:"proxy_url,omitempty"`
}
