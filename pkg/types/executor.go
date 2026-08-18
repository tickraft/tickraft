// Copyright © 2026 Beijing Ruishuo Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

package types

// ExecutorType identifies the kind of task executor that runs a scheduled
// task or a remediation action. The string value is the executor name
// registered with the executor registry and returned by the Name method of
// each executor implementation in pkg/executor.
//
// These constants are shared by the task domain, the prism remediation
// engine (rule executor_type validation and operator registration), and the
// internal assembly layer so that every package agrees on the same closed
// set of executor identifiers.
type ExecutorType string

const (
	// ExecutorHTTP identifies the executor that issues HTTP requests to a
	// configured endpoint.
	ExecutorHTTP ExecutorType = "http"
	// ExecutorTCP identifies the executor that probes TCP port
	// connectivity and measures connection latency.
	ExecutorTCP ExecutorType = "tcp"
	// ExecutorICMP identifies the executor that sends ICMP echo requests
	// to measure reachability and round-trip time.
	ExecutorICMP ExecutorType = "icmp"
	// ExecutorLocal identifies the executor that runs local scripts or
	// commands on the host machine.
	ExecutorLocal ExecutorType = "local"
	// ExecutorWebhook identifies the executor that delivers HTTP webhook
	// callbacks to external endpoints.
	ExecutorWebhook ExecutorType = "webhook"
)
