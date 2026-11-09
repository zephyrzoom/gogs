// Copyright 2026 The Gogs Authors. All rights reserved.
// Use of this source code is governed by a MIT-style
// license that can be found in the LICENSE file.

package database

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"gogs.io/gogs/internal/conf"
)

func TestRepoSizeLimit(t *testing.T) {
	tests := []struct {
		name              string
		repoMaxRepoSize   int64
		globalMaxRepoSize int64
		wantLimit         int64
		wantFromRepo      bool
	}{
		{
			name:              "repository limit takes precedence over the global limit",
			repoMaxRepoSize:   100,
			globalMaxRepoSize: 1024,
			wantLimit:         100,
			wantFromRepo:      true,
		},
		{
			name:              "fall back to the global limit",
			repoMaxRepoSize:   0,
			globalMaxRepoSize: 1024,
			wantLimit:         1024,
			wantFromRepo:      false,
		},
		{
			name:              "repository limit works without a global limit",
			repoMaxRepoSize:   512,
			globalMaxRepoSize: 0,
			wantLimit:         512,
			wantFromRepo:      true,
		},
		{
			name:              "repository explicitly waives the global limit",
			repoMaxRepoSize:   -1,
			globalMaxRepoSize: 1024,
			wantLimit:         0,
			wantFromRepo:      true,
		},
		{
			name:              "no limit at all",
			repoMaxRepoSize:   0,
			globalMaxRepoSize: 0,
			wantLimit:         0,
			wantFromRepo:      false,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			conf.SetMockRepository(t, conf.RepositoryOpts{MaxRepoSize: test.globalMaxRepoSize})

			limit, fromRepo := repoSizeLimit(&Repository{MaxRepoSize: test.repoMaxRepoSize})
			assert.EqualValues(t, test.wantLimit, limit)
			assert.Equal(t, test.wantFromRepo, fromRepo)
		})
	}
}
