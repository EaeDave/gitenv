# gitenv installer for Windows (PowerShell).
#
#   irm https://raw.githubusercontent.com/EaeDave/gitenv/main/install.ps1 | iex
#
# Environment overrides:
#   GITENV_VERSION      release tag to install (default: latest)
#   GITENV_INSTALL_DIR  install directory (default: %LOCALAPPDATA%\gitenv\bin)
#   GITENV_BASE_URL     release base URL (default: GitHub releases)
$ErrorActionPreference = 'Stop'

$repo = 'EaeDave/gitenv'
$baseUrl = if ($env:GITENV_BASE_URL) { $env:GITENV_BASE_URL } else { "https://github.com/$repo/releases" }
$version = if ($env:GITENV_VERSION) { $env:GITENV_VERSION } else { 'latest' }
$installDir = if ($env:GITENV_INSTALL_DIR) { $env:GITENV_INSTALL_DIR } else { Join-Path $env:LOCALAPPDATA 'gitenv\bin' }

function Info($msg) { Write-Host "==> $msg" -ForegroundColor Cyan }
function Fail($msg) { Write-Host "error: $msg" -ForegroundColor Red; exit 1 }

switch ($env:PROCESSOR_ARCHITECTURE) {
    'AMD64' { $arch = 'amd64' }
    'ARM64' { $arch = 'arm64' }
    default { Fail "unsupported architecture '$($env:PROCESSOR_ARCHITECTURE)'" }
}

$asset = "gitenv_windows_$arch.exe"
$urlDir = if ($version -eq 'latest') { "$baseUrl/latest/download" } else { "$baseUrl/download/$version" }

$tmp = Join-Path ([System.IO.Path]::GetTempPath()) ("gitenv-" + [System.Guid]::NewGuid().ToString('N'))
New-Item -ItemType Directory -Path $tmp -Force | Out-Null
try {
    $binPath = Join-Path $tmp 'gitenv.exe'
    $sumPath = Join-Path $tmp 'checksums.txt'

    Info "downloading $asset ($version)"
    Invoke-WebRequest -Uri "$urlDir/$asset" -OutFile $binPath -UseBasicParsing
    Invoke-WebRequest -Uri "$urlDir/checksums.txt" -OutFile $sumPath -UseBasicParsing

    Info 'verifying checksum'
    $expected = $null
    foreach ($line in Get-Content $sumPath) {
        $parts = $line -split '\s+', 2
        if ($parts.Count -eq 2 -and $parts[1].Trim() -eq $asset) { $expected = $parts[0].Trim() }
    }
    if (-not $expected) { Fail "no checksum listed for $asset" }
    $actual = (Get-FileHash -Path $binPath -Algorithm SHA256).Hash
    if ($actual -ine $expected) { Fail "checksum mismatch for $asset (expected $expected, got $actual)" }

    Info "installing to $installDir\gitenv.exe"
    New-Item -ItemType Directory -Path $installDir -Force | Out-Null
    Move-Item -Path $binPath -Destination (Join-Path $installDir 'gitenv.exe') -Force

    $userPath = [Environment]::GetEnvironmentVariable('Path', 'User')
    if (($userPath -split ';') -notcontains $installDir) {
        Info "adding $installDir to your user PATH"
        $newPath = if ([string]::IsNullOrEmpty($userPath)) { $installDir } else { "$userPath;$installDir" }
        [Environment]::SetEnvironmentVariable('Path', $newPath, 'User')
        $env:Path = "$env:Path;$installDir"
        Write-Host "note: open a new terminal for the updated PATH to take effect." -ForegroundColor Yellow
    }

    Info "installed $(& (Join-Path $installDir 'gitenv.exe') version)"
}
finally {
    Remove-Item -Path $tmp -Recurse -Force -ErrorAction SilentlyContinue
}
