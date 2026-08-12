// Copyright © 2026 Beijing Ruishuo Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

package alert

import (
	"context"
	"fmt"
	"time"
)

// RecordAlert creates alert records for each violation carried by the event.
// It is intended as the OnAlert callback wired into the prism engine.
// When the event carries multiple violations, a single batch INSERT is used
// to minimize DB round-trips. A nil recordStore makes the function a no-op
// so the callback is safe to register even when record persistence is disabled.
func RecordAlert(ctx context.Context, recordStore RecordStore, evt Event) error {
	if recordStore == nil {
		return nil
	}
	if len(evt.Violations) == 0 {
		return nil
	}
	triggeredAt := evt.Timestamp
	if triggeredAt.IsZero() {
		triggeredAt = time.Now()
	}
	records := make([]*Record, 0, len(evt.Violations))
	for _, v := range evt.Violations {
		records = append(records, ViolationToRecord(v, triggeredAt))
	}
	if err := recordStore.CreateBatch(ctx, records); err != nil {
		return fmt.Errorf("persist alert records: %w", err)
	}
	return nil
}

// ViolationToRecord builds an alert Record from a single violation. The
// rule name is derived from the metric name, log keyword, or violation source;
// severity defaults to "warning" when empty.
func ViolationToRecord(v Violation, triggeredAt time.Time) *Record {
	severity := v.Severity
	if severity == "" {
		severity = "warning"
	}
	ruleName := v.Source
	var value float64
	if v.Metric != nil {
		ruleName = v.Metric.Name
		value = v.Metric.Value
	}
	if ruleName == "" && v.Log != nil {
		ruleName = v.Log.Keyword
	}
	message := v.Message
	if message == "" {
		message = fmt.Sprintf("alert %s: %s", v.Kind, ruleName)
	}
	return &Record{
		RuleID:      0,
		RuleName:    ruleName,
		Severity:    severity,
		Value:       value,
		Message:     message,
		Status:      "firing",
		TriggeredAt: triggeredAt,
	}
}
