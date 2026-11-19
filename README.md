# NetMerge Windows

Windows x64 distribution based on [net-merge v0.3.7](https://github.com/net-merge/net-merge/tree/v0.3.7).
The upstream MIT license and third-party notices are included.

## Use on Windows

Start `net-merge-tray.exe` and approve the Windows admin prompt. Use the notification area icon to open the menu.
The menu shows link traffic, per-adapter caps, language settings and distribution controls.

- **Smart** selects a link based on estimated capacity and load.
- **Round robin** rotates new TCP and UDP flows across available links.
- **Single connection** requires a compatible remote relay and cannot be selected in this version.

## Publish v0.7.0

Extract the source ZIP and run `bash install-release.sh` in Git Bash with GitHub CLI installed.
It targets `0fariid0/netmerge` and defaults to `$HOME/github-projects/netmerge` for the local checkout.
Set `REPO` to override the local checkout path. The script pushes the branch and tag, then waits for GitHub Actions
to build and publish the Windows x64 installer and portable ZIP.

For local packaging, install Go 1.27 and Inno Setup 6 and run `scripts/build.ps1 -Version 0.7.0` with PowerShell.
