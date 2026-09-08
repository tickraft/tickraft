// Copyright © 2026 Beijing Ruishuo Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

package prism

import (
	"errors"
	"strings"
	"testing"

	"github.com/tickraft/tickraft/pkg/prism/channel"
	"github.com/tickraft/tickraft/pkg/prism/channel/httpclient"
)

// TestBuildChannelGlobalEgressProxy verifies the deployment-wide proxy in
// BuildOptions flows into the per-type HTTP client construction: a valid
// socks5 proxy builds cleanly, while an unsupported scheme surfaces the
// httpclient build error.
func TestBuildChannelGlobalEgressProxy(t *testing.T) {
	RegisterBuiltinChannelTypes()
	def := &channel.Channel{
		Type:   "dingtalk",
		Config: `{"webhook_url":"https://oapi.dingtalk.com/robot/send?access_token=tok"}`,
	}

	if _, err := BuildChannel(def, channel.BuildOptions{
		ProxyURL: "socks5://proxy.intranet.example.com:1080",
	}, nil); err != nil {
		t.Fatalf("build with valid global proxy: %v", err)
	}

	_, err := BuildChannel(def, channel.BuildOptions{
		ProxyURL: "ftp://proxy.example.com:21",
	}, nil)
	if !errors.Is(err, httpclient.ErrUnsupportedProxyScheme) {
		t.Errorf("build with invalid global proxy = %v, want ErrUnsupportedProxyScheme", err)
	}
}

// TestBuildChannelPerChannelProxyPrecedence verifies the per-channel
// proxy_url wins over the deployment-wide proxy: a valid per-channel
// proxy must override an invalid global one, and vice versa an invalid
// per-channel value must fail even when the global proxy is valid.
func TestBuildChannelPerChannelProxyPrecedence(t *testing.T) {
	RegisterBuiltinChannelTypes()

	ownValid := &channel.Channel{
		Type: "dingtalk",
		Config: `{"webhook_url":"https://oapi.dingtalk.com/robot/send?access_token=tok",` +
			`"proxy_url":"socks5://per-channel:1080"}`,
	}
	if _, err := BuildChannel(ownValid, channel.BuildOptions{
		ProxyURL: "ftp://global-invalid:21",
	}, nil); err != nil {
		t.Errorf("valid per-channel proxy should override invalid global: %v", err)
	}

	ownInvalid := &channel.Channel{
		Type: "dingtalk",
		Config: `{"webhook_url":"https://oapi.dingtalk.com/robot/send?access_token=tok",` +
			`"proxy_url":"ftp://per-channel:21"}`,
	}
	_, err := BuildChannel(ownInvalid, channel.BuildOptions{
		ProxyURL: "socks5://global-valid:1080",
	}, nil)
	if !errors.Is(err, httpclient.ErrUnsupportedProxyScheme) {
		t.Errorf("invalid per-channel proxy should fail despite valid global = %v, want ErrUnsupportedProxyScheme", err)
	}
}

// TestBuildChannelEmailGlobalProxy verifies the email build path applies
// the deployment-wide proxy under its socks5-only constraint.
func TestBuildChannelEmailGlobalProxy(t *testing.T) {
	RegisterBuiltinChannelTypes()
	def := &channel.Channel{
		Type:   "email",
		Config: `{"host":"smtp.example.com","port":25,"from":"alert@example.com","to":["ops@example.com"]}`,
	}

	if _, err := BuildChannel(def, channel.BuildOptions{
		ProxyURL: "socks5://proxy.intranet.example.com:1080",
	}, nil); err != nil {
		t.Fatalf("build with socks5 global proxy: %v", err)
	}

	_, err := BuildChannel(def, channel.BuildOptions{
		ProxyURL: "http://proxy.example.com:8080",
	}, nil)
	if err == nil || !strings.Contains(err.Error(), "socks5") {
		t.Errorf("build with http global proxy = %v, want socks5-only error", err)
	}
}

// TestFlatConfigProxyValidation verifies the flat-wire proxy_url is
// validated at the registry Validate seam for webhook and email, so API
// callers get scheme feedback before anything is persisted as buildable.
func TestFlatConfigProxyValidation(t *testing.T) {
	RegisterBuiltinChannelTypes()

	webhookInfo, ok := channel.LookupType("webhook")
	if !ok {
		t.Fatal("webhook type not registered")
	}
	emailInfo, ok := channel.LookupType("email")
	if !ok {
		t.Fatal("email type not registered")
	}

	if err := webhookInfo.Validate(`{"url":"https://example.com/hook","proxy_url":"socks5://p:1080"}`); err != nil {
		t.Errorf("webhook socks5 proxy_url should validate: %v", err)
	}
	if err := webhookInfo.Validate(`{"url":"https://example.com/hook","proxy_url":"ftp://p:21"}`); err == nil {
		t.Error("webhook ftp proxy_url should fail validation")
	}

	emailSocks5 := `{"host":"smtp.example.com","port":25,"from":"a@example.com",` +
		`"to":["ops@example.com"],"proxy_url":"socks5://p:1080"}`
	if err := emailInfo.Validate(emailSocks5); err != nil {
		t.Errorf("email socks5 proxy_url should validate: %v", err)
	}
	emailHTTP := `{"host":"smtp.example.com","port":25,"from":"a@example.com",` +
		`"to":["ops@example.com"],"proxy_url":"http://p:8080"}`
	err := emailInfo.Validate(emailHTTP)
	if err == nil || !strings.Contains(err.Error(), "socks5") {
		t.Errorf("email http proxy_url = %v, want socks5-only error", err)
	}
}

// TestEmailTypeRegistersSMTPPasswordSensitive verifies the email type
// declares the SMTP password as sensitive, so channel configs persist it
// encrypted at rest and mask it in API echoes. A regression here stores
// mail credentials in plaintext.
func TestEmailTypeRegistersSMTPPasswordSensitive(t *testing.T) {
	RegisterBuiltinChannelTypes()
	sensitive := channel.SensitiveKeys()
	if _, ok := sensitive["password"]; !ok {
		t.Error("SensitiveKeys union missing \"password\": SMTP credentials would be stored plaintext")
	}
}
