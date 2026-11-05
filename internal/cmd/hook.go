// Copyright 2014 The Gogs Authors. All rights reserved.
// Use of this source code is governed by a MIT-style
// license that can be found in the LICENSE file.

package cmd

import (
	"bufio"
	"bytes"
	"context"
	"crypto/tls"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/unknwon/com"
	"github.com/urfave/cli"
	log "unknwon.dev/clog/v2"

	"github.com/gogs/git-module"

	"gogs.io/gogs/internal/conf"
	"gogs.io/gogs/internal/database"
	"gogs.io/gogs/internal/email"
	"gogs.io/gogs/internal/httplib"
	"gogs.io/gogs/internal/repoutil"
)

var (
	Hook = cli.Command{
		Name:        "hook",
		Usage:       "Delegate commands to corresponding Git hooks",
		Description: "All sub-commands should only be called by Git",
		Flags: []cli.Flag{
			stringFlag("config, c", "", "Custom configuration file path"),
		},
		Subcommands: []cli.Command{
			subcmdHookPreReceive,
			subcmdHookUpadte,
			subcmdHookPostReceive,
		},
	}

	subcmdHookPreReceive = cli.Command{
		Name:        "pre-receive",
		Usage:       "Delegate pre-receive Git hook",
		Description: "This command should only be called by Git",
		Action:      runHookPreReceive,
	}
	subcmdHookUpadte = cli.Command{
		Name:        "update",
		Usage:       "Delegate update Git hook",
		Description: "This command should only be called by Git",
		Action:      runHookUpdate,
	}
	subcmdHookPostReceive = cli.Command{
		Name:        "post-receive",
		Usage:       "Delegate post-receive Git hook",
		Description: "This command should only be called by Git",
		Action:      runHookPostReceive,
	}
)

func runHookPreReceive(c *cli.Context) error {
	if os.Getenv("SSH_ORIGINAL_COMMAND") == "" {
		return nil
	}
	setup(c, "pre-receive.log", true)

	isWiki := strings.Contains(os.Getenv(database.EnvRepoCustomHooksPath), ".wiki.git/")

	// The size limit applies to the repository itself, but not to its wiki.
	if !isWiki {
		checkRepositorySizeLimit()
	}

	buf := bytes.NewBuffer(nil)
	scanner := bufio.NewScanner(os.Stdin)
	for scanner.Scan() {
		buf.Write(scanner.Bytes())
		buf.WriteByte('\n')

		if isWiki {
			continue
		}

		fields := bytes.Fields(scanner.Bytes())
		if len(fields) != 3 {
			continue
		}
		oldCommitID := string(fields[0])
		newCommitID := string(fields[1])
		branchName := git.RefShortName(string(fields[2]))

		// Branch protection
		repoID := com.StrTo(os.Getenv(database.EnvRepoID)).MustInt64()
		protectBranch, err := database.GetProtectBranchOfRepoByName(repoID, branchName)
		if err != nil {
			if database.IsErrBranchNotExist(err) {
				continue
			}
			fail("Internal error", "GetProtectBranchOfRepoByName [repo_id: %d, branch: %s]: %v", repoID, branchName, err)
		}
		if !protectBranch.Protected {
			continue
		}

		// Whitelist users can bypass require pull request check
		bypassRequirePullRequest := false

		// Check if user is in whitelist when enabled
		userID := com.StrTo(os.Getenv(database.EnvAuthUserID)).MustInt64()
		if protectBranch.EnableWhitelist {
			if !database.IsUserInProtectBranchWhitelist(repoID, userID, branchName) {
				fail(fmt.Sprintf("Branch '%s' is protected and you are not in the push whitelist", branchName), "")
			}

			bypassRequirePullRequest = true
		}

		// Check if branch allows direct push
		if !bypassRequirePullRequest && protectBranch.RequirePullRequest {
			fail(fmt.Sprintf("Branch '%s' is protected and commits must be merged through pull request", branchName), "")
		}

		// check and deletion
		if newCommitID == git.EmptyID {
			fail(fmt.Sprintf("Branch '%s' is protected from deletion", branchName), "")
		}

		// Check force push
		output, err := git.NewCommand("rev-list", "--max-count=1", oldCommitID, "^"+newCommitID).
			RunInDir(database.RepoPath(os.Getenv(database.EnvRepoOwnerName), os.Getenv(database.EnvRepoName)))
		if err != nil {
			fail("Internal error", "Failed to detect force push: %v", err)
		} else if len(output) > 0 {
			fail(fmt.Sprintf("Branch '%s' is protected from force push", branchName), "")
		}
	}

	customHooksPath := filepath.Join(os.Getenv(database.EnvRepoCustomHooksPath), "pre-receive")
	if !com.IsFile(customHooksPath) {
		return nil
	}

	var hookCmd *exec.Cmd
	if conf.IsWindowsRuntime() {
		hookCmd = exec.Command("bash.exe", "custom_hooks/pre-receive")
	} else {
		hookCmd = exec.Command(customHooksPath)
	}
	hookCmd.Dir = database.RepoPath(os.Getenv(database.EnvRepoOwnerName), os.Getenv(database.EnvRepoName))
	hookCmd.Stdout = os.Stdout
	hookCmd.Stdin = buf
	hookCmd.Stderr = os.Stderr
	if err := hookCmd.Run(); err != nil {
		fail("Internal error", "Failed to execute custom pre-receive hook: %v", err)
	}
	return nil
}

// repositoryLFSSize returns the size of LFS objects of the repository in
// kilobytes. LFS objects are stored outside of the Git repository, but still
// occupy the disk space of the repository.
func repositoryLFSSize(repoID int64) int64 {
	size, err := database.Handle.LFS().TotalObjectsSize(context.Background(), repoID)
	if err != nil {
		log.Error("Failed to calculate repository LFS size [repo_id: %d]: %v", repoID, err)
		return 0
	}
	return size / 1024
}

// checkRepositorySizeLimit rejects the push when the repository has reached its
// size limit, or when the on-going push would take it over the limit.
func checkRepositorySizeLimit() {
	repoOwner := os.Getenv(database.EnvRepoOwnerName)
	repoName := os.Getenv(database.EnvRepoName)
	repoID := com.StrTo(os.Getenv(database.EnvRepoID)).MustInt64()

	checkSingleRepoSizeLimit(repoOwner, repoName, repoID)
	checkOwnerTotalSizeLimit(repoOwner, repoName)
}

// checkSingleRepoSizeLimit rejects the push when the repository itself has
// reached the global size limit ("MAX_REPO_SIZE" of the "[repository]" section).
func checkSingleRepoSizeLimit(repoOwner, repoName string, repoID int64) {
	limitMB := conf.Repository.MaxRepoSize
	if limitMB <= 0 {
		return
	}

	sizeKB, err := repoutil.RepositorySize(database.RepoPath(repoOwner, repoName))
	if err != nil {
		// Failing to measure the size should never block users from pushing.
		log.Error("Failed to calculate repository size [repo: %s/%s]: %v", repoOwner, repoName, err)
		return
	}
	lfsSizeKB := repositoryLFSSize(repoID)

	limitKB := limitMB * 1024
	totalKB := sizeKB + lfsSizeKB
	pendingKB := repoutil.PendingObjectsSize()
	if totalKB < limitKB && totalKB+pendingKB < limitKB {
		return
	}

	var b strings.Builder
	if totalKB >= limitKB {
		b.WriteString("仓库已达到大小上限，推送被拒绝。\n")
	} else {
		b.WriteString("本次推送后将超过仓库大小上限，推送被拒绝。\n")
	}
	fmt.Fprintf(&b, "仓库：%s/%s\n", repoOwner, repoName)
	fmt.Fprintf(&b, "当前大小：%s（Git 对象 %s", repoutil.HumanSize(totalKB), repoutil.HumanSize(sizeKB))
	if lfsSizeKB > 0 {
		fmt.Fprintf(&b, " + LFS 对象 %s", repoutil.HumanSize(lfsSizeKB))
	}
	fmt.Fprintf(&b, "），单仓库上限：%d MB", limitMB)
	if pendingKB > 0 && totalKB < limitKB {
		fmt.Fprintf(&b, "，本次推送约 %s", repoutil.HumanSize(pendingKB))
	}
	b.WriteString("\n请删除仓库中的大文件（如使用 git filter-repo 或 BFG 重写历史）后重试，或联系管理员调整上限。")

	fail(b.String(), "Push rejected by repository size limit [repo: %s/%s, size: %d KB, limit: %d KB]",
		repoOwner, repoName, totalKB, limitKB)
}

// checkOwnerTotalSizeLimit rejects the push when the total disk usage of all
// repositories owned by the account has reached the account limit
// ("max_repo_total_size" of the "user" table).
func checkOwnerTotalSizeLimit(repoOwner, repoName string) {
	owner, err := database.Handle.Users().GetByUsername(context.Background(), repoOwner)
	if err != nil {
		if !database.IsErrUserNotExist(err) {
			log.Error("Failed to get repository owner [name: %s]: %v", repoOwner, err)
		}
		return
	}
	// The account specific limit takes precedence over the global default,
	// which is configured by "MAX_REPO_TOTAL_SIZE" of the "[repository]" section.
	limitMB := owner.MaxRepoTotalSizeLimit()
	if limitMB <= 0 {
		return
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
		return
	}
	if lfsSize, err := database.Handle.LFS().TotalObjectsSizeByOwner(context.Background(), owner.ID); err != nil {
		log.Error("Failed to calculate account LFS size [user_id: %d]: %v", owner.ID, err)
	} else {
		sizeKB += lfsSize / 1024
	}

	limitKB := limitMB * 1024
	pendingKB := repoutil.PendingObjectsSize()
	if sizeKB < limitKB && sizeKB+pendingKB < limitKB {
		return
	}

	var b strings.Builder
	if sizeKB >= limitKB {
		b.WriteString("账号下所有仓库的总占用已达到上限，推送被拒绝。\n")
	} else {
		b.WriteString("本次推送后将超过账号下所有仓库的总占用上限，推送被拒绝。\n")
	}
	fmt.Fprintf(&b, "仓库：%s/%s\n", repoOwner, repoName)
	fmt.Fprintf(&b, "账号：%s\n", owner.Name)
	fmt.Fprintf(&b, "当前总占用：%s，%s：%d MB", repoutil.HumanSize(sizeKB), limitSource, limitMB)
	if pendingKB > 0 && sizeKB < limitKB {
		fmt.Fprintf(&b, "，本次推送约 %s", repoutil.HumanSize(pendingKB))
	}
	b.WriteString("\n请删除账号下仓库中的大文件（如使用 git filter-repo 或 BFG 重写历史）后重试，或联系管理员调整上限。")

	fail(b.String(), "Push rejected by account repository total size limit [user: %s, size: %d KB, limit: %d KB]",
		owner.Name, sizeKB, limitKB)
}

func runHookUpdate(c *cli.Context) error {
	if os.Getenv("SSH_ORIGINAL_COMMAND") == "" {
		return nil
	}
	setup(c, "update.log", false)

	args := c.Args()
	if len(args) != 3 {
		fail("Arguments received are not equal to three", "Arguments received are not equal to three")
	} else if args[0] == "" {
		fail("First argument 'refName' is empty", "First argument 'refName' is empty")
	}

	customHooksPath := filepath.Join(os.Getenv(database.EnvRepoCustomHooksPath), "update")
	if !com.IsFile(customHooksPath) {
		return nil
	}

	var hookCmd *exec.Cmd
	if conf.IsWindowsRuntime() {
		hookCmd = exec.Command("bash.exe", append([]string{"custom_hooks/update"}, args...)...)
	} else {
		hookCmd = exec.Command(customHooksPath, args...)
	}
	hookCmd.Dir = database.RepoPath(os.Getenv(database.EnvRepoOwnerName), os.Getenv(database.EnvRepoName))
	hookCmd.Stdout = os.Stdout
	hookCmd.Stdin = os.Stdin
	hookCmd.Stderr = os.Stderr
	if err := hookCmd.Run(); err != nil {
		fail("Internal error", "Failed to execute custom pre-receive hook: %v", err)
	}
	return nil
}

func runHookPostReceive(c *cli.Context) error {
	if os.Getenv("SSH_ORIGINAL_COMMAND") == "" {
		return nil
	}
	setup(c, "post-receive.log", true)

	// Post-receive hook does more than just gather Git information,
	// so we need to setup additional services for email notifications.
	email.NewContext()

	isWiki := strings.Contains(os.Getenv(database.EnvRepoCustomHooksPath), ".wiki.git/")

	buf := bytes.NewBuffer(nil)
	scanner := bufio.NewScanner(os.Stdin)
	for scanner.Scan() {
		buf.Write(scanner.Bytes())
		buf.WriteByte('\n')

		// TODO: support news feeds for wiki
		if isWiki {
			continue
		}

		fields := bytes.Fields(scanner.Bytes())
		if len(fields) != 3 {
			continue
		}

		options := database.PushUpdateOptions{
			OldCommitID:  string(fields[0]),
			NewCommitID:  string(fields[1]),
			FullRefspec:  string(fields[2]),
			PusherID:     com.StrTo(os.Getenv(database.EnvAuthUserID)).MustInt64(),
			PusherName:   os.Getenv(database.EnvAuthUserName),
			RepoUserName: os.Getenv(database.EnvRepoOwnerName),
			RepoName:     os.Getenv(database.EnvRepoName),
		}
		if err := database.PushUpdate(options); err != nil {
			log.Error("PushUpdate: %v", err)
		}

		// Ask for running deliver hook and test pull request tasks
		q := make(url.Values)
		q.Add("branch", git.RefShortName(options.FullRefspec))
		q.Add("secret", os.Getenv(database.EnvRepoOwnerSaltMd5))
		q.Add("pusher", os.Getenv(database.EnvAuthUserID))
		reqURL := fmt.Sprintf("%s%s/%s/tasks/trigger?%s", conf.Server.LocalRootURL, options.RepoUserName, options.RepoName, q.Encode())
		log.Trace("Trigger task: %s", reqURL)

		resp, err := httplib.Get(reqURL).
			SetTLSClientConfig(&tls.Config{
				InsecureSkipVerify: true,
			}).Response()
		if err == nil {
			_ = resp.Body.Close()
			if resp.StatusCode/100 != 2 {
				log.Error("Failed to trigger task: unsuccessful response code %d", resp.StatusCode)
			}
		} else {
			log.Error("Failed to trigger task: %v", err)
		}
	}

	customHooksPath := filepath.Join(os.Getenv(database.EnvRepoCustomHooksPath), "post-receive")
	if !com.IsFile(customHooksPath) {
		return nil
	}

	var hookCmd *exec.Cmd
	if conf.IsWindowsRuntime() {
		hookCmd = exec.Command("bash.exe", "custom_hooks/post-receive")
	} else {
		hookCmd = exec.Command(customHooksPath)
	}
	hookCmd.Dir = database.RepoPath(os.Getenv(database.EnvRepoOwnerName), os.Getenv(database.EnvRepoName))
	hookCmd.Stdout = os.Stdout
	hookCmd.Stdin = buf
	hookCmd.Stderr = os.Stderr
	if err := hookCmd.Run(); err != nil {
		fail("Internal error", "Failed to execute custom post-receive hook: %v", err)
	}
	return nil
}
