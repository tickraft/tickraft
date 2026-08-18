// Copyright © 2026 Beijing Ruishuo Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

// Package hlogzap bridges the Hertz hlog logging interface to zap so that
// all framework-level logs (middleware recovery, access log, TLS reload,
// ACME, etc.) flow through the same structured logging pipeline as the
// rest of the application.
//
// The bridge implements hlog.FullLogger (Logger + FormatLogger + CtxLogger
// + Control) by delegating each call to the wrapped *zap.Logger. A dynamic
// zap.AtomicLevel tracks the level set via SetLevel so hlog.SetLevel calls
// from the API server (e.g., LevelWarn in release mode) are honored without
// recreating the logger.
package hlogzap

import (
	"context"
	"fmt"
	"io"
	"os"

	"github.com/cloudwego/hertz/pkg/common/hlog"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

// zapFullLogger implements hlog.FullLogger by delegating to a *zap.Logger.
// It is the bridge between Hertz's framework logging (hlog) and the
// application's zap logger, ensuring all log output — including middleware
// recovery, access logs, TLS reload warnings, and ACME events — goes
// through zap instead of the default stderr-based hlog writer.
type zapFullLogger struct {
	logger *zap.Logger
	level  zap.AtomicLevel
}

// Compile-time assertion that zapFullLogger satisfies hlog.FullLogger.
var _ hlog.FullLogger = (*zapFullLogger)(nil)

// NewLogger creates an hlog.FullLogger backed by the given zap logger.
// The returned logger is suitable for passing to hlog.SetLogger so all
// hlog calls are redirected to zap.
//
// The base logger's own level still applies as a floor: messages below the
// base logger's level are filtered by zap's core even if the bridge's
// dynamic level would allow them. In practice both levels are derived from
// the same config.Logger.Mode so they stay in sync.
func NewLogger(base *zap.Logger) hlog.FullLogger {
	return &zapFullLogger{
		logger: base,
		level:  zap.NewAtomicLevelAt(zapcore.InfoLevel),
	}
}

// --- Control ---

// SetLevel sets the minimum hlog level that will be forwarded to zap.
// Messages below this level are dropped before reaching the zap logger.
func (l *zapFullLogger) SetLevel(lv hlog.Level) {
	l.level.SetLevel(hlogToZapLevel(lv))
}

// SetOutput is a no-op because the zap logger's output is configured at
// construction time (via zap.NewDevelopment / zap.NewProduction) and
// cannot be redirected without rebuilding the core. This method is never
// called in the tickraft codebase; hlog.SetOutput is unused because the
// runtime controls output through the zap logger configuration.
func (l *zapFullLogger) SetOutput(_ io.Writer) {
	// intentionally no-op: see godoc.
}

// --- Logger (v ...interface{}) ---

// Trace logs at Debug level (zap has no Trace level).
func (l *zapFullLogger) Trace(v ...any) {
	l.log(hlog.LevelTrace, fmt.Sprint(v...))
}

// Debug logs at Debug level.
func (l *zapFullLogger) Debug(v ...any) {
	l.log(hlog.LevelDebug, fmt.Sprint(v...))
}

// Info logs at Info level.
func (l *zapFullLogger) Info(v ...any) {
	l.log(hlog.LevelInfo, fmt.Sprint(v...))
}

// Notice logs at Info level (zap has no Notice level).
func (l *zapFullLogger) Notice(v ...any) {
	l.log(hlog.LevelNotice, fmt.Sprint(v...))
}

// Warn logs at Warn level.
func (l *zapFullLogger) Warn(v ...any) {
	l.log(hlog.LevelWarn, fmt.Sprint(v...))
}

// Error logs at Error level.
func (l *zapFullLogger) Error(v ...any) {
	l.log(hlog.LevelError, fmt.Sprint(v...))
}

// Fatal logs at Error level and then exits the process. zap.Fatal would
// call os.Exit(1) directly; we use Error + os.Exit to ensure the log
// entry is flushed before exit and to keep the behavior consistent with
// the hlog contract (Fatal is terminal).
func (l *zapFullLogger) Fatal(v ...any) {
	l.log(hlog.LevelFatal, fmt.Sprint(v...))
	_ = l.logger.Sync()
	os.Exit(1)
}

// --- FormatLogger (format string, v ...interface{}) ---

// Tracef logs at Debug level with a formatted message.
func (l *zapFullLogger) Tracef(format string, v ...any) {
	l.log(hlog.LevelTrace, fmt.Sprintf(format, v...))
}

// Debugf logs at Debug level with a formatted message.
func (l *zapFullLogger) Debugf(format string, v ...any) {
	l.log(hlog.LevelDebug, fmt.Sprintf(format, v...))
}

// Infof logs at Info level with a formatted message.
func (l *zapFullLogger) Infof(format string, v ...any) {
	l.log(hlog.LevelInfo, fmt.Sprintf(format, v...))
}

// Noticef logs at Info level with a formatted message.
func (l *zapFullLogger) Noticef(format string, v ...any) {
	l.log(hlog.LevelNotice, fmt.Sprintf(format, v...))
}

// Warnf logs at Warn level with a formatted message.
func (l *zapFullLogger) Warnf(format string, v ...any) {
	l.log(hlog.LevelWarn, fmt.Sprintf(format, v...))
}

// Errorf logs at Error level with a formatted message.
func (l *zapFullLogger) Errorf(format string, v ...any) {
	l.log(hlog.LevelError, fmt.Sprintf(format, v...))
}

// Fatalf logs at Error level with a formatted message and then exits.
func (l *zapFullLogger) Fatalf(format string, v ...any) {
	l.log(hlog.LevelFatal, fmt.Sprintf(format, v...))
	_ = l.logger.Sync()
	os.Exit(1)
}

// --- CtxLogger (ctx, format, v ...) ---

// CtxTracef logs at Debug level. The context is accepted to satisfy the
// hlog.CtxLogger interface; zap does not perform context-based filtering
// so the context is not consulted.
func (l *zapFullLogger) CtxTracef(_ context.Context, format string, v ...any) {
	l.log(hlog.LevelTrace, fmt.Sprintf(format, v...))
}

// CtxDebugf logs at Debug level.
func (l *zapFullLogger) CtxDebugf(_ context.Context, format string, v ...any) {
	l.log(hlog.LevelDebug, fmt.Sprintf(format, v...))
}

// CtxInfof logs at Info level.
func (l *zapFullLogger) CtxInfof(_ context.Context, format string, v ...any) {
	l.log(hlog.LevelInfo, fmt.Sprintf(format, v...))
}

// CtxNoticef logs at Info level.
func (l *zapFullLogger) CtxNoticef(_ context.Context, format string, v ...any) {
	l.log(hlog.LevelNotice, fmt.Sprintf(format, v...))
}

// CtxWarnf logs at Warn level.
func (l *zapFullLogger) CtxWarnf(_ context.Context, format string, v ...any) {
	l.log(hlog.LevelWarn, fmt.Sprintf(format, v...))
}

// CtxErrorf logs at Error level.
func (l *zapFullLogger) CtxErrorf(_ context.Context, format string, v ...any) {
	l.log(hlog.LevelError, fmt.Sprintf(format, v...))
}

// CtxFatalf logs at Error level and then exits.
func (l *zapFullLogger) CtxFatalf(_ context.Context, format string, v ...any) {
	l.log(hlog.LevelFatal, fmt.Sprintf(format, v...))
	_ = l.logger.Sync()
	os.Exit(1)
}

// --- internal ---

// log is the single entry point for all non-Fatal log calls. It checks
// the bridge's dynamic level first (set via SetLevel), then delegates to
// the zap logger's own level check via CheckedEntry. Both checks must pass
// for the message to be written.
func (l *zapFullLogger) log(lv hlog.Level, msg string) {
	zapLv := hlogToZapLevel(lv)
	if !l.level.Enabled(zapLv) {
		return
	}
	if ce := l.logger.Check(zapLv, msg); ce != nil {
		ce.Write()
	}
}

// hlogToZapLevel maps an hlog.Level to the closest zapcore.Level. zap has
// no Trace or Notice level, so they map to Debug and Info respectively.
// Fatal maps to Error to avoid os.Exit in a running server; the Fatal
// methods above call os.Exit explicitly after logging.
func hlogToZapLevel(lv hlog.Level) zapcore.Level {
	switch lv {
	case hlog.LevelTrace, hlog.LevelDebug:
		return zapcore.DebugLevel
	case hlog.LevelInfo, hlog.LevelNotice:
		return zapcore.InfoLevel
	case hlog.LevelWarn:
		return zapcore.WarnLevel
	case hlog.LevelError, hlog.LevelFatal:
		return zapcore.ErrorLevel
	default:
		return zapcore.InfoLevel
	}
}
