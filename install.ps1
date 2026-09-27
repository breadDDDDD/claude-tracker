# honjoji installer for Windows. No admin needed: installs to
# %LOCALAPPDATA%\Programs\honjoji and adds it to your *user* PATH.
#   install.cmd              install (or update) and run
#   install.cmd -NoRun       install only
#   install.cmd -Uninstall   remove
param([switch]$Uninstall, [switch]$NoRun)
$ErrorActionPreference = 'Stop'

$dir = Join-Path $env:LOCALAPPDATA 'Programs\honjoji'
$exe = Join-Path $dir 'honjoji.exe'
$envKey = 'HKCU:\Environment'
# WindowsApps is on every user's PATH by default, including in terminals that were
# already open (VS Code, Windows Terminal tabs), so a copy there works immediately.
$appsDir = Join-Path $env:LOCALAPPDATA 'Microsoft\WindowsApps'
$appsExe = Join-Path $appsDir 'honjoji.exe'

function Get-UserPath {
    # Read raw so %VARS% in the user's PATH are preserved.
    $v = (Get-Item $envKey).GetValue('Path', '', 'DoNotExpandEnvironmentNames')
    if ($null -eq $v) { '' } else { $v }
}
function Set-UserPath($value) {
    Set-ItemProperty -Path $envKey -Name Path -Value $value -Type ExpandString
    # Touch a dummy variable so Windows tells new terminals the environment changed.
    [Environment]::SetEnvironmentVariable('HONJOJI_REFRESH', '1', 'User')
    [Environment]::SetEnvironmentVariable('HONJOJI_REFRESH', $null, 'User')
}
$parts = @((Get-UserPath) -split ';' | Where-Object { $_ -ne '' })

if ($Uninstall) {
    Remove-Item -Recurse -Force $dir -ErrorAction SilentlyContinue
    Remove-Item -Force $appsExe, "$appsExe.old" -ErrorAction SilentlyContinue
    Set-UserPath (($parts | Where-Object { $_.TrimEnd('\') -ne $dir }) -join ';')
    Write-Host 'honjoji removed. (Your cache at ~\.honjoji can be deleted too.)'
    exit 0
}

$arch = 'amd64'
if ($env:PROCESSOR_ARCHITECTURE -eq 'ARM64' -or $env:PROCESSOR_ARCHITEW6432 -eq 'ARM64') { $arch = 'arm64' }
$src = Join-Path $PSScriptRoot "dist\honjoji-windows-$arch.exe"

if (-not (Test-Path $src)) {
    if (Get-Command go -ErrorAction SilentlyContinue) {
        Write-Host 'No prebuilt binary for this machine, building with Go...'
        $src = Join-Path $env:TEMP 'honjoji-build.exe'
        Push-Location $PSScriptRoot
        try { go build -trimpath -ldflags '-s -w' -o $src . } finally { Pop-Location }
    } else {
        Write-Error "Missing $src. Try 'git pull' to fetch the prebuilt binaries."
    }
}

function Install-Exe($from, $to) {
    if (Test-Path $to) {
        # A running exe can't be overwritten but can be renamed, so updates work even while it's open.
        Remove-Item "$to.old" -Force -ErrorAction SilentlyContinue
        Rename-Item $to "$to.old" -Force
    }
    Copy-Item $from $to -Force
    Remove-Item "$to.old" -Force -ErrorAction SilentlyContinue
}

New-Item -ItemType Directory -Force $dir | Out-Null
Install-Exe $src $exe
$instant = $false
if (Test-Path $appsDir) {
    try { Install-Exe $src $appsExe; $instant = $true } catch { }
}

if (-not ($parts | Where-Object { $_.TrimEnd('\') -eq $dir })) {
    Set-UserPath ((@($parts) + $dir) -join ';')
}
if (-not (($env:Path -split ';') -contains $dir)) { $env:Path = "$env:Path;$dir" }

$claudeDir = if ($env:CLAUDE_CONFIG_DIR) { $env:CLAUDE_CONFIG_DIR } else { Join-Path $env:USERPROFILE '.claude' }
Write-Host ''
Write-Host "honjoji installed to $dir"
if (-not (Test-Path (Join-Path $claudeDir '.credentials.json'))) {
    Write-Host 'Note: no Claude Code login found yet. Run "claude" and log in; honjoji picks it up automatically.' -ForegroundColor Yellow
}
if ($instant) {
    Write-Host 'Type "honjoji" in any terminal to open it. Press Esc to quit.'
} else {
    Write-Host 'Type "honjoji" in a NEW terminal to open it (restart VS Code if you use its terminal). Press Esc to quit.'
}
Write-Host ''

if (-not $NoRun) { & $exe }
