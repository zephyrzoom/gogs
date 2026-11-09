// Copyright 2026 The Gogs Authors. All rights reserved.
// Use of this source code is governed by a MIT-style
// license that can be found in the LICENSE file.

package migrations

import (
	"gorm.io/gorm"
)

// repositoryV24 is a subset of the "repository" table that only contains the
// column introduced by the migration v24.
type repositoryV24 struct {
	MaxRepoSize int64 `gorm:"not null;default:0"`
}

func (*repositoryV24) TableName() string {
	return "repository"
}

// addMaxRepoSizeToRepository adds the "max_repo_size" column to the "repository"
// table, which stores the maximum disk usage (in megabytes) of the repository
// itself. A value of 0 means the repository follows the global limit
// "MAX_REPO_SIZE" of the "[repository]" section.
func addMaxRepoSizeToRepository(db *gorm.DB) error {
	if db.Migrator().HasColumn(&repositoryV24{}, "MaxRepoSize") {
		return errMigrationSkipped
	}
	return db.Migrator().AddColumn(&repositoryV24{}, "MaxRepoSize")
}
