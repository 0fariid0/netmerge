# Installs net-merge on Windows. In PowerShell:
#
#   irm https://raw.githubusercontent.com/0fariid0/netmerge/main/install.ps1 | iex
#
# Downloads the newest installer into your Downloads folder and runs it with its progress window
# showing; it asks for admin rights itself. Then starts the tray. Set $env:NET_MERGE_NO_START = '1'
# first to leave the tray off. It does what you would do by hand, in plain sight: antivirus such as
# Kaspersky stops scripts that drop a program in Temp, run it hidden and elevated, then delete it.
$ErrorActionPreference = 'Stop'
$ProgressPreference = 'SilentlyContinue'   # the progress bar makes downloads very slow in Windows PowerShell

$release = Invoke-RestMethod 'https://api.github.com/repos/0fariid0/netmerge/releases/latest'
if ($release.tag_name -notmatch '^v[0-9]+\.[0-9]+\.[0-9]+$') { throw 'Unexpected NetMerge release tag' }
$name = 'net-merge-setup-' + $release.tag_name.Substring(1) + '.exe'
$asset = $release.assets | Where-Object name -EQ $name | Select-Object -First 1
if (!$asset) { throw "Installer $name is missing from the release." }
$url = "https://github.com/0fariid0/netmerge/releases/download/$($release.tag_name)/$name"
$setup = Join-Path (Join-Path $env:USERPROFILE 'Downloads') $name
Write-Host "==> Downloading net-merge to $setup"
New-Item -ItemType Directory -Force (Split-Path $setup) | Out-Null
Invoke-WebRequest $url -OutFile $setup -UseBasicParsing
if ($asset.digest -match '^sha256:([a-fA-F0-9]{64})$') {
    if ((Get-FileHash -LiteralPath $setup -Algorithm SHA256).Hash -ne $Matches[1]) {
        throw 'Installer checksum mismatch'
    }
}
Write-Host '==> Installing (approve the admin prompt)'
$p = Start-Process $setup -ArgumentList '/SILENT', '/NORESTART' -Wait -PassThru
if ($p.ExitCode -ne 0) { throw "the installer failed (exit code $($p.ExitCode))" }

$dir = Join-Path $env:ProgramFiles 'net-merge'
Write-Host "==> Installed $(& (Join-Path $dir 'net-merge.exe') version)"
if ($env:NET_MERGE_NO_START -eq '1') {
    Write-Host '==> Start it from the Start menu: NetMerge'
} else {
    Start-Process (Join-Path $dir 'net-merge-tray.exe')
    Write-Host '==> Started: the icon is in the tray (maybe behind the ^ arrow next to the clock)'
}
