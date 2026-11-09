---
name: "Dev: Release a patch version"
about: ONLY USED BY MAINTAINERS.
title: "Release [VERSION]"
labels: 📸 release
---

_This is generated from the [patch release template](https://github.com/gogs/gogs/blob/main/.github/ISSUE_TEMPLATE/dev_release_patch_version.md)._

## Before release

On the release branch:

- [ ] Make sure all commits are cherry-picked from the `main` branch by checking the patch milestone.
	- Run `task build` for every cherry-picked commit to make sure there is no compilation error.
- [ ] [Update CHANGELOG on the `main` branch](https://github.com/gogs/gogs/commit/e6c5633f580399c8f4dfc07166a63a01c6c70346) to include entries for the current patch release.

## During release

On the release branch:

- [ ] [Update the hard-coded version](https://github.com/gogs/gogs/commit/f0e3cd90f8d7695960eeef2e4e54b2e717302f6c) to the current release, e.g. `0.12.0` -> `0.12.1`.
- [ ] Wait for GitHub Actions to complete and no failed jobs.
- [ ] Publish a new [GitHub release](https://github.com/gogs/gogs/releases) with entries from [CHANGELOG](https://github.com/gogs/gogs/blob/main/CHANGELOG.md) for the current patch release and all previous releases with same minor version.
	- ⚠️ **Make sure the tag is created on the release branch**.
- [ ] Update all previous GitHub releases with same minor version with the warning:
	```
	**ℹ️ Heads up! There is a new patch release [0.12.1](https://github.com/gogs/gogs/releases/tag/v0.12.1) available, we recommend directly installing or upgrading to that version.**
	```
- [ ] [Compile and pack binaries](https://www.notion.so/jcunknwon/Cheatsheet-and-playbooks-c3b053da42114411bd27285cd065b2a6?source=copy_link#1654f105c63f803f8bfcc117395d9747) (all prefixed with `gogs_<MAJOR>.<MINOR>.<PATCH>_`, e.g. `gogs_0.12.0_`):
	- [ ] macOS: `darwin_arm64.zip`, `darwin_amd64.zip`
	- [ ] Linux: `linux_amd64.tar.gz`, `linux_amd64.zip`
	- [ ] ARM: `linux_armv8.tar.gz`, `linux_armv8.zip`
	- [ ] Windows: `windows_amd64.zip`, `windows_amd64_mws.zip`
- [ ] [Generate SHA256 checksum](https://www.notion.so/jcunknwon/Cheatsheet-and-playbooks-c3b053da42114411bd27285cd065b2a6?source=copy_link#1654f105c63f80d4a74ad8821a403f52) for all binaries to the file `checksum_sha256.txt`.
- [ ] Upload all binaries and `checksum_sha256.txt` to:
	- [ ] GitHub release
	- [ ] https://dl.gogs.io
- [ ] Update content of [Install from binary](https://gogs.io/docs/installation/install_from_binary).

## After release

On the `main` branch:

- [ ] Post the following message on issues that are included in the patch milestone:
    ```
    The <MAJOR>.<MINOR>.<PATCH> has been released that includes the patch of the reported issue.
    ```
- [ ] Create a new release announcement in [Discussions](https://github.com/gogs/gogs/discussions/categories/announcements).
- [ ] Send a tweet on the [official Twitter account](https://twitter.com/GogsHQ) for the patch release.
- [ ] Close the patch milestone.
- [ ] **After 14 days**, publish [GitHub security advisories](https://github.com/gogs/gogs/security) for security patches included in the release.
