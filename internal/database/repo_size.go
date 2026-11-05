// Copyright 2025 The Gogs Authors. All rights reserved.
// Use of this source code is governed by a MIT-style
// license that can be found in the LICENSE file.

package database

import (
	"context"
	"fmt"
	"strings"

	"gorm.io/gorm"
	log "unknwon.dev/clog/v2"

	"gogs.io/gogs/internal/conf"
	"gogs.io/gogs/internal/repoutil"
)

// lfsSizeKB returns the size of LFS objects of the repository in kilobytes.
func lfsSizeKB(ctx context.Context, db *gorm.DB, repoID int64) int64 {
	size, err := newLFSStore(db).TotalObjectsSize(ctx, repoID)
	if err != nil {
		log.Error("Failed to calculate repository LFS size [repo_id: %d]: %v", repoID, err)
		return 0
	}
	return size / 1024
}

// lfsSizeByOwnerKB returns the size of LFS objects of all repositories owned by
// the given account in kilobytes.
func lfsSizeByOwnerKB(ctx context.Context, db *gorm.DB, ownerID int64) int64 {
	size, err := newLFSStore(db).TotalObjectsSizeByOwner(ctx, ownerID)
	if err != nil {
		log.Error("Failed to calculate account LFS size [user_id: %d]: %v", ownerID, err)
		return 0
	}
	return size / 1024
}

// CheckSizeLimits reports whether the repository has reached the size limits
// that apply to it, and returns a message explaining the rejection to the user
// when it has. The message is empty when no limit is reached.
//
// Two independent limits are checked:
//   - the per repository limit, see "MAX_REPO_SIZE" of the "[repository]" section
//   - the total disk usage limit of the owning account, see "MAX_REPO_TOTAL_SIZE"
//     of the "[repository]" section and "max_repo_total_size" of the account
//
// It is safe to call this before the objects of a push are received, which
// allows rejecting a push without transferring it.
func (s *RepositoriesStore) CheckSizeLimits(ctx context.Context, repoID int64) (exceeded bool, message string) {
	repo, err := s.GetByID(ctx, repoID)
	if err != nil {
		if !IsErrRepoNotExist(err) {
			log.Error("Failed to get repository [id: %d]: %v", repoID, err)
		}
		return false, ""
	}

	owner, err := newUsersStore(s.db).GetByID(ctx, repo.OwnerID)
	if err != nil {
		log.Error("Failed to get repository owner [id: %d]: %v", repo.OwnerID, err)
		return false, ""
	}

	if exceeded, message = checkRepoSizeLimit(ctx, s.db, repo, owner); exceeded {
		return true, message
	}
	return checkOwnerTotalSizeLimit(ctx, s.db, repo, owner)
}

// checkRepoSizeLimit checks the per repository limit of "MAX_REPO_SIZE".
func checkRepoSizeLimit(ctx context.Context, db *gorm.DB, repo *Repository, owner *User) (bool, string) {
	limitMB := conf.Repository.MaxRepoSize
	if limitMB <= 0 {
		return false, ""
	}

	sizeKB, err := repoutil.RepositorySize(repoutil.RepositoryPath(owner.Name, repo.Name))
	if err != nil {
		// Failing to measure the size should never block users from pushing.
		log.Error("Failed to calculate repository size [repo: %s/%s]: %v", owner.Name, repo.Name, err)
		return false, ""
	}

	lfsKB := lfsSizeKB(ctx, db, repo.ID)
	totalKB := sizeKB + lfsKB
	limitKB := limitMB * 1024
	pendingKB := repoutil.PendingObjectsSize()
	if totalKB < limitKB && totalKB+pendingKB < limitKB {
		return false, ""
	}

	var b strings.Builder
	if totalKB >= limitKB {
		b.WriteString("仓库已达到大小上限，推送被拒绝。\n")
	} else {
		b.WriteString("本次推送后将超过仓库大小上限，推送被拒绝。\n")
	}
	fmt.Fprintf(&b, "仓库：%s/%s\n", owner.Name, repo.Name)
	fmt.Fprintf(&b, "当前大小：%s（Git 对象 %s", repoutil.HumanSize(totalKB), repoutil.HumanSize(sizeKB))
	if lfsKB > 0 {
		fmt.Fprintf(&b, " + LFS 对象 %s", repoutil.HumanSize(lfsKB))
	}
	fmt.Fprintf(&b, "），单仓库上限：%d MB", limitMB)
	if pendingKB > 0 && totalKB < limitKB {
		fmt.Fprintf(&b, "，本次推送约 %s", repoutil.HumanSize(pendingKB))
	}
	b.WriteString("\n请及时清理不使用的仓库或文件。")
	log.Info("Reject push by repository size limit [repo: %s/%s, size: %d KB, limit: %d KB]", owner.Name, repo.Name, totalKB, limitKB)
	return true, b.String()
}

// checkOwnerTotalSizeLimit checks the total disk usage limit of the account
// owning the repository, see "MaxRepoTotalSize".
func checkOwnerTotalSizeLimit(ctx context.Context, db *gorm.DB, repo *Repository, owner *User) (bool, string) {
	// The account specific limit takes precedence over the global default,
	// which is configured by "MAX_REPO_TOTAL_SIZE" of the "[repository]" section.
	limitMB := owner.MaxRepoTotalSizeLimit()
	if limitMB <= 0 {
		return false, ""
	}
	limitSource := "全局默认上限"
	if owner.MaxRepoTotalSize > 0 {
		limitSource = "账号上限"
	}

	// The quarantine directory of the on-going push lives inside the repository
	// directory, leave it out to avoid counting the push twice.
	sizeKB, err := repoutil.DirSize(repoutil.UserPath(owner.Name), repoutil.QuarantineDir())
	if err != nil {
		// Failing to measure the size should never block users from pushing.
		log.Error("Failed to calculate account disk usage [user: %s]: %v", owner.Name, err)
		return false, ""
	}
	sizeKB += lfsSizeByOwnerKB(ctx, db, owner.ID)

	limitKB := limitMB * 1024
	pendingKB := repoutil.PendingObjectsSize()
	if sizeKB < limitKB && sizeKB+pendingKB < limitKB {
		return false, ""
	}

	var b strings.Builder
	if sizeKB >= limitKB {
		b.WriteString("账号下所有仓库的总占用已达到上限，推送被拒绝。\n")
	} else {
		b.WriteString("本次推送后将超过账号下所有仓库的总占用上限，推送被拒绝。\n")
	}
	fmt.Fprintf(&b, "仓库：%s/%s\n", owner.Name, repo.Name)
	fmt.Fprintf(&b, "账号：%s\n", owner.Name)
	fmt.Fprintf(&b, "当前总占用：%s，%s：%d MB", repoutil.HumanSize(sizeKB), limitSource, limitMB)
	if pendingKB > 0 && sizeKB < limitKB {
		fmt.Fprintf(&b, "，本次推送约 %s", repoutil.HumanSize(pendingKB))
	}
	b.WriteString("\n请及时清理不使用的仓库或文件。")
	log.Info("Reject push by account repository total size limit [user: %s, size: %d KB, limit: %d KB]", owner.Name, sizeKB, limitKB)
	return true, b.String()
}
