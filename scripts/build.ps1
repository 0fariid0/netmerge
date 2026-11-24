# Builds everything into dist\: the two exes, wintun.dll, the installer, and a portable zip.
#   scripts\build.ps1 -Version 0.1.0
# Needs Go and Inno Setup 6 (ISCC.exe on PATH, in a default install dir, or in $env:ISCC).
param(
    [string]$Version = '0.0.0',
    [string]$RepoUrl = ''
)
$ErrorActionPreference = 'Stop'
Set-Location (Split-Path $PSScriptRoot -Parent)

function Invoke-Native {
    param([string]$What, [scriptblock]$Cmd)
    & $Cmd
    if ($LASTEXITCODE -ne 0) { throw "$What failed (exit $LASTEXITCODE)" }
}

if ($Version -notmatch '^\d+\.\d+\.\d+$') { throw "Version must look like 1.2.3, got '$Version'" }

if (Test-Path dist) { Remove-Item dist -Recurse -Force }
New-Item -ItemType Directory dist | Out-Null

Invoke-Native 'go vet'  { go vet ./... }
Invoke-Native 'go test' { go test ./... }

# Install the pinned resource compiler once for both executables.
function Install-ResourceCompiler {
    $toolDir = Join-Path (Get-Location).Path 'dist\build-tools'
    New-Item -ItemType Directory -Force $toolDir | Out-Null
    $previousBin = $env:GOBIN
    $previousProxy = $env:GOPROXY
    $PSNativeCommandUseErrorActionPreference = $false
    try {
        $env:GOBIN = $toolDir
        if ([string]::IsNullOrEmpty($previousProxy)) {
            $env:GOPROXY = 'https://proxy.golang.org|direct'
        }
        for ($attempt = 1; $attempt -le 3; $attempt++) {
            go install github.com/tc-hib/go-winres@v0.3.3 | Out-Host
            if ($LASTEXITCODE -eq 0) {
                return (Join-Path $toolDir 'go-winres.exe')
            }
            if ($attempt -lt 3) {
                Write-Warning "go-winres install failed (attempt $attempt of 3). Retrying..."
                Start-Sleep -Seconds (2 * $attempt)
            }
        }
        throw "Installing go-winres failed after 3 attempts (exit $LASTEXITCODE)"
    } finally {
        $env:GOBIN = $previousBin
        $env:GOPROXY = $previousProxy
    }
}

$winres = Install-ResourceCompiler
function New-Resources([string]$Exe, [string]$Description) {
    Invoke-Native "resources $Exe" { & $winres simply --arch amd64 --out cmd\net-merge\rsrc --icon internal\icon\net-merge.ico --manifest cli --product-name NetMerge --file-description $Description --original-filename $Exe --product-version $Version --file-version $Version --copyright 'net-merge contributors' }
}

# One source, two exes: a console CLI, and a windowless build that defaults to the tray.
New-Resources 'net-merge.exe' 'net-merge'
Invoke-Native 'build cli'  { go build -trimpath -ldflags "-s -w -X main.version=$Version" -o dist\net-merge.exe .\cmd\net-merge }
New-Resources 'net-merge-tray.exe' 'NetMerge Windows Tray'
Invoke-Native 'build tray' { go build -trimpath -ldflags "-s -w -H=windowsgui -X main.defaultCmd=tray -X main.version=$Version" -o dist\net-merge-tray.exe .\cmd\net-merge }
Remove-Item cmd\net-merge\rsrc_windows_amd64.syso
Invoke-Native 'icon' { .\dist\net-merge.exe _icon dist\net-merge.ico }

& "$PSScriptRoot\fetch-wintun.ps1" -OutDir dist

$iscc = $env:ISCC
if (-not $iscc) { $iscc = (Get-Command ISCC.exe -ErrorAction SilentlyContinue).Source }
if (-not $iscc) {
    $iscc = Get-ChildItem 'C:\Program Files (x86)\Inno Setup 6\ISCC.exe', 'C:\Program Files\Inno Setup 6\ISCC.exe' -ErrorAction SilentlyContinue | Select-Object -First 1 -ExpandProperty FullName
}
if (-not $iscc) { throw 'ISCC.exe not found. Install Inno Setup 6 or set $env:ISCC.' }

$isccArgs = @("/DVersion=$Version")
if ($RepoUrl) { $isccArgs += "/DRepoUrl=$RepoUrl" }
Invoke-Native 'inno setup' { & $iscc @isccArgs installer\net-merge.iss }

$portable = "dist\net-merge-$Version-portable.zip"
Compress-Archive -Path dist\net-merge.ico, dist\net-merge.exe, dist\net-merge-tray.exe, dist\wintun.dll, dist\wintun-LICENSE.txt, LICENSE, THIRD-PARTY-NOTICES.md, README.md -DestinationPath $portable
Get-ChildItem dist\net-merge-setup-$Version.exe, $portable | ForEach-Object { '{0}  {1:N1} MB' -f $_.Name, ($_.Length / 1MB) }
