<#
.SYNOPSIS
    Simulates a brand-new user installing and setting up opal-downloader from scratch,
    following only the README.md Installation / Quick Start steps.

.DESCRIPTION
    Covers everything that can be validated WITHOUT real OPAL credentials:
      1. Fresh clone (or local copy) of the repo into a temp directory.
      2. Install Playwright browser binaries (pinned version from go.mod).
      3. Build the binary exactly as documented in the README.
      4. Run `init` and verify config.yaml is created from config.example.yaml.
      5. Edit config.yaml with plausible placeholder values and verify the binary
         parses it without a config error.
      6. Verify malformed/missing config produce clear, actionable error messages.
      7. Verify `list`/`sync` do NOT fail with a config-parsing error once config
         is valid (they instead proceed to the auth/browser stage - see NOTE below).

    NOTE on the auth-error check: with no saved session state file, `list`/`sync`
    do not fail fast with a clean "please log in" error. They instead attempt to
    open a real interactive browser and wait for login (see docs/setup-friction.md).
    This script therefore verifies the safe, offline-observable part of that
    behavior (config loads cleanly and the command proceeds past config parsing)
    by pointing at an unroutable URL so the browser layer fails fast on its own,
    without ever contacting the real OPAL server or requiring a GUI login.

    This script never touches the real OPAL site, never requires credentials, and
    never touches your existing repo checkout's config.yaml / sync_run.log / built
    binaries - it always clones and builds into a fresh temp directory.

.PARAMETER RepoUrl
    Git URL to clone. Defaults to the canonical GitHub repo.

.PARAMETER SkipClone
    If set, copies the local repository (git working tree) into the temp directory
    instead of cloning from GitHub. Useful for offline runs / testing local changes.

.PARAMETER KeepWorkDir
    If set, do not delete the temporary working directory at the end (useful for
    manual inspection after a failure).

.EXAMPLE
    ./scripts/test-fresh-install.ps1

.EXAMPLE
    ./scripts/test-fresh-install.ps1 -SkipClone -KeepWorkDir
#>

param(
    [string]$RepoUrl = "https://github.com/alu-developer/Opal_downloader.git",
    [switch]$SkipClone,
    [switch]$KeepWorkDir
)

$ErrorActionPreference = "Stop"

$script:StepNumber = 0
$script:WorkDir = $null
# $IsWindows is only defined automatically on PowerShell Core (6+). On Windows
# PowerShell 5.1, $env:OS is the reliable check.
$IsWindowsPlatform = ($env:OS -eq "Windows_NT") -or ($PSVersionTable.PSVersion.Major -ge 6 -and $IsWindows)

function Write-Step {
    param([string]$Message)
    $script:StepNumber++
    Write-Host ""
    Write-Host "[$script:StepNumber] $Message" -ForegroundColor Cyan
}

function Write-Ok {
    param([string]$Message)
    Write-Host "    OK: $Message" -ForegroundColor Green
}

function Write-Note {
    param([string]$Message)
    Write-Host "    NOTE: $Message" -ForegroundColor Yellow
}

function Invoke-Binary {
    <#
    Runs a built binary reliably and returns @{ ExitCode; StdOut; StdErr; Combined }.

    We deliberately use Start-Process + redirected files rather than the call
    operator (`& ".\opal-downloader" ...`). During this dry run we found that
    invoking the extensionless binary produced by `go build -o opal-downloader .`
    (exactly the README's command, on Windows) via `& ".\opal-downloader" ...`
    does not reliably execute in PowerShell - it returns without running the
    program and without an exit code, while the identical file renamed to
    `opal-downloader.exe` runs instantly. Start-Process resolves and launches
    the file correctly either way, so we use it here to keep the rest of this
    script's checks reliable regardless of that issue (which is itself flagged
    as a friction point - see docs/setup-friction.md).
    #>
    param(
        [string]$Path,
        [string[]]$Arguments = @()
    )
    $stdoutFile = [System.IO.Path]::GetTempFileName()
    $stderrFile = [System.IO.Path]::GetTempFileName()
    try {
        $p = Start-Process -FilePath $Path -ArgumentList $Arguments -NoNewWindow -PassThru `
            -RedirectStandardOutput $stdoutFile -RedirectStandardError $stderrFile
        # Accessing .Handle forces .NET to cache process info; without this,
        # .ExitCode reads back empty after WaitForExit() in some PowerShell
        # versions when combined with -PassThru (a known .NET/PowerShell quirk).
        $null = $p.Handle
        $finished = $p.WaitForExit(60000)
        $stdout = Get-Content $stdoutFile -Raw -ErrorAction SilentlyContinue
        $stderr = Get-Content $stderrFile -Raw -ErrorAction SilentlyContinue
        if (-not $finished) {
            Stop-Process -Id $p.Id -Force -ErrorAction SilentlyContinue
            return @{ ExitCode = -1; StdOut = $stdout; StdErr = $stderr; Combined = "$stdout`n$stderr"; TimedOut = $true }
        }
        return @{ ExitCode = $p.ExitCode; StdOut = $stdout; StdErr = $stderr; Combined = "$stdout`n$stderr"; TimedOut = $false }
    } finally {
        Remove-Item $stdoutFile, $stderrFile -ErrorAction SilentlyContinue
    }
}

function Fail {
    param([string]$Message)
    Write-Host ""
    Write-Host "FAILED: $Message" -ForegroundColor Red
    if ($script:WorkDir -and (Test-Path $script:WorkDir) -and -not $KeepWorkDir) {
        Write-Host "Cleaning up temp dir before exit: $script:WorkDir"
        Remove-Item -Recurse -Force $script:WorkDir -ErrorAction SilentlyContinue
    }
    exit 1
}

function Invoke-Checked {
    param(
        [string]$Description,
        [scriptblock]$Action
    )
    # Native commands (git, go) write normal progress/info to stderr. Under
    # $ErrorActionPreference = 'Stop' that stderr chatter would otherwise be
    # promoted into a terminating PowerShell error even on exit code 0, so we
    # relax EAP just for the duration of the native call and check
    # $LASTEXITCODE ourselves instead. Output is captured (not streamed
    # directly to the host) so stderr lines render as plain text, not as
    # red NativeCommandError records.
    $previousEap = $ErrorActionPreference
    $ErrorActionPreference = "Continue"
    $global:LASTEXITCODE = 0
    try {
        $output = & $Action 2>&1 | ForEach-Object { "$_" }
    } catch {
        $ErrorActionPreference = $previousEap
        Fail "$Description threw an exception: $_"
    }
    $ErrorActionPreference = $previousEap
    if ($output) {
        $output | Write-Host
    }
    if ($null -ne $LASTEXITCODE -and $LASTEXITCODE -ne 0) {
        Fail "$Description failed with exit code $LASTEXITCODE"
    }
}

# ---------------------------------------------------------------------------
# Step 0: locate tools and the source repo, create an isolated temp workdir
# ---------------------------------------------------------------------------
Write-Step "Preparing isolated temp working directory"

$goCmd = Get-Command go -ErrorAction SilentlyContinue
if (-not $goCmd) {
    Fail "Go executable not found on PATH. Install Go 1.23+ and retry. (This is exactly the kind of prerequisite a brand-new user could hit first.)"
}
Write-Ok "Found Go: $($goCmd.Source) ($(& $goCmd.Source version))"

$gitCmd = Get-Command git -ErrorAction SilentlyContinue
if (-not $SkipClone -and -not $gitCmd) {
    Fail "git executable not found on PATH, and -SkipClone was not specified."
}

$sourceRepoRoot = (Resolve-Path (Join-Path $PSScriptRoot "..")).Path

$tempRoot = [System.IO.Path]::GetTempPath()
$runId = [Guid]::NewGuid().ToString("N").Substring(0, 8)
$script:WorkDir = Join-Path $tempRoot "opal-fresh-install-test-$runId"
New-Item -ItemType Directory -Path $script:WorkDir -Force | Out-Null
Write-Ok "Working directory: $script:WorkDir"

$repoDir = Join-Path $script:WorkDir "opal-downloader"

# ---------------------------------------------------------------------------
# Step 1: fresh clone (or local copy) - simulates `git clone` from the README
# ---------------------------------------------------------------------------
if ($SkipClone) {
    Write-Step "Copying local repository into temp dir (-SkipClone) instead of cloning"
    if (Get-Command robocopy -ErrorAction SilentlyContinue) {
        robocopy $sourceRepoRoot $repoDir /E /XD .git .claude tmp /NFL /NDL /NJH /NJS /NC /NS | Out-Null
        # robocopy exit codes 0-7 are success/informational
        if ($LASTEXITCODE -ge 8) {
            Fail "robocopy failed while copying local repo (exit $LASTEXITCODE)"
        }
    } else {
        Copy-Item -Path $sourceRepoRoot -Destination $repoDir -Recurse -Force
    }
    Write-Ok "Copied local repo to $repoDir"
} else {
    Write-Step "Cloning repo exactly as documented in README.md: git clone $RepoUrl"
    Invoke-Checked "git clone" { git clone $RepoUrl $repoDir }
    Write-Ok "Cloned into $repoDir"
}

if (-not (Test-Path (Join-Path $repoDir "go.mod"))) {
    Fail "go.mod not found after clone/copy - repository layout looks wrong."
}

Push-Location $repoDir
try {

    # -----------------------------------------------------------------------
    # Step 2: determine the pinned playwright-go version from go.mod and
    # install Playwright browser binaries, exactly as the README instructs.
    # -----------------------------------------------------------------------
    Write-Step "Reading pinned playwright-go version from go.mod"
    $goModContent = Get-Content "go.mod" -Raw
    $match = [regex]::Match($goModContent, "github\.com/mxschmitt/playwright-go\s+(v[0-9][A-Za-z0-9\.\-]*)")
    if (-not $match.Success) {
        Fail "Could not find github.com/mxschmitt/playwright-go version in go.mod. README install command may be out of date."
    }
    $playwrightVersion = $match.Groups[1].Value
    Write-Ok "go.mod pins playwright-go $playwrightVersion"

    $readmePath = Join-Path $repoDir "README.md"
    $readmeContent = Get-Content $readmePath -Raw
    if ($readmeContent -notmatch [regex]::Escape("playwright-go/cmd/playwright@$playwrightVersion")) {
        Write-Note "README.md install command does not reference playwright-go@$playwrightVersion (the version currently pinned in go.mod). Consider updating the README so copy-pasted install commands stay in sync with go.mod."
    } else {
        Write-Ok "README install command matches go.mod-pinned version"
    }

    Write-Step "Installing Playwright browser binaries: go run github.com/mxschmitt/playwright-go/cmd/playwright@$playwrightVersion install"
    Invoke-Checked "playwright install" {
        go run "github.com/mxschmitt/playwright-go/cmd/playwright@$playwrightVersion" install
    }
    Write-Ok "Playwright install command completed (exit 0)"
    Write-Note "Upstream's playwright CLI prints nothing on success even when it downloads ~470 MB. Not a gap in this tool any more: 'setup' calls playwright.Install directly and prints 'Installing Playwright browser binaries...' then 'Playwright browsers ready.' This step exercises the raw README command, so the silence here is upstream's."

    # -----------------------------------------------------------------------
    # Step 3: build the binary exactly as documented
    # -----------------------------------------------------------------------
    Write-Step "Building binary: go build -o opal-downloader . (exact README command)"
    Invoke-Checked "go build" { go build -o opal-downloader . }

    $binName = "opal-downloader.exe"
    if (-not (Test-Path $binName)) {
        # go build -o opal-downloader . on Windows does NOT append .exe - it
        # produces a literal extensionless file named "opal-downloader".
        $binName = "opal-downloader"
    }
    if (-not (Test-Path $binName)) {
        Fail "Build did not produce an opal-downloader(.exe) binary."
    }
    Write-Ok "Built $binName"

    if ($binName -eq "opal-downloader" -and $IsWindowsPlatform) {
        $noteMsg = 'On Windows, go build -o opal-downloader . produces an EXTENSIONLESS file that PowerShell''s call operator does not reliably execute (returns with no output/exit code), while the same file renamed to .exe runs immediately. This script builds the extensionless form deliberately, to keep proving that the trap is real - README.md already documents the .exe form as the Windows build command and names this trap, so a user following the README is not exposed to it. Do not read this note as an open gap.'
        Write-Note $noteMsg
    }

    # -----------------------------------------------------------------------
    # Step 4: `init` - verify config.yaml gets created from config.example.yaml
    # -----------------------------------------------------------------------
    Write-Step "Running: ./$binName init"
    $binPath = Join-Path (Get-Location) $binName
    $result = Invoke-Binary -Path $binPath -Arguments @("init")
    Write-Host $result.Combined.Trim()
    if ($result.TimedOut) {
        Fail "'init' timed out / did not execute within 60s."
    }
    if ($result.ExitCode -ne 0) {
        Fail "'init' exited non-zero ($($result.ExitCode)) on a completely fresh checkout."
    }
    if (-not (Test-Path "config.yaml")) {
        Fail "'init' reported success but config.yaml was not created."
    }
    $configDiff = Compare-Object (Get-Content "config.yaml") (Get-Content "config.example.yaml")
    if ($configDiff) {
        Fail "'init'-generated config.yaml differs from config.example.yaml unexpectedly."
    }
    Write-Ok "config.yaml created from config.example.yaml (init is byte-identical to example, as expected)"

    if ($result.Combined -notmatch "(?i)next step") {
        Write-Note "'init' output does not include clear 'next steps' guidance."
    } else {
        Write-Ok "'init' output includes 'Next steps' guidance for a new user"
    }

    Write-Step "Running: ./$binName init a second time (idempotency check)"
    $result2 = Invoke-Binary -Path $binPath -Arguments @("init")
    Write-Host $result2.Combined.Trim()
    if ($result2.ExitCode -ne 0) {
        Fail "'init' exited non-zero when config.yaml already exists (should skip gracefully)."
    }
    if ($result2.Combined -notmatch "(?i)skip") {
        Write-Note "'init' re-run does not clearly say it skipped an existing config.yaml."
    } else {
        Write-Ok "'init' re-run clearly reports it skipped an existing config.yaml"
    }

    # -----------------------------------------------------------------------
    # Step 5: missing / malformed config error clarity (safe, offline, fast)
    # -----------------------------------------------------------------------
    Write-Step "Verifying error message when config.yaml is missing"
    Rename-Item "config.yaml" "config.yaml.bak"
    $missingResult = Invoke-Binary -Path $binPath -Arguments @("list")
    Rename-Item "config.yaml.bak" "config.yaml"
    if ($missingResult.ExitCode -eq 0) {
        Fail "'list' with no config.yaml should fail, but it exited 0."
    }
    if ($missingResult.Combined -match "config file not found") {
        Write-Ok "Missing-config error is clear and mentions the missing path: $($missingResult.Combined.Trim())"
    } else {
        Write-Note "Missing-config error message may not be clear to a new user: $($missingResult.Combined.Trim())"
    }

    Write-Step "Verifying error message when config.yaml is malformed YAML"
    Copy-Item "config.yaml" "config.yaml.bak"
    Set-Content -Path "config.yaml" -Value "download_path: `"./downloads`"`ncourses: [`n"
    $malformedResult = Invoke-Binary -Path $binPath -Arguments @("list")
    Move-Item -Force "config.yaml.bak" "config.yaml"
    if ($malformedResult.ExitCode -eq 0) {
        Fail "'list' with malformed YAML should fail, but it exited 0."
    }
    if ($malformedResult.Combined -match "invalid yaml") {
        Write-Ok "Malformed-YAML error is clear and names the file: $($malformedResult.Combined.Trim())"
    } else {
        Write-Note "Malformed-YAML error message may not be clear to a new user: $($malformedResult.Combined.Trim())"
    }

    # -----------------------------------------------------------------------
    # Step 6: edit config.yaml with plausible placeholder values and verify
    # the binary parses it without a CONFIG error. `list`/`sync` require a
    # real browser + real OPAL network access to go further than this, so we
    # point opal_url at an unroutable address to force a fast, safe failure
    # in the browser/network layer - proving config parsing itself succeeded.
    # -----------------------------------------------------------------------
    Write-Step "Writing plausible placeholder config.yaml (download_path, courses, course_folders)"
    $downloadPath = Join-Path $script:WorkDir "downloads"
    $placeholderConfig = @"
download_path: "$($downloadPath -replace '\\','/')"
default_course_folder: "default"
course_folders:
  "*Programmierung*": "Informatik/Programmierung"
  "*Analysis*": "Mathematik/Analysis"
courses:
  - "Lineare Algebra 2"
  - "Grundlagen der Programmierung"
sync: true
opal_url: "http://127.0.0.1:1/opal/"
session_state_file: "$($script:WorkDir -replace '\\','/')/opal_storage_state_test.json"
"@
    Set-Content -Path "config.yaml" -Value $placeholderConfig
    Write-Ok "Wrote placeholder config.yaml with realistic download_path/courses/course_folders"
    Write-Note "opal_url is temporarily pointed at an unroutable address (127.0.0.1:1) so this script can prove config parsing succeeds without ever contacting the real OPAL server or opening an interactive login window."

    Write-Step "Running: ./$binName list (expect it to pass config parsing, then fail in the browser/network layer)"
    $listResult = Invoke-Binary -Path $binPath -Arguments @("list")
    Write-Host $listResult.Combined.Trim()
    if ($listResult.TimedOut) {
        Fail "'list' timed out - it should have failed fast against an unroutable opal_url, not hung."
    }
    if ($listResult.ExitCode -eq 0) {
        Fail "'list' unexpectedly succeeded against an unroutable opal_url."
    }
    if ($listResult.Combined -match "invalid yaml" -or $listResult.Combined -match "config file not found") {
        Fail "'list' failed with a CONFIG error even though config.yaml is well-formed: $($listResult.Combined.Trim())"
    }
    Write-Ok "'list' did not fail with a config-parsing error (config.yaml with placeholder values parses fine)"
    # This used to report the raw Playwright text as the whole story. It is not,
    # and has not been since the unreachable-URL error got wrapped: check for the
    # friendly line rather than describing its absence, so the note can never
    # again contradict the output printed directly above it.
    $friendly = ($listResult.Combined -match "could not reach OPAL at")
    if ($friendly) {
        Write-Ok "the network failure is wrapped in a friendly, actionable line before the raw Playwright detail"
    } elseif ($listResult.Combined -match "ERR_UNSAFE_PORT|net::|playwright:") {
        Write-Note "REGRESSION: the failure at this stage is a bare Playwright/Chromium error ('$($Matches[0])...') with no friendly line. That wrapping existed as of 2026-07-30; see docs/setup-friction.md item 4."
    }
    Write-Note "Still true, and needs credentials to see: with a valid opal_url but no saved session state file, 'list'/'sync' do NOT fail fast - they open a real interactive browser and wait up to 5 minutes for login. 'status' answers the same question offline, but only if the user thinks to ask it. See docs/manual-setup-checklist.md."

    Write-Host ""
    Write-Host "All automatable checks passed." -ForegroundColor Green

} finally {
    Pop-Location
}

# ---------------------------------------------------------------------------
# Cleanup
# ---------------------------------------------------------------------------
if ($KeepWorkDir) {
    Write-Host ""
    Write-Host "Leaving working directory in place for inspection: $script:WorkDir" -ForegroundColor Yellow
} else {
    Write-Host ""
    Write-Host "Cleaning up temp working directory: $script:WorkDir"
    Remove-Item -Recurse -Force $script:WorkDir -ErrorAction SilentlyContinue
}

exit 0
