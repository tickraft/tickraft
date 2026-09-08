// Copyright © 2026 Beijing Ruishuo Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

package db

import (
	"reflect"
	"testing"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

// commaRow exercises the commalist serializer on the []string field shape
// used by task.Tags.
type commaRow struct {
	ID   int64    `gorm:"column:id;primaryKey;autoIncrement"`
	Tags []string `gorm:"column:tags;size:255;serializer:commalist"`
}

func (commaRow) TableName() string { return "test_commalist" }

func openCommaDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := db.AutoMigrate(&commaRow{}); err != nil {
		t.Fatalf("automigrate: %v", err)
	}
	return db
}

func TestCommaListRoundTrip(t *testing.T) {
	db := openCommaDB(t)
	row := commaRow{Tags: []string{"critical", "nightly"}}
	if err := db.Create(&row).Error; err != nil {
		t.Fatalf("create: %v", err)
	}

	var stored string
	if err := db.Raw("SELECT tags FROM test_commalist WHERE id = ?", row.ID).Row().Scan(&stored); err != nil {
		t.Fatalf("raw select tags: %v", err)
	}
	if stored != "critical,nightly" {
		t.Fatalf("stored tags = %q, want %q", stored, "critical,nightly")
	}

	var got commaRow
	if err := db.First(&got, row.ID).Error; err != nil {
		t.Fatalf("reload: %v", err)
	}
	if !reflect.DeepEqual(got.Tags, row.Tags) {
		t.Fatalf("tags round trip = %v, want %v", got.Tags, row.Tags)
	}
}

func TestCommaListNilAndEmptyPersistEmptyString(t *testing.T) {
	db := openCommaDB(t)
	for i, tags := range [][]string{nil, {}} {
		row := commaRow{Tags: tags}
		if err := db.Create(&row).Error; err != nil {
			t.Fatalf("create row %d: %v", i, err)
		}
		var stored string
		if err := db.Raw("SELECT tags FROM test_commalist WHERE id = ?", row.ID).Row().Scan(&stored); err != nil {
			t.Fatalf("raw select row %d: %v", i, err)
		}
		if stored != "" {
			t.Fatalf("row %d: stored tags = %q, want empty string", i, stored)
		}
		var got commaRow
		if err := db.First(&got, row.ID).Error; err != nil {
			t.Fatalf("reload row %d: %v", i, err)
		}
		if got.Tags != nil {
			t.Fatalf("row %d: tags = %v, want nil", i, got.Tags)
		}
	}
}

func TestCommaListToleratesBadColumnValues(t *testing.T) {
	cases := []string{"", "  ", ",", " a , ,b "}
	db := openCommaDB(t)

	for i, raw := range cases {
		row := commaRow{Tags: []string{"seed"}}
		if err := db.Create(&row).Error; err != nil {
			t.Fatalf("create row %d: %v", i, err)
		}
		if err := db.Exec("UPDATE test_commalist SET tags = ? WHERE id = ?", raw, row.ID).Error; err != nil {
			t.Fatalf("seed row %d: %v", i, err)
		}

		var got commaRow
		if err := db.First(&got, row.ID).Error; err != nil {
			t.Fatalf("case %q: load failed: %v", raw, err)
		}
		switch raw {
		case "", "  ", ",":
			if got.Tags != nil {
				t.Fatalf("case %q: tags = %v, want nil", raw, got.Tags)
			}
		case " a , ,b ":
			if !reflect.DeepEqual(got.Tags, []string{"a", "b"}) {
				t.Fatalf("case %q: tags = %v, want [a b] (trimmed, empties dropped)", raw, got.Tags)
			}
		}
	}
}
