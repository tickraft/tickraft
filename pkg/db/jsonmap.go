// Copyright © 2026 Beijing Ruishuo Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

package db

import (
	"context"
	"reflect"

	"github.com/bytedance/sonic"
	"gorm.io/gorm/schema"
)

// tolerantJSON is a GORM serializer for JSON-shaped map fields
// (map[string]string, map[string]any) stored in text columns, declared via
// `gorm:"serializer:tolerantjson"`.
//
// It differs from the built-in json serializer in two deliberate ways,
// both preserving the historical MetadataMap/decodeRuleMetadata contract:
//
//   - Reads never fail. NULL, "", "null", "{}", and malformed blobs all
//     decode to a nil field instead of erroring, so a bad payload never
//     blocks row loading.
//   - Nil and empty maps persist as the empty string, not SQL NULL and not
//     a literal "null", matching the on-disk format previously written by
//     encodeRuleMetadata.
//
// JSON encoding uses bytedance/sonic for performance; its API is
// drop-in compatible with encoding/json.
type tolerantJSON struct{}

// Scan implements schema.SerializerInterface.
func (tolerantJSON) Scan(ctx context.Context, field *schema.Field, dst reflect.Value, dbValue any) error {
	var raw []byte
	switch v := dbValue.(type) {
	case nil:
		return nil
	case []byte:
		raw = v
	case string:
		raw = []byte(v)
	default:
		var err error
		if raw, err = sonic.Marshal(v); err != nil {
			//nolint:nilerr // tolerant by contract: undecodable payload decodes to nil
			return nil
		}
	}
	if len(raw) == 0 {
		return nil
	}
	decoded := reflect.New(field.FieldType)
	if err := sonic.Unmarshal(raw, decoded.Interface()); err != nil {
		//nolint:nilerr // tolerant by contract: undecodable payload decodes to nil
		return nil
	}
	value := decoded.Elem()
	if value.Kind() == reflect.Map && value.Len() == 0 {
		return nil
	}
	field.ReflectValueOf(ctx, dst).Set(value)
	return nil
}

// Value implements schema.SerializerValuerInterface.
func (tolerantJSON) Value(_ context.Context, _ *schema.Field, _ reflect.Value, fieldValue any) (any, error) {
	rv := reflect.ValueOf(fieldValue)
	if rv.Kind() != reflect.Map || rv.IsNil() || rv.Len() == 0 {
		return "", nil
	}
	raw, err := sonic.Marshal(fieldValue)
	if err != nil {
		return "", err
	}
	return string(raw), nil
}

func init() {
	schema.RegisterSerializer("tolerantjson", tolerantJSON{})
}
