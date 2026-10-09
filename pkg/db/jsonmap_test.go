// Copyright © 2026 Beijing Ruishuo Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

package db

import (
	"database/sql"
	"reflect"
	"testing"

	"github.com/bytedance/sonic"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

// jsonmapRow exercises tolerantjson on the supported field shapes:
// map[string]string (alert rule metadata), map[string]any (monitor
// point config), and typed slices (task sleep windows).
type jsonmapRow struct {
	ID       int64             `gorm:"column:id;primaryKey;autoIncrement"`
	Metadata map[string]string `gorm:"column:metadata;type:text;serializer:tolerantjson"`
	Config   map[string]any    `gorm:"column:config;type:text;serializer:tolerantjson"`
	Windows  []testWindow      `gorm:"column:windows;type:text;serializer:tolerantjson"`
}

type testWindow struct {
	Start string `json:"start"`
	End   string `json:"end"`
}

func (jsonmapRow) TableName() string { return "test_jsonmap" }

func openJSONMapDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := db.AutoMigrate(&jsonmapRow{}); err != nil {
		t.Fatalf("automigrate: %v", err)
	}
	return db
}

func TestTolerantJSONRoundTrip(t *testing.T) {
	db := openJSONMapDB(t)
	row := jsonmapRow{
		Metadata: map[string]string{"owner": "ops", "env": "prod"},
		Config:   map[string]any{"retries": float64(3), "endpoint": "https://example.test"},
		Windows:  []testWindow{{Start: "00:00", End: "06:00"}, {Start: "22:00", End: "06:00"}},
	}
	if err := db.Create(&row).Error; err != nil {
		t.Fatalf("create: %v", err)
	}

	var stored string
	if err := db.Raw("SELECT metadata FROM test_jsonmap WHERE id = ?", row.ID).Row().Scan(&stored); err != nil {
		t.Fatalf("raw select metadata: %v", err)
	}
	var decoded map[string]string
	if err := sonic.Unmarshal([]byte(stored), &decoded); err != nil {
		t.Fatalf("stored metadata is not valid JSON %q: %v", stored, err)
	}
	if !reflect.DeepEqual(decoded, row.Metadata) {
		t.Fatalf("stored metadata = %v, want %v", decoded, row.Metadata)
	}

	var got jsonmapRow
	if err := db.First(&got, row.ID).Error; err != nil {
		t.Fatalf("reload: %v", err)
	}
	if !reflect.DeepEqual(got.Metadata, row.Metadata) {
		t.Fatalf("metadata round trip = %v, want %v", got.Metadata, row.Metadata)
	}
	if !reflect.DeepEqual(got.Config, row.Config) {
		t.Fatalf("config round trip = %v, want %v", got.Config, row.Config)
	}
	if !reflect.DeepEqual(got.Windows, row.Windows) {
		t.Fatalf("windows round trip = %v, want %v", got.Windows, row.Windows)
	}
}

func TestTolerantJSONEmptySlicePersistsEmptyString(t *testing.T) {
	db := openJSONMapDB(t)
	row := jsonmapRow{Windows: []testWindow{}}
	if err := db.Create(&row).Error; err != nil {
		t.Fatalf("create: %v", err)
	}
	var stored sql.NullString
	if err := db.Raw("SELECT windows FROM test_jsonmap WHERE id = ?", row.ID).Row().Scan(&stored); err != nil {
		t.Fatalf("raw select windows: %v", err)
	}
	if !stored.Valid || stored.String != "" {
		t.Fatalf("column windows = %#v, want empty string (not NULL, not \"null\")", stored)
	}
	var got jsonmapRow
	if err := db.First(&got, row.ID).Error; err != nil {
		t.Fatalf("reload: %v", err)
	}
	if got.Windows != nil {
		t.Fatalf("empty windows column should decode to nil, got %v", got.Windows)
	}
}

func TestTolerantJSONEmptyMapPersistsEmptyString(t *testing.T) {
	db := openJSONMapDB(t)
	row := jsonmapRow{Metadata: map[string]string{}, Config: nil}
	if err := db.Create(&row).Error; err != nil {
		t.Fatalf("create: %v", err)
	}

	for _, column := range []string{"metadata", "config"} {
		var stored sql.NullString
		if err := db.Raw("SELECT "+column+" FROM test_jsonmap WHERE id = ?", row.ID).Row().Scan(&stored); err != nil {
			t.Fatalf("raw select %s: %v", column, err)
		}
		if !stored.Valid || stored.String != "" {
			t.Fatalf("column %s = %#v, want empty string (not NULL, not \"null\")", column, stored)
		}
	}

	var got jsonmapRow
	if err := db.First(&got, row.ID).Error; err != nil {
		t.Fatalf("reload: %v", err)
	}
	if got.Metadata != nil || got.Config != nil {
		t.Fatalf("empty columns should decode to nil fields, got metadata=%v config=%v", got.Metadata, got.Config)
	}
}

func TestTolerantJSONToleratesBadColumnValues(t *testing.T) {
	cases := []string{"", "null", "{}", "garbage{not-json"}
	db := openJSONMapDB(t)

	for i, raw := range cases {
		row := jsonmapRow{Metadata: map[string]string{"k": "v"}}
		if err := db.Create(&row).Error; err != nil {
			t.Fatalf("create row %d: %v", i, err)
		}
		if err := db.Exec("UPDATE test_jsonmap SET metadata = ? WHERE id = ?", raw, row.ID).Error; err != nil {
			t.Fatalf("seed row %d: %v", i, err)
		}

		var got jsonmapRow
		if err := db.First(&got, row.ID).Error; err != nil {
			t.Fatalf("case %q: load failed: %v", raw, err)
		}
		if got.Metadata != nil {
			t.Fatalf("case %q: metadata = %v, want nil", raw, got.Metadata)
		}
	}
}
