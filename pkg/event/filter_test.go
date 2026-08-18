// Copyright © 2026 Beijing Ruishuo Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

package event

import (
	"testing"
)

func TestFilterFunc(t *testing.T) {
	filter := FilterFunc(func(env Envelope) bool {
		return env.TenantID == "tenant-001"
	})

	matching := Envelope{TenantID: "tenant-001"}
	nonMatching := Envelope{TenantID: "tenant-002"}

	if !filter(matching) {
		t.Error("filter should return true for matching tenant")
	}
	if filter(nonMatching) {
		t.Error("filter should return false for non-matching tenant")
	}
}

func TestFilterFuncByPriority(t *testing.T) {
	filter := FilterFunc(func(env Envelope) bool {
		return env.Priority >= 5
	})

	if !filter(Envelope{Priority: 10}) {
		t.Error("filter should return true for priority 10")
	}
	if !filter(Envelope{Priority: 5}) {
		t.Error("filter should return true for priority 5")
	}
	if filter(Envelope{Priority: 4}) {
		t.Error("filter should return false for priority 4")
	}
}

func TestFilterFuncByEventType(t *testing.T) {
	filter := FilterFunc(func(env Envelope) bool {
		return env.Type == TypeExecutionTriggered
	})

	if !filter(Envelope{Type: TypeExecutionTriggered}) {
		t.Error("filter should return true for TypeExecutionTriggered")
	}
	if filter(Envelope{Type: TypeExecutionCompleted}) {
		t.Error("filter should return false for TypeExecutionCompleted")
	}
}

func TestExactMatcher(t *testing.T) {
	matcher := ExactMatcher{Type: TypeExecutionTriggered}

	if !matcher.Match(Envelope{Type: TypeExecutionTriggered}) {
		t.Error("ExactMatcher should match same event type")
	}
	if matcher.Match(Envelope{Type: TypeExecutionCompleted}) {
		t.Error("ExactMatcher should not match different event type")
	}
}

func TestExactMatcherAllTypes(t *testing.T) {
	types := []Type{
		TypeTaskCreated, TypeExecutionTriggered, TypeAssetStatusChanged,
		TypeTelemetryMetricExceeded, TypeAlertTriggered, TypeRemediationTriggered,
		TypeSystemConfigChanged,
	}
	for _, typ := range types {
		matcher := ExactMatcher{Type: typ}
		if !matcher.Match(Envelope{Type: typ}) {
			t.Errorf("ExactMatcher should match %s", typ)
		}
	}
}

func TestRegexMatcher(t *testing.T) {
	matcher, err := NewRegexMatcher(`execution\.\w+`)
	if err != nil {
		t.Fatalf("create regex matcher: %v", err)
	}

	if !matcher.Match(Envelope{Type: TypeExecutionTriggered}) {
		t.Error("RegexMatcher should match execution.triggered")
	}
	if !matcher.Match(Envelope{Type: TypeExecutionCompleted}) {
		t.Error("RegexMatcher should match execution.completed")
	}
	if matcher.Match(Envelope{Type: TypeTaskCreated}) {
		t.Error("RegexMatcher should not match task.created")
	}
}

func TestRegexMatcherSpecificPattern(t *testing.T) {
	matcher, err := NewRegexMatcher(`^execution\.triggered$`)
	if err != nil {
		t.Fatalf("create regex matcher: %v", err)
	}

	if !matcher.Match(Envelope{Type: TypeExecutionTriggered}) {
		t.Error("RegexMatcher should match execution.triggered")
	}
	if matcher.Match(Envelope{Type: TypeExecutionCompleted}) {
		t.Error("RegexMatcher should not match execution.completed")
	}
}

func TestRegexMatcherDomainPattern(t *testing.T) {
	matcher, err := NewRegexMatcher(`^task\.\w+`)
	if err != nil {
		t.Fatalf("create regex matcher: %v", err)
	}

	taskTypes := []Type{
		TypeTaskCreated, TypeTaskUpdated, TypeTaskDeleted,
		TypeTaskPaused, TypeTaskResumed, TypeTaskScheduled, TypeTaskRetryScheduled,
	}
	for _, typ := range taskTypes {
		if !matcher.Match(Envelope{Type: typ}) {
			t.Errorf("RegexMatcher should match %s", typ)
		}
	}

	nonTaskTypes := []Type{
		TypeExecutionTriggered, TypeAssetStatusChanged, TypeAlertTriggered,
	}
	for _, typ := range nonTaskTypes {
		if matcher.Match(Envelope{Type: typ}) {
			t.Errorf("RegexMatcher should not match %s", typ)
		}
	}
}

func TestRegexMatcherInvalidPattern(t *testing.T) {
	_, err := NewRegexMatcher(`[invalid`)
	if err == nil {
		t.Error("expected error for invalid regex pattern")
	}
}

func TestMatcherInterface(t *testing.T) {
	matchers := make([]Matcher, 0, 2)
	matchers = append(matchers, ExactMatcher{Type: TypeExecutionTriggered})

	regexM, err := NewRegexMatcher(`execution\.\w+`)
	if err != nil {
		t.Fatalf("create regex matcher: %v", err)
	}
	matchers = append(matchers, regexM)

	env := Envelope{Type: TypeExecutionTriggered}
	for i, m := range matchers {
		if !m.Match(env) {
			t.Errorf("matcher %d should match", i)
		}
	}
}
