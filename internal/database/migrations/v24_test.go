// Copyright 2026 The Gogs Authors. All rights reserved.
// Use of this source code is governed by a MIT-style
// license that can be found in the LICENSE file.

package migrations

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"gogs.io/gogs/internal/dbtest"
)

// repositoryPreV24 is a subset of the "repository" table before the migration v24.
type repositoryPreV24 struct {
	ID   int64 `gorm:"primaryKey"`
	Name string
}

func (*repositoryPreV24) TableName() string {
	return "repository"
}

func TestAddMaxRepoSizeToRepository(t *testing.T) {
	if testing.Short() {
		t.Skip()
	}
	t.Parallel()

	db := dbtest.NewDB(t, "addMaxRepoSizeToRepository", new(repositoryPreV24))
	err := db.Create(&repositoryPreV24{ID: 1, Name: "peach"}).Error
	require.NoError(t, err)
	assert.False(t, db.Migrator().HasColumn(&repositoryV24{}, "MaxRepoSize"))

	err = addMaxRepoSizeToRepository(db)
	require.NoError(t, err)
	assert.True(t, db.Migrator().HasColumn(&repositoryV24{}, "MaxRepoSize"))

	// Existing rows should fall back to the default value.
	var got struct {
		MaxRepoSize int64
	}
	err = db.Table("repository").Where("id = ?", 1).Take(&got).Error
	require.NoError(t, err)
	assert.EqualValues(t, 0, got.MaxRepoSize)

	// Re-run should be skipped
	require.Equal(t, errMigrationSkipped, addMaxRepoSizeToRepository(db))
}
