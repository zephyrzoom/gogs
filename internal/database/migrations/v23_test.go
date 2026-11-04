// Copyright 2025 The Gogs Authors. All rights reserved.
// Use of this source code is governed by a MIT-style
// license that can be found in the LICENSE file.

package migrations

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"gogs.io/gogs/internal/dbtest"
)

// userPreV23 is a subset of the "user" table before the migration v23.
type userPreV23 struct {
	ID   int64 `gorm:"primaryKey"`
	Name string
}

func (*userPreV23) TableName() string {
	return "user"
}

func TestAddMaxRepoTotalSizeToUser(t *testing.T) {
	if testing.Short() {
		t.Skip()
	}
	t.Parallel()

	db := dbtest.NewDB(t, "addMaxRepoTotalSizeToUser", new(userPreV23))
	err := db.Create(&userPreV23{ID: 1, Name: "alice"}).Error
	require.NoError(t, err)
	assert.False(t, db.Migrator().HasColumn(&userV23{}, "MaxRepoTotalSize"))

	err = addMaxRepoTotalSizeToUser(db)
	require.NoError(t, err)
	assert.True(t, db.Migrator().HasColumn(&userV23{}, "MaxRepoTotalSize"))

	// Existing rows should fall back to the default value.
	var got struct {
		MaxRepoTotalSize int64
	}
	err = db.Table("user").Where("id = ?", 1).Take(&got).Error
	require.NoError(t, err)
	assert.EqualValues(t, 0, got.MaxRepoTotalSize)

	// Re-run should be skipped
	require.Equal(t, errMigrationSkipped, addMaxRepoTotalSizeToUser(db))
}
