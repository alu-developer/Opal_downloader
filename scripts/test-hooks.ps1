# Tests for the .claude/hooks/ budget + autopilot machinery.
#
# These hooks are the safety net around autonomous runs, and until 2026-07-23
# they had no tests at all - which is how rate-limit-gate.ps1 shipped with no
# freshness check, and how keep-warm's 42s wait sat inside a 15s hook timeout
# silently killing autopilot. Both were found by reading, not by failing, which
# is not a repeatable way to find the next one.
#
# ISOLATION IS NOT OPTIONAL. Every test runs against a throwaway queue
# directory (OPAL_AUTOPILOT_QUEUE_DIR) and a synthetic rate-limit status file
# (OPAL_RATE_LIMIT_STATUS). On 2026-07-21 a verification run cleaned up after
# itself by deleting the real AUTOPILOT marker and both state files, killing a
# live autopilot run and taking the session record with it - the one thing that
# defeats the restore-on-delete protection. Never point these at the real
# queue.
#
# Hooks are invoked as the harness invokes them: a child powershell.exe with
# JSON on real stdin. Dot-sourcing instead would let [Console]::In read this
# process's console, which is not the thing under test.

param(
    [switch]$VerboseOutput
)

$ErrorActionPreference = "Stop"
Set-StrictMode -Off

$repoRoot = Split-Path $PSScriptRoot -Parent
$hooksDir = Join-Path $repoRoot ".claude\hooks"

$script:passed = 0
$script:failed = 0
$script:failures = @()

function Assert-That {
    param([string]$Name, [bool]$Condition, [string]$Detail = "")
    if ($Condition) {
        $script:passed++
        if ($VerboseOutput) { Write-Host "  PASS  $Name" -ForegroundColor DarkGreen }
    } else {
        $script:failed++
        $script:failures += "$Name$(if ($Detail) { " - $Detail" })"
        Write-Host "  FAIL  $Name" -ForegroundColor Red
        if ($Detail) { Write-Host "        $Detail" -ForegroundColor DarkRed }
    }
}

# --- sandbox ------------------------------------------------------------------
$sandbox = Join-Path ([System.IO.Path]::GetTempPath()) "opal-hook-tests-$([guid]::NewGuid().ToString('N').Substring(0,8))"
New-Item -ItemType Directory -Path $sandbox -Force | Out-Null
$queueDir = Join-Path $sandbox "queue"
New-Item -ItemType Directory -Path $queueDir -Force | Out-Null
$statusFile = Join-Path $sandbox "rate-limit-status.json"

$env:OPAL_AUTOPILOT_QUEUE_DIR = $queueDir
$env:OPAL_RATE_LIMIT_STATUS = $statusFile

function Set-Status {
    <#  Writes a synthetic rate-limit-status.json.
        Offsets are seconds relative to now: a POSITIVE resets_at is a window
        still running, a NEGATIVE one has already rolled over. #>
    param(
        $FivePct, [int]$FiveResetsIn = 3600,
        $SevenPct, [int]$SevenResetsIn = 86400,
        [int]$AgeSeconds = 0,
        [switch]$OmitResetsAt
    )
    $now = [DateTimeOffset]::UtcNow.ToUnixTimeSeconds()
    $five = @{ used_percentage = $FivePct }
    $seven = @{ used_percentage = $SevenPct }
    if (-not $OmitResetsAt) {
        $five.resets_at = $now + $FiveResetsIn
        $seven.resets_at = $now + $SevenResetsIn
    }
    $obj = @{ five_hour = $five; seven_day = $seven; updated_at = ($now - $AgeSeconds) }
    $obj | ConvertTo-Json -Depth 5 | Set-Content $statusFile -Encoding utf8
}

function Invoke-Hook {
    <#  Runs a hook the way the harness does: child process, JSON on stdin.
        Returns the raw stdout. #>
    param([string]$Script, [hashtable]$Payload)
    $json = $Payload | ConvertTo-Json -Depth 5 -Compress
    $path = Join-Path $hooksDir $Script
    $out = $json | powershell.exe -NoProfile -ExecutionPolicy Bypass -File $path 2>$null
    return ($out -join "`n")
}

try {
    # =========================================================================
    Write-Host "`nbudget-lib: Get-BudgetFloor" -ForegroundColor Cyan
    # =========================================================================
    . (Join-Path $hooksDir "budget-lib.ps1")

    Set-Status -FivePct 42 -SevenPct 30
    $b = Get-BudgetFloor -StatusPath $statusFile
    Assert-That "fresh, live windows are usable" ($b.Known -and $b.FiveHour -eq 42 -and $b.SevenDay -eq 30) "got Known=$($b.Known) five=$($b.FiveHour) seven=$($b.SevenDay)"

    # The floor property: an old reading inside a live window is still valid,
    # because usage only climbs within a window.
    Set-Status -FivePct 60 -SevenPct 20 -AgeSeconds 7200
    $b = Get-BudgetFloor -StatusPath $statusFile
    Assert-That "2h-old reading in a live window is still a usable floor" ($b.Known -and $b.FiveHour -eq 60) "got Known=$($b.Known) five=$($b.FiveHour)"

    # The bug rate-limit-gate.ps1 had: an expired window's number describes a
    # window that no longer exists. Trusting it would gate every future run
    # forever, long after the quota came back.
    Set-Status -FivePct 99 -FiveResetsIn -60 -SevenPct 12 -AgeSeconds 7200
    $b = Get-BudgetFloor -StatusPath $statusFile
    Assert-That "expired 5h window reads as unknown, not as 99%" ($null -eq $b.FiveHour) "got five=$($b.FiveHour)"
    Assert-That "a live 7d window still counts when 5h expired" ($b.SevenDay -eq 12) "got seven=$($b.SevenDay)"

    Set-Status -FivePct 99 -FiveResetsIn -60 -SevenPct 98 -SevenResetsIn -60
    $b = Get-BudgetFloor -StatusPath $statusFile
    Assert-That "both windows expired => Known false" (-not $b.Known) "got Known=$($b.Known)"

    Set-Status -FivePct 55 -SevenPct 44 -OmitResetsAt -AgeSeconds 10
    $b = Get-BudgetFloor -StatusPath $statusFile
    Assert-That "no resets_at but fresh => falls back to plain freshness" ($b.Known -and $b.FiveHour -eq 55) "got Known=$($b.Known)"

    Set-Status -FivePct 55 -SevenPct 44 -OmitResetsAt -AgeSeconds 1200
    $b = Get-BudgetFloor -StatusPath $statusFile
    Assert-That "no resets_at and stale => unknown" (-not $b.Known) "got Known=$($b.Known)"

    "not json at all" | Set-Content $statusFile -Encoding utf8
    $b = Get-BudgetFloor -StatusPath $statusFile
    Assert-That "unreadable status file => unknown, no throw" (-not $b.Known) "got Known=$($b.Known)"

    $b = Get-BudgetFloor -StatusPath (Join-Path $sandbox "does-not-exist.json")
    Assert-That "missing status file => unknown, no throw" (-not $b.Known) "got Known=$($b.Known)"

    # =========================================================================
    Write-Host "`nbudget-lib: Get-BudgetRung" -ForegroundColor Cyan
    # =========================================================================
    function Rung-For { param($f, $s) Get-BudgetRung ([pscustomobject]@{ Known = $true; FiveHour = $f; SevenDay = $s }) }

    Assert-That "rung 0 below all thresholds" ((Rung-For 10 10) -eq 0)
    Assert-That "rung 1 at 5h=50" ((Rung-For 50 0) -eq 1)
    Assert-That "rung 1 at 7d=65" ((Rung-For 0 65) -eq 1)
    Assert-That "rung 2 at 5h=70" ((Rung-For 70 0) -eq 2)
    Assert-That "rung 2 at 7d=80" ((Rung-For 0 80) -eq 2)
    Assert-That "rung 3 at 5h=80" ((Rung-For 80 0) -eq 3)
    Assert-That "rung 3 at 7d=85" ((Rung-For 0 85) -eq 3)
    Assert-That "worst window wins" ((Rung-For 5 85) -eq 3)
    Assert-That "unknown budget => rung 0" ((Get-BudgetRung ([pscustomobject]@{ Known = $false })) -eq 0)
    Assert-That "null budget => rung 0, no throw" ((Get-BudgetRung $null) -eq 0)

    # =========================================================================
    Write-Host "`nbudget-guard (PreToolUse)" -ForegroundColor Cyan
    # =========================================================================
    $guardState = Join-Path $queueDir ".budget-guard-state.json"

    Set-Status -FivePct 5 -SevenPct 5
    Remove-Item $guardState -Force -ErrorAction SilentlyContinue
    $out = Invoke-Hook "budget-guard.ps1" @{ session_id = "s1"; tool_name = "Read" }
    Assert-That "quiet below thresholds" ([string]::IsNullOrWhiteSpace($out)) "got: $out"

    # Rung 2: advises, does not block.
    Set-Status -FivePct 72 -SevenPct 5
    Remove-Item $guardState -Force -ErrorAction SilentlyContinue
    $out = Invoke-Hook "budget-guard.ps1" @{ session_id = "s2"; tool_name = "Read" }
    Assert-That "rung 2 emits output" (-not [string]::IsNullOrWhiteSpace($out)) "got nothing"
    $parsed = $null
    try { $parsed = $out | ConvertFrom-Json } catch { }
    Assert-That "rung 2 output is valid JSON" ($null -ne $parsed) "got: $out"
    Assert-That "rung 2 injects additionalContext" ($parsed.hookSpecificOutput.additionalContext -match "BUDGET CHECKPOINT") "got: $($parsed.hookSpecificOutput.additionalContext)"
    Assert-That "rung 2 does NOT block the tool call" ($null -eq $parsed.hookSpecificOutput.permissionDecision) "got decision=$($parsed.hookSpecificOutput.permissionDecision)"

    # Throttle: the same rung must not re-announce on every tool call, or the
    # reminder costs more budget than it saves.
    $out2 = Invoke-Hook "budget-guard.ps1" @{ session_id = "s2"; tool_name = "Read" }
    Assert-That "same rung is throttled on the next call" ([string]::IsNullOrWhiteSpace($out2)) "got: $out2"

    # A rise in severity always speaks, throttle or not.
    Set-Status -FivePct 85 -SevenPct 5
    $out3 = Invoke-Hook "budget-guard.ps1" @{ session_id = "s2"; tool_name = "Read" }
    Assert-That "a rung increase breaks the throttle" ($out3 -match "BUDGET CRITICAL") "got: $out3"

    # Different sessions are throttled independently.
    Set-Status -FivePct 72 -SevenPct 5
    $out4 = Invoke-Hook "budget-guard.ps1" @{ session_id = "other-session"; tool_name = "Read" }
    Assert-That "throttle is per session" (-not [string]::IsNullOrWhiteSpace($out4)) "got nothing"

    # Rung 3 + Agent: the one hard deny.
    Set-Status -FivePct 85 -SevenPct 5
    Remove-Item $guardState -Force -ErrorAction SilentlyContinue
    $out = Invoke-Hook "budget-guard.ps1" @{ session_id = "s3"; tool_name = "Agent" }
    $parsed = $out | ConvertFrom-Json
    Assert-That "rung 3 denies a subagent launch" ($parsed.hookSpecificOutput.permissionDecision -eq "deny") "got: $out"

    Set-Status -FivePct 72 -SevenPct 5
    Remove-Item $guardState -Force -ErrorAction SilentlyContinue
    $out = Invoke-Hook "budget-guard.ps1" @{ session_id = "s4"; tool_name = "Agent" }
    $parsed = $out | ConvertFrom-Json
    Assert-That "rung 2 allows a subagent launch" ($null -eq $parsed.hookSpecificOutput.permissionDecision) "got: $out"

    # An expired window must not deny anything - the whole point of the
    # freshness rule.
    Set-Status -FivePct 99 -FiveResetsIn -60 -SevenPct 2 -AgeSeconds 7200
    Remove-Item $guardState -Force -ErrorAction SilentlyContinue
    $out = Invoke-Hook "budget-guard.ps1" @{ session_id = "s5"; tool_name = "Agent" }
    Assert-That "expired 99% window does not deny" ([string]::IsNullOrWhiteSpace($out)) "got: $out"

    # Malformed stdin must never break a tool call.
    $path = Join-Path $hooksDir "budget-guard.ps1"
    $out = "}{ not json" | powershell.exe -NoProfile -ExecutionPolicy Bypass -File $path 2>$null
    Assert-That "malformed stdin fails open" ($LASTEXITCODE -eq 0) "exit=$LASTEXITCODE"

    # =========================================================================
    Write-Host "`nturn-failure-checkpoint (StopFailure)" -ForegroundColor Cyan
    # =========================================================================
    $failureFile = Join-Path $queueDir "LAST_FAILURE.json"
    $failureLog = Join-Path $queueDir "turn-failures.log"
    Remove-Item $failureFile, $failureLog -Force -ErrorAction SilentlyContinue
    Set-Status -FivePct 91 -SevenPct 40

    Invoke-Hook "turn-failure-checkpoint.ps1" @{
        session_id    = "dead-session"
        error_type    = "rate_limit_error"
        error_message = "5-hour limit reached, resets at 19:00"
    } | Out-Null

    Assert-That "a killed turn leaves a LAST_FAILURE.json" (Test-Path $failureFile)
    if (Test-Path $failureFile) {
        $rec = Get-Content $failureFile -Raw | ConvertFrom-Json
        Assert-That "records the error type" ($rec.error_type -eq "rate_limit_error") "got $($rec.error_type)"
        Assert-That "recognises a usage limit" ($rec.looks_rate_limited -eq $true) "got $($rec.looks_rate_limited)"
        Assert-That "records the session id" ($rec.session_id -eq "dead-session") "got $($rec.session_id)"
        Assert-That "records the budget floor at death" ($rec.budget_at_failure -match "91") "got $($rec.budget_at_failure)"
    }
    Assert-That "appends to the failure log" ((Test-Path $failureLog) -and ((Get-Content $failureLog).Count -ge 1))

    # A non-rate-limit API error is still recorded, just not mislabelled.
    Invoke-Hook "turn-failure-checkpoint.ps1" @{
        session_id = "s"; error_type = "overloaded_error"; error_message = "server busy"
    } | Out-Null
    $rec = Get-Content $failureFile -Raw | ConvertFrom-Json
    Assert-That "a non-rate-limit error is not mislabelled" ($rec.looks_rate_limited -eq $false) "got $($rec.looks_rate_limited)"
    Assert-That "the log accumulates rather than overwrites" ((Get-Content $failureLog).Count -ge 2) "got $((Get-Content $failureLog).Count) lines"

    # The hook must not disturb the working tree it is trying to protect.
    Push-Location $repoRoot
    $before = @(& git status --porcelain 2>$null) -join "`n"
    Pop-Location
    Invoke-Hook "turn-failure-checkpoint.ps1" @{ session_id = "s"; error_type = "x"; error_message = "y" } | Out-Null
    Push-Location $repoRoot
    $after = @(& git status --porcelain 2>$null) -join "`n"
    Pop-Location
    Assert-That "capturing WIP does not modify the working tree" ($before -eq $after) "before=[$before] after=[$after]"

    # =========================================================================
    Write-Host "`nsession-start-autopilot (SessionStart)" -ForegroundColor Cyan
    # =========================================================================
    $marker = Join-Path $queueDir "AUTOPILOT"
    $offSwitch = Join-Path $queueDir "AUTOPILOT.OFF"

    Remove-Item $marker, $offSwitch, $failureFile -Force -ErrorAction SilentlyContinue
    Set-Status -FivePct 5 -SevenPct 5
    $out = Invoke-Hook "session-start-autopilot.ps1" @{ session_id = "n1"; source = "startup" }
    Assert-That "arms on a healthy budget" (Test-Path $marker)
    if (Test-Path $marker) {
        $cfg = Get-Content $marker -Raw | ConvertFrom-Json
        Assert-That "arms the full stretch when budget is fine" ($cfg.max_iterations -eq 20) "got $($cfg.max_iterations)"
    }

    # The bug observed on 2026-07-23: a 4h/20 stretch was armed at 7d=85%,
    # which the Stop gate vetoes at its very first check.
    Remove-Item $marker -Force -ErrorAction SilentlyContinue
    Set-Status -FivePct 5 -SevenPct 88
    $out = Invoke-Hook "session-start-autopilot.ps1" @{ session_id = "n2"; source = "startup" }
    Assert-That "does not arm on a spent budget" (-not (Test-Path $marker)) "marker was created anyway"
    Assert-That "says why it did not arm" ($out -match "NOT armed") "got: $out"

    Remove-Item $marker -Force -ErrorAction SilentlyContinue
    Set-Status -FivePct 72 -SevenPct 5
    $out = Invoke-Hook "session-start-autopilot.ps1" @{ session_id = "n3"; source = "startup" }
    Assert-That "arms a short stretch at rung 2" (Test-Path $marker)
    if (Test-Path $marker) {
        $cfg = Get-Content $marker -Raw | ConvertFrom-Json
        Assert-That "short stretch is 6 iterations" ($cfg.max_iterations -eq 6) "got $($cfg.max_iterations)"
    }

    # Recovery: the new session must be told the last one was killed.
    Set-Status -FivePct 5 -SevenPct 5
    @{
        recorded_at_iso = "2026-07-23T12:00:00Z"; looks_rate_limited = $true
        error_type = "rate_limit_error"; dirty_file_count = 3
        wip_commit = "deadbeef"; wip_ref = "refs/wip-checkpoints/123"
        budget_at_failure = "5h >=99%"
    } | ConvertTo-Json | Set-Content $failureFile -Encoding utf8
    $out = Invoke-Hook "session-start-autopilot.ps1" @{ session_id = "n4"; source = "startup" }
    Assert-That "reports the previous killed turn" ($out -match "PREVIOUS TURN DID NOT FINISH") "got: $out"
    Assert-That "points at the recoverable WIP commit" ($out -match "deadbeef") "got: $out"
    Assert-That "consumes the failure record" (-not (Test-Path $failureFile)) "LAST_FAILURE.json survived"

    # The maintainer's off switch outranks everything.
    Remove-Item $marker -Force -ErrorAction SilentlyContinue
    New-Item $offSwitch -ItemType File -Force | Out-Null
    $out = Invoke-Hook "session-start-autopilot.ps1" @{ session_id = "n5"; source = "startup" }
    Assert-That "AUTOPILOT.OFF prevents arming" (-not (Test-Path $marker)) "armed over the off switch"
    Remove-Item $offSwitch -Force -ErrorAction SilentlyContinue

    # =========================================================================
    Write-Host "`nresume-runner (scheduled self-restart)" -ForegroundColor Cyan
    # =========================================================================
    # Every assertion here uses -DryRun. These gates decide whether to spend
    # real money unattended; a test that actually launched `claude` would be
    # charging the maintainer to run the test suite.
    $fakeRepo = Join-Path $sandbox "fakerepo\docs"
    New-Item -ItemType Directory -Path $fakeRepo -Force | Out-Null
    $env:OPAL_RESUME_REPO_ROOT = (Split-Path $fakeRepo -Parent)
    $runner = Join-Path $hooksDir "resume-runner.ps1"
    $runnerState = Join-Path $queueDir ".resume-runner-state.json"

    function Set-FakeWork {
        param([switch]$None)
        if ($None) {
            "# Resume note`n`n_Nothing in flight._" | Set-Content (Join-Path $fakeRepo "RESUME.md") -Encoding utf8
            "# Backlog`n`n## Now`n`n## Done recently`n`n### an old thing" | Set-Content (Join-Path $fakeRepo "BACKLOG.md") -Encoding utf8
        } else {
            "# Resume note`n`n_Nothing in flight._" | Set-Content (Join-Path $fakeRepo "RESUME.md") -Encoding utf8
            "# Backlog`n`n## Now`n`n### a real pending item`n`n## Done recently" | Set-Content (Join-Path $fakeRepo "BACKLOG.md") -Encoding utf8
        }
    }

    function Invoke-Runner {
        $out = & powershell.exe -NoProfile -ExecutionPolicy Bypass -File $runner -DryRun 2>$null
        return (($out -join "`n"))
    }

    Remove-Item $runnerState -Force -ErrorAction SilentlyContinue
    Set-FakeWork
    Set-Status -FivePct 10 -SevenPct 10
    $out = Invoke-Runner
    Assert-That "launches when budget is healthy and work exists" ($out -match "would-launch") "got: $out"

    # The situation this was built in: 5h reset to near zero while 7d sat at
    # 86%. A healthy 5h window must not mask an exhausted 7d one.
    Remove-Item $runnerState -Force -ErrorAction SilentlyContinue
    Set-Status -FivePct 2 -SevenPct 86
    $out = Invoke-Runner
    Assert-That "a fresh 5h window does not mask an exhausted 7d one" ($out -match "budget not recovered") "got: $out"

    Remove-Item $runnerState -Force -ErrorAction SilentlyContinue
    Set-Status -FivePct 75 -SevenPct 10
    $out = Invoke-Runner
    Assert-That "does not resume at rung 2" ($out -match "budget not recovered") "got: $out"

    # THE DEADLOCK REGRESSION TEST.
    #
    # Both windows expired means "no usable reading", and that is exactly the
    # moment the quota came back. The first version of this runner treated it
    # as a reason to give up - which would have deadlocked the whole feature:
    # nothing refreshes rate-limit-status.json while no session is running, so
    # it needed fresh numbers to decide it may start a session, and only a
    # session produces fresh numbers. It would have logged "refusing to guess"
    # hourly, forever, in silence.
    #
    # A stub keep-warm stands in for the real one so this costs nothing: the
    # real one launches `claude`, and a test suite must not spend money.
    $stubKeepwarm = Join-Path $sandbox "stub-keepwarm.ps1"
    $env:OPAL_KEEPWARM_CMD = $stubKeepwarm
    $healthyStatus = @{
        five_hour  = @{ used_percentage = 3; resets_at = ([DateTimeOffset]::UtcNow.ToUnixTimeSeconds() + 18000) }
        seven_day  = @{ used_percentage = 9; resets_at = ([DateTimeOffset]::UtcNow.ToUnixTimeSeconds() + 500000) }
        updated_at = [DateTimeOffset]::UtcNow.ToUnixTimeSeconds()
    } | ConvertTo-Json -Depth 5 -Compress
    # The stub writes what a real refresh would have produced after a rollover.
    "param([switch]`$Force,[switch]`$NoWait)`n'$healthyStatus' | Set-Content '$statusFile' -Encoding utf8" |
        Set-Content $stubKeepwarm -Encoding utf8

    Remove-Item $runnerState -Force -ErrorAction SilentlyContinue
    Set-Status -FivePct 99 -FiveResetsIn -60 -SevenPct 99 -SevenResetsIn -60
    $out = Invoke-Runner
    Assert-That "an expired window triggers a refresh instead of giving up" ($out -match "refreshing") "got: $out"
    Assert-That "and resumes once the refresh shows a recovered budget" ($out -match "would-launch") "got: $out"

    # A refresh that produces nothing usable is the one case where giving up is
    # right - guessing wrong here spends money with nobody watching.
    "param([switch]`$Force,[switch]`$NoWait)" | Set-Content $stubKeepwarm -Encoding utf8
    Remove-Item $runnerState -Force -ErrorAction SilentlyContinue
    Set-Status -FivePct 99 -FiveResetsIn -60 -SevenPct 99 -SevenResetsIn -60
    $out = Invoke-Runner
    Assert-That "still refuses to guess when a refresh yields nothing" ($out -match "even after a refresh") "got: $out"

    # A usable reading must NOT trigger a refresh - a live window's old figure
    # is a valid floor, so spending a launch to confirm it would be waste.
    "param([switch]`$Force,[switch]`$NoWait)`n'BROKEN' | Set-Content '$statusFile' -Encoding utf8" |
        Set-Content $stubKeepwarm -Encoding utf8
    Remove-Item $runnerState -Force -ErrorAction SilentlyContinue
    Set-Status -FivePct 5 -SevenPct 88 -AgeSeconds 20000
    $out = Invoke-Runner
    Assert-That "a stale-but-live reading is used as-is, no refresh" (($out -notmatch "refreshing") -and ($out -match "budget not recovered")) "got: $out"
    Remove-Item Env:\OPAL_KEEPWARM_CMD -ErrorAction SilentlyContinue

    Remove-Item $runnerState -Force -ErrorAction SilentlyContinue
    Set-Status -FivePct 10 -SevenPct 10
    Set-FakeWork -None
    $out = Invoke-Runner
    Assert-That "does not launch when there is no work" ($out -match "nothing in RESUME.md or BACKLOG") "got: $out"

    # RESUME.md alone is enough, even with an empty backlog - that is the
    # killed-mid-task case.
    "# Resume note`n`n## In flight: something half-done" | Set-Content (Join-Path $fakeRepo "RESUME.md") -Encoding utf8
    $out = Invoke-Runner
    Assert-That "in-flight RESUME.md alone justifies a resume" ($out -match "would-launch") "got: $out"

    # Cooldown: a run that dies immediately must not become a relaunch loop.
    Set-FakeWork
    @{ last_launch = ([DateTimeOffset]::UtcNow.ToUnixTimeSeconds() - 600); last_pid = 0 } |
        ConvertTo-Json | Set-Content $runnerState -Encoding utf8
    $out = Invoke-Runner
    Assert-That "respects the cooldown after a recent launch" ($out -match "cooldown") "got: $out"

    @{ last_launch = ([DateTimeOffset]::UtcNow.ToUnixTimeSeconds() - 10800); last_pid = 0 } |
        ConvertTo-Json | Set-Content $runnerState -Encoding utf8
    $out = Invoke-Runner
    Assert-That "resumes again once the cooldown expires" ($out -match "would-launch") "got: $out"

    # Two unattended agents in one worktree would be a mess.
    @{ last_launch = 0; last_pid = $PID } | ConvertTo-Json | Set-Content $runnerState -Encoding utf8
    $out = Invoke-Runner
    Assert-That "will not start a second run while one is active" ($out -match "still active") "got: $out"

    # One off switch for the whole family, not two.
    Remove-Item $runnerState -Force -ErrorAction SilentlyContinue
    New-Item $offSwitch -ItemType File -Force | Out-Null
    $out = Invoke-Runner
    Assert-That "AUTOPILOT.OFF stops the resume runner too" ($out -match "AUTOPILOT.OFF") "got: $out"
    Remove-Item $offSwitch -Force -ErrorAction SilentlyContinue

    Remove-Item Env:\OPAL_RESUME_REPO_ROOT -ErrorAction SilentlyContinue

    # An unattended run must be bounded, since nobody is watching it.
    Set-Status -FivePct 5 -SevenPct 5
    Remove-Item $marker -Force -ErrorAction SilentlyContinue
    $env:OPAL_UNATTENDED_RESUME = "1"
    Invoke-Hook "session-start-autopilot.ps1" @{ session_id = "u1"; source = "startup" } | Out-Null
    Remove-Item Env:\OPAL_UNATTENDED_RESUME -ErrorAction SilentlyContinue
    if (Test-Path $marker) {
        $cfg = Get-Content $marker -Raw | ConvertFrom-Json
        Assert-That "unattended runs get a tight iteration cap" ($cfg.max_iterations -le 5) "got $($cfg.max_iterations)"
    } else {
        Assert-That "unattended runs still arm autopilot" $false "no marker written"
    }

    # =========================================================================
    Write-Host "`nautopilot-gate (Stop)" -ForegroundColor Cyan
    # =========================================================================
    $backlog = Join-Path $repoRoot "docs\BACKLOG.md"
    Assert-That "docs/BACKLOG.md exists (the gate reads it for remaining work)" (Test-Path $backlog)

    # Guard against the keep-warm regression: a wait longer than the hook's own
    # timeout silently ends autopilot, so hook callers must pass -NoWait.
    $gateSrc = Get-Content (Join-Path $hooksDir "autopilot-gate.ps1") -Raw
    Assert-That "the Stop gate calls keep-warm with -NoWait" ($gateSrc -match 'keepwarm\s+-NoWait') "the 42s wait exceeds the hook timeout"

    $settings = Get-Content (Join-Path $repoRoot ".claude\settings.json") -Raw | ConvertFrom-Json
    $stopTimeout = $settings.hooks.Stop[0].hooks[0].timeout
    Assert-That "the Stop hook timeout leaves room for keep-warm" ($stopTimeout -ge 60) "got $stopTimeout"

    $preToolNames = @($settings.hooks.PreToolUse | ForEach-Object { $_.hooks[0].command })
    Assert-That "budget-guard is registered on PreToolUse" (($preToolNames -join " ") -match "budget-guard") "got: $($preToolNames -join ' ; ')"

    $guardEntry = $settings.hooks.PreToolUse | Where-Object { $_.hooks[0].command -match "budget-guard" }
    Assert-That "budget-guard has no matcher, so it sees every tool call" ($null -eq $guardEntry.matcher) "matcher=$($guardEntry.matcher)"
    Assert-That "StopFailure is registered" ($null -ne $settings.hooks.StopFailure) "no StopFailure hook"

    # The retired hook must be gone, not merely unreferenced.
    Assert-That "the superseded rate-limit-gate.ps1 is deleted" (-not (Test-Path (Join-Path $hooksDir "rate-limit-gate.ps1"))) "still present"
    Assert-That "nothing still references rate-limit-gate.ps1" ((Get-Content (Join-Path $repoRoot ".claude\settings.json") -Raw) -notmatch "rate-limit-gate")
}
finally {
    Remove-Item $sandbox -Recurse -Force -ErrorAction SilentlyContinue
    Remove-Item Env:\OPAL_AUTOPILOT_QUEUE_DIR -ErrorAction SilentlyContinue
    Remove-Item Env:\OPAL_RATE_LIMIT_STATUS -ErrorAction SilentlyContinue
}

Write-Host ""
if ($script:failed -eq 0) {
    Write-Host "hooks: $($script:passed) passed" -ForegroundColor Green
    exit 0
} else {
    Write-Host "hooks: $($script:passed) passed, $($script:failed) FAILED" -ForegroundColor Red
    $script:failures | ForEach-Object { Write-Host "  - $_" -ForegroundColor Red }
    exit 1
}
