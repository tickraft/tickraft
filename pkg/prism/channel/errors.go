// Copyright © 2026 Beijing Zhichuan Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

package channel

import (
	"errors"
	"fmt"
)

// ErrCircuitOpen is the sentinel error returned when a channel's circuit
// breaker is open and the send is short-circuited.
var ErrCircuitOpen = errors.New("channel: circuit breaker is open")

// SendError wraps an error encountered while sending to a notification
// channel. It retains the channel name for logging and a Retryable flag so
// callers can decide whether to requeue the alert.
type SendError struct {
	// ChannelName is the name of the channel that produced the error.
	ChannelName string
	// Retryable indicates whether the caller may retry the send.
	Retryable bool
	// Err is the underlying error.
	Err error
}

// Error implements the error interface.
func (e *SendError) Error() string {
	return fmt.Sprintf("channel %s: %v", e.ChannelName, e.Err)
}

// Unwrap returns the underlying error, allowing errors.Is and errors.As to
// traverse into the wrapped error.
func (e *SendError) Unwrap() error {
	return e.Err
}

// NewSendError creates a SendError instance.
func NewSendError(channelName string, retryable bool, err error) *SendError {
	return &SendError{
		ChannelName: channelName,
		Retryable:   retryable,
		Err:         err,
	}
}
