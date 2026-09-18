# Install wtf on Windows (downloads latest release to a PATH folder).
#   irm https://github.com/ParthKadam11/WTFisRunning/releases/latest/download/i.ps1 | iex
[CmdletBinding()]
param()

$ErrorActionPreference = "Stop"
$Repo = "ParthKadam11/WTFisRunning"
$Bin = "wtf"

function Get-Arch {
    switch ($env:PROCESSOR_ARCHITECTURE) {
        "AMD64" { return "amd64" }
        "ARM64" { return "arm64" }
        default {
            throw "unsupported architecture: $($env:PROCESSOR_ARCHITECTURE)"
        }
    }
}

$arch = Get-Arch
$asset = "${Bin}_windows_${arch}.zip"
$url = "https://github.com/$Repo/releases/latest/download/$asset"

$dest = Join-Path $env:LOCALAPPDATA "Programs\wtf"
New-Item -ItemType Directory -Force -Path $dest | Out-Null

$tmp = Join-Path ([System.IO.Path]::GetTempPath()) ("wtf-install-" + [guid]::NewGuid().ToString("N"))
New-Item -ItemType Directory -Force -Path $tmp | Out-Null

try {
    Write-Host "downloading $asset…"
    $zip = Join-Path $tmp $asset
    Invoke-WebRequest -Uri $url -OutFile $zip -UseBasicParsing

    Expand-Archive -Path $zip -DestinationPath $tmp -Force
    $exe = Get-ChildItem -Path $tmp -Filter "wtf.exe" -Recurse | Select-Object -First 1
    if (-not $exe) {
        throw "wtf.exe not found in release archive"
    }

    $target = Join-Path $dest "wtf.exe"
    Copy-Item -Path $exe.FullName -Destination $target -Force
    Write-Host "installed: $target"

    $userPath = [Environment]::GetEnvironmentVariable("Path", "User")
    $parts = @()
    if ($userPath) {
        $parts = $userPath -split ";" | Where-Object { $_ -and $_.Trim() -ne "" }
    }
    if ($parts -notcontains $dest) {
        $newPath = ($parts + $dest) -join ";"
        [Environment]::SetEnvironmentVariable("Path", $newPath, "User")
        $env:Path = "$dest;$env:Path"
        Write-Host "added to user PATH: $dest"
        Write-Host "(open a new terminal if 'wtf' is still not found)"
    }

    if (-not (Get-Command ssh -ErrorAction SilentlyContinue)) {
        Write-Host ""
        Write-Host "warning: OpenSSH 'ssh' not found on PATH."
        Write-Host "install it: Settings → Apps → Optional features → OpenSSH Client"
        Write-Host "or: Add-WindowsCapability -Online -Name OpenSSH.Client~~~~0.0.1.0"
    }

    Write-Host ""
    Write-Host "try:  wtf user@host"
    Write-Host "  or: wtf user@host --once"
}
finally {
    Remove-Item -Recurse -Force $tmp -ErrorAction SilentlyContinue
}
