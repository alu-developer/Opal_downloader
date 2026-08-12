# Usage: scripts\build-installer.ps1 [-Version vX.Y.Z] [-ChromiumCacheSrc <path>] [-ChromiumSrcDir <path>]
#
# Produces installer\output\opal-downloader-setup.exe from a clean checkout:
# builds opal-downloader.exe with the version embedded via -ldflags (see
# cmd/opal-downloader/root.go's buildVersion), stages a copy of the local
# Playwright Chromium cache into installer\chromium-cache (the layout
# installer\opal-downloader.iss expects - see its header comment), and
# invokes Inno Setup's `iscc` to compile the installer.
#
# Requires Inno Setup 6 installed locally (iscc.exe on PATH or in one of the
# usual install locations) and a populated
# %USERPROFILE%\.opal-downloader\ms-playwright (run `opal-downloader.exe
# setup` once beforehand if it's empty - see docs/installer-plan.md Section 9
# task 4). This is where EnsurePlaywrightBrowsersPath
# (internal/scraper/session.go) actually points PLAYWRIGHT_BROWSERS_PATH,
# not %LOCALAPPDATA%\ms-playwright - see docs/installer-plan.md's addendum.
#
# This is a local/manual release-build helper (docs/installer-plan.md
# Section 9 task 4) - no CI/release-workflow automation is in scope here.

param(
    # Version string embedded into opal-downloader.exe's --version output via
    # -ldflags. Defaults to the installer script's own MyAppVersion so the
    # binary and the installer's AppVersion stay in sync without having to
    # pass it twice.
    [string]$Version,

    # Directory containing an existing local Playwright Chromium cache to
    # stage from, i.e. the folder that contains "chromium-<rev>" and
    # "chromium_headless_shell-<rev>" subfolders. Defaults to the real
    # %USERPROFILE%\.opal-downloader\ms-playwright (acceptance criterion (a):
    # reuse an existing local cache if present) - matches
    # EnsurePlaywrightBrowsersPath's default in internal/scraper/session.go,
    # NOT %LOCALAPPDATA%\ms-playwright.
    [string]$ChromiumCacheSrc = (Join-Path $env:USERPROFILE ".opal-downloader\ms-playwright"),

    # Where the Chromium cache gets staged to for the .iss script to pick up
    # (matches ChromiumSrcDir's default in installer\opal-downloader.iss).
    [string]$ChromiumSrcDir
)

$ErrorActionPreference = "Stop"

$repoRoot = Split-Path $PSScriptRoot -Parent
$installerDir = Join-Path $repoRoot "installer"
$issPath = Join-Path $installerDir "opal-downloader.iss"

if (-not (Test-Path $issPath)) {
    throw "installer\opal-downloader.iss not found at $issPath"
}

if (-not $ChromiumSrcDir) {
    $ChromiumSrcDir = Join-Path $installerDir "chromium-cache"
}

# --- Resolve go.exe (same fallback pattern as scripts\dev.ps1) ---
$go = "C:\Program Files\Go\bin\go.exe"
if (-not (Test-Path $go)) {
    $cmd = Get-Command go -ErrorAction SilentlyContinue
    if ($null -eq $cmd) {
        throw "Go executable not found. Install Go or add it to PATH."
    }
    $go = $cmd.Source
}

# --- Resolve iscc.exe ---
$iscc = $null
$isccCmd = Get-Command iscc -ErrorAction SilentlyContinue
if ($isccCmd) {
    $iscc = $isccCmd.Source
}
else {
    $candidates = @(
        (Join-Path $env:LOCALAPPDATA "Programs\Inno Setup 6\ISCC.exe"),
        "C:\Program Files (x86)\Inno Setup 6\ISCC.exe",
        "C:\Program Files\Inno Setup 6\ISCC.exe"
    )
    foreach ($candidate in $candidates) {
        if (Test-Path $candidate) {
            $iscc = $candidate
            break
        }
    }
}
if (-not $iscc) {
    throw "iscc.exe (Inno Setup 6 command-line compiler) not found. Install Inno Setup 6 (https://jrsoftware.org/isinfo.php) or add ISCC.exe to PATH."
}

# --- Resolve version ---
if (-not $Version) {
    $issContent = Get-Content $issPath -Raw
    $match = [regex]::Match($issContent, '#define\s+MyAppVersion\s+"([^"]+)"')
    if ($match.Success) {
        $Version = "v" + $match.Groups[1].Value
    }
    else {
        $Version = "dev"
    }
}

# Inno Setup's AppVersion (the string Apps & Features shows) wants the bare
# number, while $Version is a tag like "v0.1.1". Passed to iscc below as
# /DMyAppVersion so ONE input - the pushed tag - drives both the Go binary's
# buildVersion and the installer's AppVersion. Until 2026-08-12 only the
# former was wired up and the latter sat frozen at the .iss's committed
# literal, so a v0.1.1 build would have installed itself as "0.1.0".
$appVersion = $Version -replace '^v', ''
Write-Host "Building opal-downloader.exe version $Version (installer AppVersion $appVersion) ..."

Push-Location $repoRoot
try {
    # --- Step 1: build the binary ---
    $ldflags = "-X github.com/alu-developer/opal-downloader/cmd/opal-downloader.buildVersion=$Version"
    & $go build -ldflags $ldflags -o (Join-Path $repoRoot "opal-downloader.exe") .
    if ($LASTEXITCODE -ne 0) {
        throw "go build failed with exit code $LASTEXITCODE"
    }

    # --- Step 2: stage the Chromium cache ---
    # installer\opal-downloader.iss needs both "chromium-<rev>" AND
    # "chromium_headless_shell-<rev>" staged (see its header comment) -
    # opal-downloader launches Chromium headless for list/sync session reuse,
    # which resolves to chrome-headless-shell.exe under the
    # chromium_headless_shell-<rev> folder, not chrome.exe.
    $requiredPrefixes = @("chromium-", "chromium_headless_shell-")

    if (-not (Test-Path $ChromiumCacheSrc)) {
        Write-Warning "Chromium cache source '$ChromiumCacheSrc' does not exist. Run 'opal-downloader.exe setup' once to populate it, then re-run this script. Continuing without staging Chromium - the resulting installer will fall back to running 'setup' post-install (requires internet on the target machine; no Go toolchain needed - runSetup calls playwright-go's Install() API directly)."
    }
    else {
        if (Test-Path $ChromiumSrcDir) {
            Remove-Item -Recurse -Force $ChromiumSrcDir
        }
        New-Item -ItemType Directory -Force -Path $ChromiumSrcDir | Out-Null

        $stagedAny = $false
        foreach ($prefix in $requiredPrefixes) {
            $dirs = Get-ChildItem -Path $ChromiumCacheSrc -Directory -Filter "$prefix*" -ErrorAction SilentlyContinue
            if (-not $dirs -or $dirs.Count -eq 0) {
                Write-Warning "No '$prefix*' folder found under '$ChromiumCacheSrc' - the compiled installer will be missing this piece and will fall back to 'setup' post-install."
                continue
            }
            foreach ($dir in $dirs) {
                $dest = Join-Path $ChromiumSrcDir $dir.Name
                Write-Host "Staging $($dir.Name) -> $dest ..."
                Copy-Item -Recurse -Force -Path $dir.FullName -Destination $dest
                $stagedAny = $true
            }
        }
        if (-not $stagedAny) {
            Write-Warning "Nothing staged from '$ChromiumCacheSrc' - installer will not bundle Chromium."
        }
    }

    # --- Step 3: invoke iscc ---
    Write-Host "Compiling installer with iscc ..."
    $issArgs = @("/DMyAppVersion=$appVersion")
    if ($ChromiumSrcDir -ne (Join-Path $installerDir "chromium-cache")) {
        $issArgs += "/DChromiumSrcDir=$ChromiumSrcDir"
    }
    $issArgs += $issPath
    & $iscc @issArgs
    if ($LASTEXITCODE -ne 0) {
        throw "iscc failed with exit code $LASTEXITCODE"
    }

    $outputExe = Join-Path $installerDir "output\opal-downloader-setup.exe"
    if (-not (Test-Path $outputExe)) {
        throw "Expected output not found at $outputExe"
    }
    $sizeMb = [math]::Round((Get-Item $outputExe).Length / 1MB, 1)
    Write-Host "Built $outputExe ($sizeMb MB)"
}
finally {
    Pop-Location
}
