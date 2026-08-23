// Copyright © 2026 Beijing Ruishuo Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

package db

import (
	"context"
	"reflect"
	"strings"

	"gorm.io/gorm/schema"
)

// commaList is a GORM serializer for []string fields stored as
// comma-separated varchar columns, declared via
// `gorm:"serializer:commalist"`.
//
// Reads of NULL and "" leave the field nil (so json omitempty drops it);
// a nil or empty slice persists as the empty string. Elements are trimmed
// of surrounding whitespace on read; empty elements are dropped.
type commaList struct{}

// Scan implements schema.SerializerInterface.
func (commaList) Scan(ctx context.Context, field *schema.Field, dst reflect.Value, dbValue any) error {
	var raw string
	switch v := dbValue.(type) {
	case nil:
		return nil
	case []byte:
		raw = string(v)
	case string:
		raw = v
	default:
		// tolerant by contract: non-string payload decodes to nil
		return nil
	}
	if raw == "" {
		return nil
	}
	parts := strings.Split(raw, ",")
	values := make([]string, 0, len(parts))
	for _, part := range parts {
		if part = strings.TrimSpace(part); part != "" {
			values = append(values, part)
		}
	}
	if len(values) == 0 {
		return nil
	}
	field.ReflectValueOf(ctx, dst).Set(reflect.ValueOf(values))
	return nil
}

// Value implements schema.SerializerValuerInterface.
func (commaList) Value(_ context.Context, _ *schema.Field, _ reflect.Value, fieldValue any) (any, error) {
	rv := reflect.ValueOf(fieldValue)
	if rv.Kind() != reflect.Slice || rv.Len() == 0 {
		return "", nil
	}
	parts := make([]string, 0, rv.Len())
	for i := range rv.Len() {
		parts = append(parts, rv.Index(i).String())
	}
	return strings.Join(parts, ","), nil
}

func init() {
	schema.RegisterSerializer("commalist", commaList{})
}
