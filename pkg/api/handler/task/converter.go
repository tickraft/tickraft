// Copyright © 2026 Beijing Ruishuo Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

package task

import (
	"encoding/json"
	"strconv"
	"time"

	"github.com/tickraft/tickraft/pkg/task"
)

// converter.go provides bidirectional conversion functions between the
// handler-layer Task and task domain Task.
//
// The two structs serve different concerns:
//   - handler Task is the user-facing task definition view (carrying
//     user-oriented fields such as Name/Schedule/Enabled)
//   - task domain Task (pkg/task.Task) is the executor-facing runtime view
//     (carrying runtime fields such as TenantID/Timeout/Priority)
//
// Conversion strategy:
//   - Directly corresponding fields (ID, Executor<->ExecutorName, Config<->Config)
//     are mapped directly.
//   - handler-only fields (Name/Description/Schedule/Enabled/CreatedAt/UpdatedAt)
//     are serialized into the task domain Task.Metadata so the bidirectional
//     conversion is reversible.
//   - task-domain-only fields (TenantID/AssetID/Timeout/Priority/DependsOn) are
//     read from handler Task.Config and written back to Config on the reverse
//     path.

// Metadata key constants. These keys persist handler-only fields inside
// the task domain Task.Metadata so the handler -> domain -> handler round trip
// does not lose core data.
const (
	metaKeyName        = "name"
	metaKeyDescription = "description"
	metaKeySchedule    = "schedule"
	metaKeyEnabled     = "enabled"
	metaKeyCreatedAt   = "created_at"
	metaKeyUpdatedAt   = "updated_at"
)

// Config key constants. These keys carry the task-domain-only runtime
// fields inside handler Task.Config so they can be expressed in the handler
// view. RunID, RetryPolicy, and Concurrency are mapped directly between
// handler Task and the task domain Task (both have top-level fields) and
// therefore do not need Config keys.
const (
	configKeyTenantID  = "tenant_id"
	configKeyAssetID   = "asset_id"
	configKeyTimeout   = "timeout"
	configKeyPriority  = "priority"
	configKeyDependsOn = "depends_on"
)

// DomainTaskToHandler converts a task domain Task into the handler-layer
// Task struct.
//
// A nil input returns nil.
func DomainTaskToHandler(t *task.Task) *Task {
	if t == nil {
		return nil
	}
	h := &Task{
		ID:          t.ID,
		Executor:    t.ExecutorName,
		Group:       t.Group,
		Tags:        t.Tags,
		RunID:       t.RunID,
		RetryPolicy: t.RetryPolicy,
		Concurrency: t.Concurrency,
	}

	// task domain Config -> handler Config (JSON unmarshal).
	if t.Config != "" {
		var cfg map[string]any
		if err := json.Unmarshal([]byte(t.Config), &cfg); err == nil {
			h.Config = cfg
		}
	}

	// Write task-domain-only fields back to Config.
	writeDomainFieldsToConfig(h, t)

	// Restore handler-only fields from Metadata.
	restoreHandlerFieldsFromMetadata(h, t.Metadata)

	return h
}

// HandlerToDomainTask converts a handler-layer Task into a task domain Task.
//
// A nil input returns nil.
func HandlerToDomainTask(t *Task) *task.Task {
	if t == nil {
		return nil
	}
	s := &task.Task{
		ID:           t.ID,
		ExecutorName: t.Executor,
		Group:        t.Group,
		Tags:         t.Tags,
		RunID:        t.RunID,
		RetryPolicy:  t.RetryPolicy,
		Concurrency:  t.Concurrency,
		Metadata:     make(map[string]string),
	}

	if len(t.Config) > 0 {
		// Read task-domain-only fields from Config.
		s.TenantID = parseInt64FromAny(t.Config[configKeyTenantID])
		s.AssetID = parseInt64FromAny(t.Config[configKeyAssetID])
		s.Timeout = parseDurationFromAny(t.Config[configKeyTimeout])
		s.Priority = parseIntFromAny(t.Config[configKeyPriority])
		s.DependsOn = parseInt64FromAny(t.Config[configKeyDependsOn])

		// Marshal Config (with task-domain-only fields stripped).
		execCfg := make(map[string]any, len(t.Config))
		for k, v := range t.Config {
			if isDomainConfigKey(k) {
				continue
			}
			execCfg[k] = v
		}
		if len(execCfg) > 0 {
			if raw, err := json.Marshal(execCfg); err == nil {
				s.Config = string(raw)
			}
		}
	}

	// Write handler-only fields into Metadata.
	s.Metadata[metaKeyName] = t.Name
	s.Metadata[metaKeyDescription] = t.Description
	s.Metadata[metaKeySchedule] = t.Schedule
	s.Metadata[metaKeyEnabled] = strconv.FormatBool(t.Enabled)
	if !t.CreatedAt.IsZero() {
		s.Metadata[metaKeyCreatedAt] = t.CreatedAt.Format(time.RFC3339Nano)
	}
	if !t.UpdatedAt.IsZero() {
		s.Metadata[metaKeyUpdatedAt] = t.UpdatedAt.Format(time.RFC3339Nano)
	}

	return s
}

// isDomainConfigKey reports whether the given key is a reserved Config key
// for a task-domain-only field.
func isDomainConfigKey(k string) bool {
	switch k {
	case configKeyTenantID, configKeyAssetID, configKeyTimeout, configKeyPriority, configKeyDependsOn:
		return true
	}
	return false
}

func writeDomainFieldsToConfig(h *Task, t *task.Task) {
	if t.TenantID == 0 && t.AssetID == 0 && t.Timeout == 0 &&
		t.Priority == 0 && t.DependsOn == 0 {
		return
	}
	if h.Config == nil {
		h.Config = make(map[string]any)
	}
	if t.TenantID != 0 {
		h.Config[configKeyTenantID] = t.TenantID
	}
	if t.AssetID != 0 {
		h.Config[configKeyAssetID] = t.AssetID
	}
	if t.Timeout != 0 {
		h.Config[configKeyTimeout] = int64(t.Timeout.Seconds())
	}
	if t.Priority != 0 {
		h.Config[configKeyPriority] = t.Priority
	}
	if t.DependsOn != 0 {
		h.Config[configKeyDependsOn] = t.DependsOn
	}
}

func restoreHandlerFieldsFromMetadata(h *Task, metadata map[string]string) {
	if len(metadata) == 0 {
		return
	}
	h.Name = metadata[metaKeyName]
	h.Description = metadata[metaKeyDescription]
	h.Schedule = metadata[metaKeySchedule]
	if v, ok := metadata[metaKeyEnabled]; ok {
		if b, err := strconv.ParseBool(v); err == nil {
			h.Enabled = b
		}
	}
	if v, ok := metadata[metaKeyCreatedAt]; ok {
		if ts, err := time.Parse(time.RFC3339Nano, v); err == nil {
			h.CreatedAt = ts
		}
	}
	if v, ok := metadata[metaKeyUpdatedAt]; ok {
		if ts, err := time.Parse(time.RFC3339Nano, v); err == nil {
			h.UpdatedAt = ts
		}
	}
}

func parseInt64FromAny(v any) int64 {
	switch x := v.(type) {
	case nil:
		return 0
	case float64:
		return int64(x)
	case int:
		return int64(x)
	case int64:
		return x
	case string:
		if n, err := strconv.ParseInt(x, 10, 64); err == nil {
			return n
		}
	}
	return 0
}

func parseIntFromAny(v any) int {
	switch x := v.(type) {
	case nil:
		return 0
	case float64:
		return int(x)
	case int:
		return x
	case int64:
		return int(x)
	case string:
		if n, err := strconv.Atoi(x); err == nil {
			return n
		}
	}
	return 0
}

func parseDurationFromAny(v any) time.Duration {
	switch x := v.(type) {
	case nil:
		return 0
	case string:
		if x == "" {
			return 0
		}
		if d, err := time.ParseDuration(x); err == nil {
			return d
		}
		if n, err := strconv.ParseInt(x, 10, 64); err == nil {
			return time.Duration(n) * time.Second
		}
	case float64:
		return time.Duration(x) * time.Second
	case int:
		return time.Duration(x) * time.Second
	case int64:
		return time.Duration(x) * time.Second
	}
	return 0
}
