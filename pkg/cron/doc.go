// Copyright © 2026 Beijing Ruishuo Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

// Package cron provides a high-performance cron scheduler for
// tickraft and external consumers.
//
// The package supports standard cron expression parsing with configurable
// field layouts, predefined descriptors, timezone specification, and
// convenient schedule construction helpers.
//
// # Schedule Types
//
// The Schedule interface defines the contract for determining next execution
// Two schedule implementations are provided:
//
//   - A bitmask-based schedule parsed from cron expressions via Parse(),
//     using optimized integer types for efficient field matching.
//   - A constant-delay schedule created via Every(), which fires at a
//     fixed interval.
//
// # Expression Format
//
// The default parser accepts 5-field (minute hour dom month dow) or 6-field
// (second minute hour dom month dow) expressions. Fields support:
//
//	Asterisk (*)   - matches all values
//	Question (?)   - "no specific value" (DOM and DOW only, not both)
//	Comma (,)      - list separator (1,3,5)
//	Hyphen (-)     - range (1-5)
//	Slash (/)      - step (*/15, 1-30/5)
//	Names          - month (JAN-DEC) and weekday (SUN-SAT) names
//
// # Predefined Descriptors
//
//	@every <duration>  - constant delay schedule
//	@yearly/@annually  - equivalent to 0 0 0 1 1 *
//	@monthly           - equivalent to 0 0 0 1 * *
//	@weekly            - equivalent to 0 0 0 * * 0
//	@daily/@midnight   - equivalent to 0 0 0 * * *
//	@hourly            - equivalent to 0 0 * * * *
//
// # Timezone Support
//
// Expressions may include a TZ= or CRON_TZ= prefix to specify the timezone:
//
//	TZ=America/New_York 0 30 9 * * MON-FRI
//
// # Convenience Functions
//
//	Minutely() - every minute at second 0
//	Hourly()   - every hour at minute 0
//	Daily()    - every day at midnight
//	Weekly()   - every Sunday at midnight
//	Monthly()  - 1st of every month at midnight
//	Yearly()   - January 1st at midnight
//	Every(d)      - constant delay of duration d
//
// # Custom Parser
//
// Use NewParser with ParseOption flags to configure parsing behavior:
//
//	p := NewParser(Minute | Hour | Dom | Month | Dow)  // standard 5-field
//	sched, err := p.Parse("30 9 * * MON-FRI")
//
// # Crontab
//
// The Crontab type coordinates scheduled entries using a min-heap for
// O(log n) scheduling efficiency and a bounded worker pool for controlled
// concurrency. Use WithWorkerSize to configure the pool size.
//
// # Relationship to the Scheduler Engine
//
// The scheduler engine (pkg/scheduler) reuses this package's Schedule interface
// and Parse function to compute next fire times, but does NOT use the Crontab
// runner type. The engine drives dispatch through its own hierarchical time
// wheel (pkg/timewheel); Crontab is an independent coordination tool provided
// for standalone consumers that need a self-contained cron runner.
package cron
