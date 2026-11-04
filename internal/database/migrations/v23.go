// Copyright 2025 The Gogs Authors. All rights reserved.
// Use of this source code is governed by a MIT-style
// license that can be found in the LICENSE file.

package migrations

import (
	"gorm.io/gorm"
)

// userV23 is a subset of the "user" table that only contains the column
// introduced by the migration v23.
type userV23 struct {
	MaxRepoTotalSize int64 `gorm:"not null;default:0"`
}

func (*userV23) TableName() string {
	return "user"
}

// addMaxRepoTotalSizeToUser adds the "max_repo_total_size" column to the "user"
// table, which stores the maximum total disk usage (in megabytes) of all
// repositories owned by an account.
func addMaxRepoTotalSizeToUser(db *gorm.DB) error {
	if db.Migrator().HasColumn(&userV23{}, "MaxRepoTotalSize") {
		return errMigrationSkipped
	}
	return db.Migrator().AddColumn(&userV23{}, "MaxRepoTotalSize")
}
