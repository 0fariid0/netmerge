# NetMerge Windows

Windows x64 distribution based on [net-merge v0.3.7](https://github.com/net-merge/net-merge/tree/v0.3.7).
The upstream MIT license and third-party notices are included.

## Use on Windows

Start `net-merge-tray.exe` and approve the Windows admin prompt. Use the notification area icon to open the menu.
The menu shows link traffic, per-adapter caps, language settings and distribution controls.

- **Smart** selects a link based on estimated capacity and observed traffic load.
- **Round robin** rotates new TCP and UDP flows across available links.
- **Game mode** routes new native UDP flows over the healthy adapter with the best probe latency, jitter,
  and recent failure rate. It steers other new connections away from a link with recent protected UDP traffic
  and automatically paces bulk traffic that shares that link. Existing connections keep their original route.
  This is UDP classification by destination port, so calls may also be prioritized. DNS and the common
  HTTPS/QUIC port follow bulk routing. The latency shown in the menu is measured to public probe endpoints.
The tray shows probe diagnostics per adapter in all modes.

Start a game session after selecting **Game mode** or set `-distribution game` when starting NetMerge.

## Game diagnostics

In Game mode, recent destination ICMP results are used when at least two eligible adapters answer.
If the game destination does not respond on enough links, selection uses the general reachability probes.
Route changes apply to new connections. The tray shows the selected link and probe source.

The adaptive cap is directional. It estimates pressure from traffic NetMerge handles and cuts bulk traffic when
a fresh probe shows increased latency. It recovers gradually after sustained good samples.

Smart distribution learns download and upload capacity separately from ordinary traffic. It considers
current adapter load, recent throughput and configured caps when choosing a link for a new connection.
Unmeasured links can still receive traffic. Existing connections stay on the link that opened them.

### Fast download

The tray's **Fast download** mode prefers the best measured download link. When that link gets busy,
new connections can use another link with useful spare capacity. Until measurements exist, it uses
Smart routing. Existing connections keep their assigned adapter. The tray remembers the selected
mode on restart. An explicit `-distribution` startup option takes precedence for that session.

## Publish v1.1.0

Extract the source ZIP and run `bash install-release.sh` in Git Bash with GitHub CLI installed.
It targets `0fariid0/netmerge` and defaults to `$HOME/github-projects/netmerge` for the local checkout.
Set `REPO` to override the local checkout path. The script pushes the branch and tag, then waits for GitHub Actions
to build and publish the Windows x64 installer and portable ZIP.

For local packaging, install Go 1.27 and Inno Setup 6 and run `scripts/build.ps1 -Version 1.1.0` with PowerShell.
