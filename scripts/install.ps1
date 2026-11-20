# Installer for tui-ssh on Windows.
#
#   irm https://raw.githubusercontent.com/MiguelFPP/tui-ssh/main/scripts/install.ps1 | iex
#
# Options:
#   -Version vX.Y.Z  Version to install (default: latest release).
#   -Dir PATH        Install directory (default: %LOCALAPPDATA%\Programs\tui-ssh).
#   -NoVerify        Skip the SHA-256 checksum verification.
#   -NoPath          Do not add the install directory to the user PATH.

[CmdletBinding()]
param(
    [string]$Version = "",
    [string]$Dir = "",
    [switch]$NoVerify,
    [switch]$NoPath
)

$ErrorActionPreference = "Stop"

$Repo = "MiguelFPP/tui-ssh"
$Releases = "https://github.com/$Repo/releases"
$Bin = "tui-ssh"

function Say($Message) { Write-Host $Message }
function Warn($Message) { Write-Warning "tui-ssh: $Message" }

function Resolve-Arch {
    switch ($env:PROCESSOR_ARCHITECTURE) {
        "AMD64" { return "amd64" }
        "ARM64" { return "arm64" }
        default { throw "arquitectura no soportada: $env:PROCESSOR_ARCHITECTURE" }
    }
}

function Resolve-Version {
    if ($Version) {
        return $Version.TrimStart("v")
    }

    $latest = Invoke-RestMethod "https://api.github.com/repos/$Repo/releases/latest"
    if (-not $latest.tag_name) {
        throw "no se pudo determinar la última versión; usa -Version vX.Y.Z"
    }

    return $latest.tag_name.TrimStart("v")
}

function Verify-Checksum($Archive, $Expected) {
    $actual = (Get-FileHash -Algorithm SHA256 -Path $Archive).Hash.ToLowerInvariant()
    if ($actual -ne $Expected.ToLowerInvariant()) {
        throw "el checksum no coincide para $Archive (esperado $Expected, obtenido $actual)"
    }
}

$arch = Resolve-Arch
$version = Resolve-Version

if (-not $Dir) {
    $Dir = Join-Path $env:LOCALAPPDATA "Programs\tui-ssh"
}

$name = "${Bin}_${version}_windows_${arch}"
$url = "$Releases/download/v$version/$name.zip"

Say "  versión:    $version"
Say "  plataforma: windows/$arch"
Say "  destino:    $Dir"

$tmp = Join-Path ([System.IO.Path]::GetTempPath()) ("tui-ssh-" + [guid]::NewGuid().ToString("N"))
New-Item -ItemType Directory -Path $tmp -Force | Out-Null

try {
    Say "→ descargando $name.zip"
    $archive = Join-Path $tmp "$name.zip"
    Invoke-WebRequest -Uri $url -OutFile $archive

    if (-not $NoVerify) {
        try {
            $checksums = Join-Path $tmp "checksums.txt"
            Invoke-WebRequest -Uri "$Releases/download/v$version/checksums.txt" -OutFile $checksums
            $expected = (Get-Content $checksums | Where-Object { $_ -match "\s$([regex]::Escape($name)).zip$" } | ForEach-Object { ($_ -split "\s+")[0] } | Select-Object -First 1)
            if ($expected) {
                Say "→ verificando checksum"
                Verify-Checksum $archive $expected
            }
            else {
                Warn "$name.zip no aparece en checksums.txt; se omite la verificación"
            }
        }
        catch {
            Warn "no se pudo verificar el checksum: $_"
        }
    }

    Say "→ extrayendo"
    Expand-Archive -Path $archive -DestinationPath $tmp -Force

    New-Item -ItemType Directory -Path $Dir -Force | Out-Null

    # Copy next to the target and rename: the move replaces the file, so an update
    # also works while the current binary is running.
    $staged = Join-Path $Dir ".$Bin.new"
    Copy-Item (Join-Path $tmp "$Bin.exe") $staged -Force
    Move-Item $staged (Join-Path $Dir "$Bin.exe") -Force

    Say "✓ instalado $Bin $version en $Dir\$Bin.exe"

    if (-not $NoPath) {
        $userPath = [Environment]::GetEnvironmentVariable("Path", "User")
        if ($userPath -notlike "*$Dir*") {
            [Environment]::SetEnvironmentVariable("Path", "$userPath;$Dir", "User")
            Warn "añadido $Dir al PATH de usuario; abre una terminal nueva"
        }
    }
}
finally {
    Remove-Item -Recurse -Force $tmp -ErrorAction SilentlyContinue
}
