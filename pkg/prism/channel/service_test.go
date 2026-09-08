// Copyright © 2026 Beijing Ruishuo Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

package channel

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/tickraft/tickraft/pkg/prism/alert"
)

// probeSender is a fake alert.Channel whose Send outcome is fixed per
// instance.
type probeSender struct {
	name string
	err  error
}

func (s *probeSender) Name() string { return s.name }

func (s *probeSender) Send(ctx context.Context, evt alert.Event) error { return s.err }

// probeRuntime is a fake Runtime that fails builds for selected channel
// IDs and hands out probeSenders for the rest.
type probeRuntime struct {
	buildErrs map[int64]error
	senders   map[int64]*probeSender
}

func (r *probeRuntime) ReloadChannels(ctx context.Context) error { return nil }

func (r *probeRuntime) BuildChannel(ch *Channel) (alert.Channel, error) {
	if err, ok := r.buildErrs[ch.ID]; ok {
		return nil, err
	}
	if s, ok := r.senders[ch.ID]; ok {
		return s, nil
	}
	return &probeSender{name: ch.Name}, nil
}

// TestChannelServiceTestAllChannels verifies the batch connectivity probe:
// every enabled channel gets one result (disabled ones are skipped), build
// failures surface as OK=false with the error, and successful sends report
// OK=true with the channel identity attached.
func TestChannelServiceTestAllChannels(t *testing.T) {
	gdb := newStoreTestDB(t)
	rt := &probeRuntime{
		buildErrs: map[int64]error{},
		senders:   map[int64]*probeSender{},
	}
	svc := NewChannelService(NewStore(gdb, nil), NewDeliveryStore(gdb), rt)
	ctx := context.Background()

	okCh, err := svc.CreateChannel(ctx, &CreateRequest{
		Name:    "primary",
		Type:    "feishu",
		Config:  json.RawMessage(`{"webhook_url":"https://hook.example/ok"}`),
		Enabled: boolPtr(true),
	})
	if err != nil {
		t.Fatalf("create ok channel: %v", err)
	}
	rt.senders[okCh.ID] = &probeSender{name: "primary"}

	sendFailCh, err := svc.CreateChannel(ctx, &CreateRequest{
		Name:    "flaky",
		Type:    "feishu",
		Config:  json.RawMessage(`{"webhook_url":"https://hook.example/flaky"}`),
		Enabled: boolPtr(true),
	})
	if err != nil {
		t.Fatalf("create flaky channel: %v", err)
	}
	rt.senders[sendFailCh.ID] = &probeSender{name: "flaky", err: errors.New("dial tcp: i/o timeout")}

	buildFailCh, err := svc.CreateChannel(ctx, &CreateRequest{
		Name:    "broken",
		Type:    "feishu",
		Config:  json.RawMessage(`{"webhook_url":"https://hook.example/broken"}`),
		Enabled: boolPtr(true),
	})
	if err != nil {
		t.Fatalf("create broken channel: %v", err)
	}
	rt.buildErrs[buildFailCh.ID] = errors.New(`feishu: build http client: unsupported proxy scheme "ftp"`)

	disabledCh, err := svc.CreateChannel(ctx, &CreateRequest{
		Name:    "off",
		Type:    "feishu",
		Config:  json.RawMessage(`{"webhook_url":"https://hook.example/off"}`),
		Enabled: boolPtr(false),
	})
	if err != nil {
		t.Fatalf("create disabled channel: %v", err)
	}

	results, err := svc.TestAllChannels(ctx)
	if err != nil {
		t.Fatalf("TestAllChannels: %v", err)
	}
	if len(results) != 3 {
		t.Fatalf("results: got %d, want 3 (disabled channels must be skipped)", len(results))
	}
	byID := make(map[int64]TestResult, len(results))
	for _, res := range results {
		byID[res.ChannelID] = res
	}

	res, ok := byID[okCh.ID]
	if !ok || !res.OK || res.Error != "" || res.Name != "primary" || res.Type != "feishu" {
		t.Errorf("ok channel result = %+v, want OK=true with identity primary/feishu", res)
	}

	if res, ok := byID[sendFailCh.ID]; !ok || res.OK || res.Error != "dial tcp: i/o timeout" {
		t.Errorf("send-failure result = %+v, want OK=false with send error", res)
	}

	if res, ok := byID[buildFailCh.ID]; !ok || res.OK || !strings.Contains(res.Error, "unsupported proxy scheme") {
		t.Errorf("build-failure result = %+v, want OK=false with build error", res)
	}

	if res, leaked := byID[disabledCh.ID]; leaked {
		t.Errorf("disabled channel was probed: %+v", res)
	}
}

// TestChannelServiceTestAllChannelsNoRuntime verifies the service refuses
// the batch probe when no runtime is wired (the 503 path).
func TestChannelServiceTestAllChannelsNoRuntime(t *testing.T) {
	gdb := newStoreTestDB(t)
	svc := NewChannelService(NewStore(gdb, nil), NewDeliveryStore(gdb), nil)

	if _, err := svc.TestAllChannels(context.Background()); err == nil {
		t.Fatal("expected error when runtime is nil")
	}
}
