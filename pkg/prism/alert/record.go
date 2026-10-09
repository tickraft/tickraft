// Copyright © 2026 Beijing Zhichuan Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

package alert

import (
	"context"
	"fmt"
	"time"

	"github.com/tickraft/tickraft/pkg/types"
)

// RecordAlert creates alert records for each violation carried by the event.
// It is intended as the OnAlert callback wired into the prism engine.
// When the event carries multiple violations, a single batch INSERT is used
// to minimize DB round-trips. A nil store makes the function a no-op
// so the callback is safe to register even when record persistence is disabled.
// The created records are returned with their database-assigned IDs so
// callers can publish alert.triggered bus events referencing them.
func RecordAlert(ctx context.Context, store RecordStore, evt Event) ([]*Record, error) {
	if store == nil || len(evt.Violations) == 0 {
		return nil, nil
	}
	triggeredAt := evt.Timestamp
	if triggeredAt.IsZero() {
		triggeredAt = time.Now()
	}
	records := make([]*Record, 0, len(evt.Violations))
	for _, v := range evt.Violations {
		r := ViolationToRecord(v, triggeredAt)
		// Stamp the dispatch-assigned EventID so inbound card callbacks
		// can locate every record born from the same event.
		r.EventID = evt.EventID
		records = append(records, r)
	}
	if err := store.CreateBatch(ctx, records); err != nil {
		return nil, fmt.Errorf("persist alert records: %w", err)
	}
	return records, nil
}

// ViolationToRecord builds an alert Record from a single violation,
// carrying the violation's rule attribution (RuleID/RuleName, stamped by
// the rule extractor or the dispatcher). When no rule identity is set
// the rule name is derived from the metric name, log keyword, or
// violation source; severity defaults to "warning" when empty.
func ViolationToRecord(v Violation, triggeredAt time.Time) *Record {
	severity := v.Severity
	if severity == "" {
		severity = string(types.SeverityWarning)
	}
	ruleName := v.RuleName
	if ruleName == "" {
		ruleName = v.Source
	}
	var value float64
	if v.Metric != nil {
		if ruleName == "" {
			ruleName = v.Metric.Name
		}
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
		RuleID:      v.RuleID,
		RuleName:    ruleName,
		Severity:    severity,
		Value:       value,
		Message:     message,
		Status:      StatusFiring,
		TriggeredAt: triggeredAt,
	}
}
