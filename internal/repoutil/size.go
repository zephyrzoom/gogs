// Copyright 2025 The Gogs Authors. All rights reserved.
// Use of this source code is governed by a MIT-style
// license that can be found in the LICENSE file.

package repoutil

import (
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
)

// hookGitEnvs are environment variables that Git sets when running server-side
// hooks (e.g. pre-receive). They make Git commands operate on the repository of
// the on-going push instead of the repository of the current working directory,
// and in particular point the object database to the quarantine directory that
// only holds objects of the on-going push.
var hookGitEnvs = []string{
	"GIT_DIR",
	"GIT_WORK_TREE",
	"GIT_COMMON_DIR",
	"GIT_INDEX_FILE",
	"GIT_OBJECT_DIRECTORY",
	"GIT_ALTERNATE_OBJECT_DIRECTORIES",
	"GIT_QUARANTINE_PATH",
}

// cleanGitEnvs returns the current environment without any variables that are
// bound to the repository of the on-going push.
func cleanGitEnvs() []string {
	envs := make([]string, 0, len(os.Environ()))
	for _, env := range os.Environ() {
		name, _, _ := strings.Cut(env, "=")

		skip := false
		for _, hookEnv := range hookGitEnvs {
			if name == hookEnv {
				skip = true
				break
			}
		}
		if !skip {
			envs = append(envs, env)
		}
	}
	return envs
}

// RepositorySize returns the size of the Git objects stored in the repository
// at the given path, in kilobytes. Both loose and packed objects are counted,
// but no files from the working tree.
func RepositorySize(repoPath string) (int64, error) {
	// Values are reported in kilobytes by default, the "-H" option would turn
	// them into human-readable strings which we do not want.
	cmd := exec.Command("git", "count-objects", "-v")
	cmd.Dir = repoPath
	cmd.Env = cleanGitEnvs()
	out, err := cmd.Output()
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			return 0, fmt.Errorf("git count-objects: %v - %s", err, strings.TrimSpace(string(exitErr.Stderr)))
		}
		return 0, fmt.Errorf("git count-objects: %v", err)
	}

	var sizeKB int64
	for _, line := range strings.Split(string(out), "\n") {
		key, value, ok := strings.Cut(strings.TrimSpace(line), ": ")
		if !ok {
			continue
		}

		switch key {
		case "size", "size-pack":
			n, err := strconv.ParseInt(strings.TrimSpace(value), 10, 64)
			if err != nil {
				return 0, fmt.Errorf("parse %q: %v", line, err)
			}
			sizeKB += n
		}
	}
	return sizeKB, nil
}

// DirSize returns the total disk usage of the files under the given directory,
// in kilobytes. It returns 0 when the directory does not exist. Directories in
// "exclude" are skipped, which is useful to leave out the quarantine directory
// of an on-going push.
func DirSize(dir string, exclude ...string) (int64, error) {
	var size int64
	err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			if os.IsNotExist(err) {
				return nil
			}
			return err
		}
		if d.IsDir() {
			for _, ex := range exclude {
				if ex != "" && filepath.Clean(path) == filepath.Clean(ex) {
					return fs.SkipDir
				}
			}
			return nil
		}
		if info, err := d.Info(); err == nil {
			size += info.Size()
		}
		return nil
	})
	if err != nil {
		return 0, err
	}
	return size / 1024, nil
}

// PendingObjectsSize returns the size of the objects that are being received by
// the on-going push, in kilobytes. It returns 0 when it is not possible to tell,
// e.g. not running inside a Git receive-pack hook.
//
// Git stores the objects of an on-going push in a quarantine directory (the
// "incoming-*" directory under "<repo>/objects"), which is discarded unless the
// push is accepted. Counting its size allows rejecting a push that would take
// the repository over the size limit instead of only rejecting the next one.
func PendingObjectsSize() int64 {
	dir := QuarantineDir()
	if dir == "" {
		return 0
	}

	var sizeKB int64
	_ = filepath.WalkDir(dir, func(_ string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		if info, err := d.Info(); err == nil {
			sizeKB += info.Size() / 1024
		}
		return nil
	})
	return sizeKB
}

// QuarantineDir returns the directory where Git stores the objects of the
// on-going push, or an empty string when not running inside a receive-pack hook.
func QuarantineDir() string {
	dir := os.Getenv("GIT_QUARANTINE_PATH")
	if dir == "" {
		dir = os.Getenv("GIT_OBJECT_DIRECTORY")
	}
	// 🚨 SECURITY: Only return the directory when it really is a quarantine
	// directory, never the live object database.
	if !strings.HasPrefix(filepath.Base(dir), "incoming-") {
		return ""
	}
	return dir
}

// HumanSize returns a human-readable representation of the given size in
// kilobytes, e.g. "10.5 MB" or "1.2 GB".
func HumanSize(sizeKB int64) string {
	const (
		mb = 1024
		gb = 1024 * mb
	)
	if sizeKB >= gb {
		return fmt.Sprintf("%.1f GB", float64(sizeKB)/gb)
	}
	return fmt.Sprintf("%.1f MB", float64(sizeKB)/mb)
}
