// Copyright 2025 The Gogs Authors. All rights reserved.
// Use of this source code is governed by a MIT-style
// license that can be found in the LICENSE file.

package repoutil

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/gogs/git-module"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRepositorySize(t *testing.T) {
	repoPath := t.TempDir()
	_, err := git.NewCommand("init").RunInDir(repoPath)
	require.NoError(t, err)

	// Create a commit so the repository is not empty.
	require.NoError(t, os.WriteFile(filepath.Join(repoPath, "a.txt"), bytes.Repeat([]byte("a"), 100*1024), 0600))
	require.NoError(t, git.Add(repoPath, git.AddOptions{All: true}))
	require.NoError(t, git.CreateCommit(repoPath, &git.Signature{
		Name:  "gogs",
		Email: "gogs@example.com",
		When:  time.Now(),
	}, "Initial commit"))

	sizeKB, err := RepositorySize(repoPath)
	require.NoError(t, err)
	assert.True(t, sizeKB > 0, "expected a non-zero size, got %d", sizeKB)

	// Simulate running inside a "git receive-pack" hook, where Git points the
	// object directory to the quarantine directory of the on-going push.
	t.Setenv("GIT_OBJECT_DIRECTORY", filepath.Join(t.TempDir(), "incoming-quarantine"))
	sizeKB, err = RepositorySize(repoPath)
	require.NoError(t, err)
	assert.True(t, sizeKB > 0, "expected a non-zero size with quarantine, got %d", sizeKB)
}

func TestPendingObjectsSize(t *testing.T) {
	t.Run("quarantine directory", func(t *testing.T) {
		dir := filepath.Join(t.TempDir(), "incoming-2BvFJ1")
		require.NoError(t, os.MkdirAll(dir, 0755))
		require.NoError(t, os.WriteFile(filepath.Join(dir, "pack-abc.pack"), bytes.Repeat([]byte{0}, 4096), 0600))

		t.Setenv("GIT_QUARANTINE_PATH", dir)
		assert.EqualValues(t, 4, PendingObjectsSize())
	})

	t.Run("not a quarantine directory", func(t *testing.T) {
		dir := filepath.Join(t.TempDir(), "objects")
		require.NoError(t, os.MkdirAll(dir, 0755))
		require.NoError(t, os.WriteFile(filepath.Join(dir, "pack-abc.pack"), bytes.Repeat([]byte{0}, 4096), 0600))

		t.Setenv("GIT_QUARANTINE_PATH", "")
		t.Setenv("GIT_OBJECT_DIRECTORY", dir)
		assert.Zero(t, PendingObjectsSize())
	})

	t.Run("no environment variable", func(t *testing.T) {
		t.Setenv("GIT_QUARANTINE_PATH", "")
		t.Setenv("GIT_OBJECT_DIRECTORY", "")
		assert.Zero(t, PendingObjectsSize())
	})
}

func TestHumanSize(t *testing.T) {
	assert.Equal(t, "0.0 MB", HumanSize(0))
	assert.Equal(t, "512.0 MB", HumanSize(512*1024))
	assert.Equal(t, "1.0 GB", HumanSize(1024*1024))
	assert.Equal(t, "1.5 GB", HumanSize(1536*1024))
}
