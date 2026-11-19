# NetMerge Windows

Windows x64 distribution based on [net-merge v0.3.7](https://github.com/net-merge/net-merge/tree/v0.3.7).
The upstream MIT license and third-party notices are included.

## Use on Windows

- Left-click the tray icon for the dashboard and right-click for the compact panel.
- Choose **English** or **فارسی** to switch the interface language. The selection is remembered.
- The dashboard uses Windows PowerShell 5.1 and Windows Forms, included with supported versions of Windows.

1. Connect the USB modem and Wi-Fi. Launch `net-merge-tray.exe`. Approve the Windows admin prompt for the network driver.
2. The dashboard shows total link speed, adapter health, active flows and adapter caps. Those speeds include all traffic the
   operating system reports on each link.
3. Choose **Smart** to use weighted balancing or **Round robin** to rotate new TCP/UDP flows across online adapters.
   Set per-adapter caps from the dashboard. Closing the dashboard keeps the tray running.
4. For the console, run `net-merge.exe run -distribution round-robin` as administrator.

Each flow stays on its selected link. A multi-connection downloader can use both links. Packet-level bonding for a
single connection requires a remote peer. Use the tray's adapter caps if saturating a link increases game latency.

## Publish v0.6.0

Extract the source ZIP and run `bash install-release.sh` in Git Bash with GitHub CLI installed.
It targets `0fariid0/netmerge` and defaults to `$HOME/github-projects/netmerge` for the local checkout.
Set `REPO` to override the local checkout path. The script pushes the branch and tag, then waits for GitHub Actions
to build and publish the Windows x64 installer and portable ZIP.

For local packaging, install Go 1.27 and Inno Setup 6 and run `scripts/build.ps1 -Version 0.6.0` with PowerShell.
