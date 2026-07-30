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
# Isolate from the real launching process: when this suite runs inside an
# actual unattended resume session, OPAL_UNATTENDED_RESUME=1 is already set
# in the environment and would leak into the attended-path assertions below.
Remove-Item Env:\OPAL_UNATTENDED_RESUME -ErrorAction SilentlyContinue

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

function Invoke-HookRaw {
    <#  Same, for the tests that need to hand a hook a literal JSON string
        (including deliberately malformed ones) rather than a hashtable.

        THE `-join` IS THE POINT, not tidiness. A bare `$json | powershell.exe`
        hands back an ARRAY when the hook prints more than one line, and
        `$array -match 'x'` returns the matching ELEMENTS, not a boolean - so
        `Assert-That` threw "cannot convert System.Object[] to Boolean" and
        aborted the whole suite mid-run on 2026-07-28. Every assertion after
        that point silently stopped being checked. Single-line output happened
        to convert cleanly, which is why 13 call sites did this for weeks and
        only the one that grew a second output line ever complained. #>
    param([string]$Hook, [string]$Json)
    $out = $Json | & powershell.exe -NoProfile -ExecutionPolicy Bypass -File $Hook
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
    Write-Host "`nbudget-lib: Get-BacklogItems" -ForegroundColor Cyan
    # =========================================================================
    # Decides whether autopilot keeps going and whether an unattended session
    # gets launched at all. If it silently returns nothing, autopilot dies quiet
    # - the exact failure the backlog migration already caused once.
    $bl = Join-Path $sandbox "backlog-fixture.md"
    @(
        "# Backlog", "", "## Now", "",
        "### First item", "Some prose about it.", "",
        "### Second item", "", "**Blocked:** waiting on a maintainer decision.", "",
        "### Third item", "Prose first.", "", "**Blocked:** this one is too late to count.", "",
        "## Done recently", "", "### An old finished thing", ""
    ) -join "`n" | Set-Content $bl -Encoding utf8

    $items = @(Get-BacklogItems -BacklogPath $bl)
    Assert-That "counts only headings above 'Done recently'" ($items.Count -eq 3) "got $($items.Count): $($items.Title -join '; ')"
    Assert-That "history is not work" ($items.Title -notcontains "An old finished thing") "got: $($items.Title -join '; ')"
    Assert-That "an unmarked item is actionable" (-not ($items | Where-Object { $_.Title -eq "First item" }).Blocked) "First item came back blocked"
    Assert-That "a **Blocked:** item is not" (($items | Where-Object { $_.Title -eq "Second item" }).Blocked) "Second item came back actionable"

    # The marker means "this item is blocked", not "the word appears somewhere
    # in it" - long entries routinely discuss blockers they have since cleared.
    Assert-That "**Blocked:** counts only as the item's opening line" `
        (-not ($items | Where-Object { $_.Title -eq "Third item" }).Blocked) "a mid-body mention blocked the item"

    # An all-blocked backlog SHOULD come back with nothing actionable - that is
    # the point - but it must be because every item was flagged, not because
    # the parser fell over.
    @("# Backlog", "", "## Now", "", "### Only item", "**Blocked:** nothing to do here.", "", "## Done recently") -join "`n" |
        Set-Content $bl -Encoding utf8
    $items = @(Get-BacklogItems -BacklogPath $bl)
    Assert-That "an all-blocked backlog still parses its items" ($items.Count -eq 1) "got $($items.Count)"
    Assert-That "...and reports none of them actionable" (@($items | Where-Object { -not $_.Blocked }).Count -eq 0) "something came back actionable"

    # BACKLOG.md has no BOM, and this file's Title values are embedded verbatim
    # into autopilot-gate's Stop-hook reason text - reading it as the system
    # ANSI codepage instead of UTF8 mangles any non-ASCII heading (an em dash,
    # here) before the model ever sees it. Byte-exact, not just "contains".
    @("# Backlog", "", "## Now", "", "### Title with an em dash `u{2014} here", "Some prose.", "", "## Done recently") -join "`n" |
        Set-Content $bl -Encoding utf8
    $items = @(Get-BacklogItems -BacklogPath $bl)
    Assert-That "a non-ASCII title round-trips byte-exact" `
        ($items.Count -eq 1 -and $items[0].Title -eq "Title with an em dash `u{2014} here") `
        "got: $($items.Title -join '; ')"

    $items = @(Get-BacklogItems -BacklogPath (Join-Path $sandbox "no-such-backlog.md"))
    Assert-That "a missing backlog returns empty, no throw" ($items.Count -eq 0) "got $($items.Count)"

    # The real file is what ships. If a formatting change ever made it parse as
    # zero items, autopilot would stop dead and nothing would say why.
    $realItems = @(Get-BacklogItems -BacklogPath (Join-Path $repoRoot "docs\BACKLOG.md"))
    Assert-That "the repo's own BACKLOG.md parses into items" ($realItems.Count -gt 0) "parsed zero items from the real backlog"

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
    Write-Host "`nhookbeat: Write-HookBeat / Get-HookBeats / Test-HookLiveness" -ForegroundColor Cyan
    # =========================================================================
    . (Join-Path $hooksDir "hookbeat.ps1")
    $beatsDir = Join-Path $queueDir ".hookbeats"
    Remove-Item $beatsDir -Recurse -Force -ErrorAction SilentlyContinue

    # The isolation bug found while building this: without honouring
    # OPAL_AUTOPILOT_QUEUE_DIR, every beat write landed in the REAL repo's
    # .claude/queue/.hookbeats regardless of the sandbox, so running this very
    # test suite kept "healing" the real liveness data with test-invocation
    # timestamps - which would make Test-HookLiveness unable to ever see a
    # truly dead hook, since the suite's own runs kept refreshing it.
    Write-HookBeat -Name "probe-hook"
    Assert-That "a beat is written under the sandboxed queue dir, not the real repo" `
        (Test-Path (Join-Path $beatsDir "probe-hook.json")) `
        "expected $beatsDir/probe-hook.json"
    $beats = Get-HookBeats
    Assert-That "the beat reads back with a parseable timestamp" `
        ($beats.ContainsKey("probe-hook") -and $beats["probe-hook"] -is [datetime]) `
        "got: $($beats["probe-hook"])"

    "not json" | Set-Content (Join-Path $beatsDir "corrupt.json") -Encoding utf8
    $beats2 = Get-HookBeats
    Assert-That "a corrupt beat file is skipped, not thrown on" ($beats2.ContainsKey("probe-hook")) "Get-HookBeats threw or dropped the good beat too"
    Remove-Item (Join-Path $beatsDir "corrupt.json") -Force -ErrorAction SilentlyContinue

    Remove-Item $beatsDir -Recurse -Force -ErrorAction SilentlyContinue
    $now = Get-Date
    $old = $now.AddDays(-1)
    foreach ($name in @('autopilot-gate', 'noticed-gate', 'budget-guard')) {
        Write-HookBeat -Name $name
    }
    $dead = @(Test-HookLiveness -LatestCommitAt $old)
    Assert-That "fresh beats after the newest commit are not flagged dead" ($dead.Count -eq 0) "got: $($dead -join '; ')"

    $dead2 = @(Test-HookLiveness -LatestCommitAt $now.AddDays(1))
    Assert-That "a beat older than the newest commit IS flagged dead" ($dead2.Count -eq 3) "got: $($dead2 -join '; ')"
    Assert-That "the dead-hook message names the hook" ($dead2 -join ';' -match 'autopilot-gate') "got: $($dead2 -join '; ')"

    Remove-Item $beatsDir -Recurse -Force -ErrorAction SilentlyContinue
    $dead3 = @(Test-HookLiveness -LatestCommitAt $now)
    Assert-That "no beats at all, but commits exist, is also flagged dead" ($dead3.Count -eq 3) "got: $($dead3 -join '; ')"

    Assert-That "a hook outside the high-frequency set is never flagged" `
        (($dead3 -join ';') -notmatch 'session-start-autopilot|turn-failure-checkpoint|pre-push-gate') `
        "got: $($dead3 -join '; ')"

    Remove-Item $beatsDir -Recurse -Force -ErrorAction SilentlyContinue

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

    # --- ref pruning --------------------------------------------------------
    # 322 checkpoint refs over seven days by 2026-07-30, each pinning a tree
    # git can then never collect, none ever read after the day it was written.
    # Against a SANDBOX repo, never the real one: this suite runs on every push
    # and must not be able to delete the maintainer's own recovery points.
    $ckRepo = Join-Path $sandbox "checkpoint-repo"
    New-Item -ItemType Directory -Path $ckRepo -Force | Out-Null
    & git -C $ckRepo init --quiet 2>$null | Out-Null
    & git -C $ckRepo config core.autocrlf false 2>$null | Out-Null
    & git -C $ckRepo config user.email "test@example.invalid" 2>$null | Out-Null
    & git -C $ckRepo config user.name "hook tests" 2>$null | Out-Null
    Set-Content (Join-Path $ckRepo "f.txt") "base" -Encoding utf8
    & git -C $ckRepo add -A 2>$null | Out-Null
    & git -C $ckRepo commit -m "base" --quiet 2>$null | Out-Null
    $ckHead = (& git -C $ckRepo rev-parse HEAD 2>$null)

    $nowStamp = [DateTimeOffset]::UtcNow.ToUnixTimeSeconds()
    $ancient = @()
    foreach ($age in 60, 50, 40, 30, 25) {
        $ref = "refs/wip-checkpoints/$($nowStamp - ($age * 86400))"
        & git -C $ckRepo update-ref $ref $ckHead 2>$null | Out-Null
        $ancient += $ref
    }
    $fresh = @()
    foreach ($age in 0, 1, 2) {
        $ref = "refs/wip-checkpoints/$($nowStamp - ($age * 3600))"
        & git -C $ckRepo update-ref $ref $ckHead 2>$null | Out-Null
        $fresh += $ref
    }
    # A ref whose name is not a timestamp: unknown provenance must be left alone
    # rather than have an age guessed for it.
    & git -C $ckRepo update-ref "refs/wip-checkpoints/manual-keepme" $ckHead 2>$null | Out-Null

    $env:OPAL_CHECKPOINT_REPO_ROOT = $ckRepo
    $env:OPAL_CHECKPOINT_KEEP_DAYS = "14"
    $env:OPAL_CHECKPOINT_KEEP_AT_LEAST = "3"
    Invoke-Hook "turn-failure-checkpoint.ps1" @{ session_id = "prune"; error_type = "x"; error_message = "y" } | Out-Null
    $remaining = @(& git -C $ckRepo for-each-ref --format='%(refname)' refs/wip-checkpoints/ 2>$null | Where-Object { $_ })
    Remove-Item Env:\OPAL_CHECKPOINT_REPO_ROOT, Env:\OPAL_CHECKPOINT_KEEP_DAYS, Env:\OPAL_CHECKPOINT_KEEP_AT_LEAST -ErrorAction SilentlyContinue

    Assert-That "checkpoints older than the cutoff are pruned" `
        (($ancient | Where-Object { $remaining -contains $_ }).Count -eq 0) `
        "still present: $(($ancient | Where-Object { $remaining -contains $_ }) -join ', ')"
    Assert-That "recent checkpoints survive" `
        (($fresh | Where-Object { $remaining -notcontains $_ }).Count -eq 0) `
        "missing: $(($fresh | Where-Object { $remaining -notcontains $_ }) -join ', ')"
    Assert-That "a ref that is not a timestamp is never guessed at" `
        ($remaining -contains "refs/wip-checkpoints/manual-keepme") "remaining: $($remaining -join ', ')"
    $prunedRec = Get-Content $failureFile -Raw | ConvertFrom-Json
    Assert-That "the prune is recorded rather than done silently" `
        ($prunedRec.checkpoints_pruned -eq 5) "got $($prunedRec.checkpoints_pruned)"

    # The floor, which is the half that keeps this safe: with everything old and
    # nothing recent, the newest few must still survive rather than the repo
    # being left with no recovery point at all.
    $ckRepo2 = Join-Path $sandbox "checkpoint-repo-2"
    New-Item -ItemType Directory -Path $ckRepo2 -Force | Out-Null
    & git -C $ckRepo2 init --quiet 2>$null | Out-Null
    # Not optional - see the note above the first sandbox repo in this file. Its
    # absence here aborted the whole suite on the first run of these assertions.
    & git -C $ckRepo2 config core.autocrlf false 2>$null | Out-Null
    & git -C $ckRepo2 config user.email "test@example.invalid" 2>$null | Out-Null
    & git -C $ckRepo2 config user.name "hook tests" 2>$null | Out-Null
    Set-Content (Join-Path $ckRepo2 "f.txt") "base" -Encoding utf8
    & git -C $ckRepo2 add -A 2>$null | Out-Null
    & git -C $ckRepo2 commit -m "base" --quiet 2>$null | Out-Null
    $ckHead2 = (& git -C $ckRepo2 rev-parse HEAD 2>$null)
    $oldest = @()
    foreach ($age in 90, 80, 70, 60, 50) {
        $ref = "refs/wip-checkpoints/$($nowStamp - ($age * 86400))"
        & git -C $ckRepo2 update-ref $ref $ckHead2 2>$null | Out-Null
        $oldest += $ref
    }
    $env:OPAL_CHECKPOINT_REPO_ROOT = $ckRepo2
    $env:OPAL_CHECKPOINT_KEEP_DAYS = "14"
    $env:OPAL_CHECKPOINT_KEEP_AT_LEAST = "2"
    Invoke-Hook "turn-failure-checkpoint.ps1" @{ session_id = "floor"; error_type = "x"; error_message = "y" } | Out-Null
    $left = @(& git -C $ckRepo2 for-each-ref --format='%(refname)' refs/wip-checkpoints/ 2>$null | Where-Object { $_ })
    Remove-Item Env:\OPAL_CHECKPOINT_REPO_ROOT, Env:\OPAL_CHECKPOINT_KEEP_DAYS, Env:\OPAL_CHECKPOINT_KEEP_AT_LEAST -ErrorAction SilentlyContinue
    Assert-That "a quiet fortnight does not wipe every recovery point" ($left.Count -eq 2) "got $($left.Count): $($left -join ', ')"
    Assert-That "and the ones kept are the newest, not an arbitrary two" `
        (($left -contains "refs/wip-checkpoints/$($nowStamp - (50 * 86400))") -and ($left -contains "refs/wip-checkpoints/$($nowStamp - (60 * 86400))")) `
        "kept: $($left -join ', ')"

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

    # SELF-AUDIT: the hook-liveness check added alongside hookbeat.ps1. This
    # runs against the REAL repo's git log (session-start-autopilot.ps1
    # resolves its own repo root, not the sandbox), so the newest commit is
    # always "recent" here - the only thing under test is whether the beats
    # directory (which IS sandboxed, via OPAL_AUTOPILOT_QUEUE_DIR) is missing
    # or stale relative to that.
    # Matching a phrase this hook emits is not enough on its own: this very
    # file (docs/RESUME.md, surfaced verbatim by this hook whenever it is
    # non-placeholder) is allowed to quote or describe that phrase in prose -
    # it did exactly that while this feature was being written, and broke
    # this assertion via a false pass/fail neither one caused by the code
    # under test. "has never recorded a beat, but commits already exist" is
    # Test-HookLiveness's per-hook detail sentence (the "no beat found at
    # all" branch), not the outer wrapper - much less likely to be echoed in
    # prose describing the feature, but still not immune, which is exactly
    # why the case below also asserts the negative with the docs' true
    # content restored, not with it emptied out.
    $beatsDirT = Join-Path $queueDir ".hookbeats"
    Remove-Item $beatsDirT -Recurse -Force -ErrorAction SilentlyContinue
    $out = Invoke-Hook "session-start-autopilot.ps1" @{ session_id = "hb1"; source = "startup" }
    Assert-That "no beats at all is reported as a self-audit finding" ($out -match "has never recorded a beat, but commits already exist") "got: $out"
    Assert-That "names at least one of the high-frequency hooks" ($out -match "autopilot-gate|noticed-gate|budget-guard") "got: $out"

    New-Item -ItemType Directory -Path $beatsDirT -Force | Out-Null
    foreach ($name in @('autopilot-gate', 'noticed-gate', 'budget-guard')) {
        [pscustomobject]@{ hook = $name; at = (Get-Date).ToString('o'); pid = $PID } |
            ConvertTo-Json -Compress | Set-Content (Join-Path $beatsDirT "$name.json") -Encoding utf8
    }
    $out = Invoke-Hook "session-start-autopilot.ps1" @{ session_id = "hb2"; source = "startup" }
    Assert-That "fresh beats for all three produce no self-audit finding" ($out -notmatch "has never recorded a beat" -and $out -notmatch "older than the newest commit") "got: $out"
    Remove-Item $beatsDirT -Recurse -Force -ErrorAction SilentlyContinue

    # A failing scheduled resume runner has no human to tell. Its launch path was
    # broken for two days and six attempts, and the only trace was a log line
    # nobody reads - so the next interactive session has to be told, once.
    $resumeLogT = Join-Path $queueDir "resume-runner.log"
    $reportStateT = Join-Path $queueDir ".resume-report-state.json"
    Remove-Item $reportStateT -Force -ErrorAction SilentlyContinue
    @(
        "2026-07-24T06:26:59Z`tskip`tbudget not recovered (7d >=86%)"
        "2026-07-24T06:52:51Z`tlaunch-failed`t%1 is not a valid Win32 application."
    ) -join "`n" | Set-Content $resumeLogT -Encoding utf8
    $out = Invoke-Hook "session-start-autopilot.ps1" @{ session_id = "r1"; source = "startup" }
    Assert-That "a failed unattended launch is reported to the next session" `
        ($out -match "COULD NOT START A SESSION") "got: $out"
    Assert-That "and it quotes the actual failure, not just a count" `
        ($out -match "not a valid Win32 application") "got: $out"

    # Reported once. Re-announcing the same dead attempt every startup would
    # train the reader to ignore it, which is the failure mode being fixed.
    $out = Invoke-Hook "session-start-autopilot.ps1" @{ session_id = "r2"; source = "startup" }
    Assert-That "the same failure is not re-announced next time" `
        ($out -notmatch "COULD NOT START A SESSION") "got: $out"

    Add-Content $resumeLogT -Value "2026-07-25T12:52:50Z`tlaunch-failed`tsomething new broke" -Encoding utf8
    $out = Invoke-Hook "session-start-autopilot.ps1" @{ session_id = "r3"; source = "startup" }
    Assert-That "but a NEW failure is" ($out -match "something new broke") "got: $out"

    # Skips are the normal quiet hour - reporting them would be noise.
    Add-Content $resumeLogT -Value "2026-07-25T13:52:50Z`tskip`tcooldown, 42m remaining" -Encoding utf8
    $out = Invoke-Hook "session-start-autopilot.ps1" @{ session_id = "r4"; source = "startup" }
    Assert-That "a routine skip is not reported as a failure" `
        ($out -notmatch "COULD NOT START A SESSION") "got: $out"
    Remove-Item $resumeLogT, $reportStateT -Force -ErrorAction SilentlyContinue

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
    # The budget-guard tests above stamped a session heartbeat into this same
    # sandbox queue, and gate 2b honours it. Clear it so these assertions test
    # what they say they test; the heartbeat gate has its own tests below.
    Remove-Item (Join-Path $queueDir ".session-heartbeat.json") -Force -ErrorAction SilentlyContinue

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
    # --- gate 2c: a worktree someone is actively editing ---------------------
    # The 2026-07-29 collision: a session opened outside this repo has no
    # heartbeat hook, so gate 2b saw nothing and an unattended agent was
    # launched into a tree a human was editing. It committed twice on top.
    #
    # A real git repo, because the gate asks git. The fake repo used elsewhere
    # in this section is not one - which is itself worth pinning: on a
    # non-repo, this gate must stay out of the way rather than block forever.
    $dirtyRepo = Join-Path $sandbox "dirty-repo"
    New-Item -ItemType Directory -Path $dirtyRepo -Force | Out-Null
    New-Item -ItemType Directory -Path (Join-Path $dirtyRepo "docs") -Force | Out-Null
    # 2>$null, never 2>&1: redirecting a native exe's stderr into the success
    # stream wraps each line in an ErrorRecord (NativeCommandError) and trips
    # $ErrorActionPreference, so git's harmless "CRLF will be replaced by LF"
    # warning aborted the whole suite on the first run of these tests.
    & git -C $dirtyRepo init --quiet 2>$null | Out-Null
    & git -C $dirtyRepo config core.autocrlf false 2>$null | Out-Null
    & git -C $dirtyRepo config user.email "test@example.invalid" 2>$null | Out-Null
    & git -C $dirtyRepo config user.name "hook tests" 2>$null | Out-Null
    "# Resume note`n`n_Nothing in flight._" | Set-Content (Join-Path $dirtyRepo "docs\RESUME.md") -Encoding utf8
    "# Backlog`n`n## Now`n`n### a real pending item`n`n## Done recently" | Set-Content (Join-Path $dirtyRepo "docs\BACKLOG.md") -Encoding utf8
    & git -C $dirtyRepo add -A 2>$null | Out-Null
    & git -C $dirtyRepo commit -m "base" --quiet 2>$null | Out-Null

    $savedRepoRoot = $env:OPAL_RESUME_REPO_ROOT
    $env:OPAL_RESUME_REPO_ROOT = $dirtyRepo
    Remove-Item $runnerState -Force -ErrorAction SilentlyContinue
    Set-Status -FivePct 10 -SevenPct 10

    $out = Invoke-Runner
    Assert-That "a clean tree is not mistaken for someone working" ($out -match "would-launch") "got: $out"

    # Edited just now: exactly the state the collision happened in.
    "scratch" | Set-Content (Join-Path $dirtyRepo "work-in-progress.txt") -Encoding utf8
    Remove-Item $runnerState -Force -ErrorAction SilentlyContinue
    $out = Invoke-Runner
    Assert-That "a freshly edited worktree holds the launch back" ($out -match "worktree was edited") "got: $out"
    Assert-That "and holding back is not a launch" ($out -notmatch "would-launch") "got: $out"

    # THE ANTI-WEDGE PROPERTY, and the reason this is an age window rather than
    # a dirty check. An unattended run that died leaving half an edit behind
    # must not silence the runner permanently - that is the failure mode this
    # whole file was written against.
    $stale = (Get-Date).AddHours(-3)
    Set-ItemProperty -LiteralPath (Join-Path $dirtyRepo "work-in-progress.txt") -Name LastWriteTime -Value $stale
    Remove-Item $runnerState -Force -ErrorAction SilentlyContinue
    $out = Invoke-Runner
    Assert-That "stale leftovers age out instead of wedging the runner shut" ($out -match "would-launch") "got: $out"

    # -Force is the maintainer saying "I know, do it anyway".
    $recent = Join-Path $dirtyRepo "work-in-progress.txt"
    Set-ItemProperty -LiteralPath $recent -Name LastWriteTime -Value (Get-Date)
    Remove-Item $runnerState -Force -ErrorAction SilentlyContinue
    $out = (& powershell.exe -NoProfile -ExecutionPolicy Bypass -File $runner -DryRun -Force 2>$null) -join "`n"
    Assert-That "-Force overrides the worktree check like every other gate" ($out -match "would-launch") "got: $out"

    $env:OPAL_RESUME_REPO_ROOT = $savedRepoRoot
    Remove-Item $runnerState -Force -ErrorAction SilentlyContinue

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

    # A backlog of nothing but blocked items is not a reason to spend a session:
    # an unattended run cannot make a maintainer decision, so it would wake,
    # read, find nothing it is allowed to do, and cost money hourly.
    Remove-Item $runnerState -Force -ErrorAction SilentlyContinue
    "# Resume note`n`n_Nothing in flight._" | Set-Content (Join-Path $fakeRepo "RESUME.md") -Encoding utf8
    "# Backlog`n`n## Now`n`n### needs the maintainer`n**Blocked:** waiting on a decision.`n`n## Done recently" |
        Set-Content (Join-Path $fakeRepo "BACKLOG.md") -Encoding utf8
    $out = Invoke-Runner
    Assert-That "a fully blocked backlog is not work" ($out -match "nothing in RESUME.md or BACKLOG") "got: $out"

    # ...but one actionable item among blocked ones still is.
    "# Backlog`n`n## Now`n`n### needs the maintainer`n**Blocked:** waiting on a decision.`n`n### something doable`nprose`n`n## Done recently" |
        Set-Content (Join-Path $fakeRepo "BACKLOG.md") -Encoding utf8
    $out = Invoke-Runner
    Assert-That "one unblocked item among blocked ones is enough" ($out -match "would-launch") "got: $out"

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

    # ...and neither would one unattended agent joining the maintainer's own
    # session. Gate 2 above only knows about runs the runner itself started; on
    # 2026-07-26, the first hour the launch path worked, it launched into a tree
    # an interactive session was already editing.
    $heartbeat = Join-Path $queueDir ".session-heartbeat.json"
    Remove-Item $runnerState -Force -ErrorAction SilentlyContinue
    @{ at = ([DateTimeOffset]::UtcNow.ToUnixTimeSeconds() - 120); session_id = "live" } |
        ConvertTo-Json -Compress | Set-Content $heartbeat -Encoding utf8
    $out = Invoke-Runner
    Assert-That "will not launch into a tree a live session is working in" ($out -match "session is active") "got: $out"

    # THE DEADLOCK THIS MUST NOT BECOME. A heartbeat that never ages out - or a
    # check based on "is any claude process alive", which the permanently-idle
    # keep-warm process would satisfy forever - would wedge this shut in silence,
    # the same failure shape as gate 4's.
    @{ at = ([DateTimeOffset]::UtcNow.ToUnixTimeSeconds() - 3600); session_id = "dead" } |
        ConvertTo-Json -Compress | Set-Content $heartbeat -Encoding utf8
    $out = Invoke-Runner
    Assert-That "a stale heartbeat ages out instead of wedging it shut" ($out -match "would-launch") "got: $out"

    Remove-Item $heartbeat -Force -ErrorAction SilentlyContinue
    $out = Invoke-Runner
    Assert-That "no heartbeat at all is not treated as a session" ($out -match "would-launch") "got: $out"

    # The stamp has to come from the hook that fires on every tool call, or the
    # gate above is asserting against a file nothing writes.
    Remove-Item $heartbeat -Force -ErrorAction SilentlyContinue
    Set-Status -FivePct 5 -SevenPct 5
    Invoke-Hook "budget-guard.ps1" @{ session_id = "hb1"; tool_name = "Read" } | Out-Null
    Assert-That "budget-guard stamps the heartbeat even on a healthy budget" (Test-Path $heartbeat) "no heartbeat written"
    Remove-Item $heartbeat -Force -ErrorAction SilentlyContinue

    # --- per-launch file housekeeping ---------------------------------------
    # 42 resume-run-*.log / .err / resume-prompt-*.txt files had accumulated
    # with no expiry. Same two rails as the checkpoint-ref prune: age plus a
    # floor counted in launches rather than files.
    Remove-Item $heartbeat -Force -ErrorAction SilentlyContinue
    Remove-Item $runnerState -Force -ErrorAction SilentlyContinue
    Set-Status -FivePct 5 -SevenPct 5
    Set-FakeWork
    $nowSec = [DateTimeOffset]::UtcNow.ToUnixTimeSeconds()
    $oldStamps = @()
    foreach ($age in 60, 50, 40, 30) {
        $s = $nowSec - ($age * 86400)
        $oldStamps += $s
        Set-Content (Join-Path $queueDir "resume-run-$s.log") "old" -Encoding utf8
        Set-Content (Join-Path $queueDir "resume-run-$s.log.err") "" -Encoding utf8
        Set-Content (Join-Path $queueDir "resume-prompt-$s.txt") "old prompt" -Encoding utf8
    }
    $newStamps = @()
    foreach ($age in 0, 1) {
        $s = $nowSec - ($age * 3600)
        $newStamps += $s
        Set-Content (Join-Path $queueDir "resume-run-$s.log") "new" -Encoding utf8
        Set-Content (Join-Path $queueDir "resume-prompt-$s.txt") "new prompt" -Encoding utf8
    }
    # Must survive: the append-only decision log, and anything without a
    # timestamp to judge.
    Set-Content (Join-Path $queueDir "resume-run-keepme.log") "not a stamp" -Encoding utf8
    $decisionLogBefore = "sentinel line"
    Add-Content (Join-Path $queueDir "resume-runner.log") $decisionLogBefore -Encoding utf8

    $env:OPAL_RESUME_KEEP_DAYS = "14"
    $env:OPAL_RESUME_KEEP_AT_LEAST = "2"
    Invoke-Runner | Out-Null
    Remove-Item Env:\OPAL_RESUME_KEEP_DAYS, Env:\OPAL_RESUME_KEEP_AT_LEAST -ErrorAction SilentlyContinue

    $stillThere = @(Get-ChildItem $queueDir -File | Select-Object -ExpandProperty Name)
    Assert-That "per-launch files older than the cutoff are pruned" `
        (($oldStamps | Where-Object { $stillThere -contains "resume-run-$_.log" }).Count -eq 0) `
        "left: $(($oldStamps | Where-Object { $stillThere -contains "resume-run-$_.log" }) -join ', ')"
    Assert-That "the empty .err files go with their run, not separately" `
        (($oldStamps | Where-Object { $stillThere -contains "resume-run-$_.log.err" }).Count -eq 0) "err files left behind"
    Assert-That "and so do the prompt files" `
        (($oldStamps | Where-Object { $stillThere -contains "resume-prompt-$_.txt" }).Count -eq 0) "prompt files left behind"
    Assert-That "recent launches survive" `
        (($newStamps | Where-Object { $stillThere -notcontains "resume-run-$_.log" }).Count -eq 0) "recent files were deleted"
    Assert-That "a file with no timestamp is never a candidate" `
        ($stillThere -contains "resume-run-keepme.log") "keepme was deleted"
    Assert-That "the append-only decision log is never pruned" `
        ((Get-Content (Join-Path $queueDir "resume-runner.log") -Raw) -match "sentinel line") "decision log lost its history"

    # The floor needs its own case: with recent launches present it is
    # indistinguishable from the age cutoff, because the newest are inside the
    # cutoff anyway. Everything old and nothing recent is the only arrangement
    # that can tell them apart - and it is the arrangement that matters, since a
    # quiet fortnight must not leave zero diagnostics behind.
    Get-ChildItem $queueDir -File -Filter "resume-*" | Where-Object { $_.Name -match '^resume-(?:run|prompt)-\d+\.' } |
        Remove-Item -Force -ErrorAction SilentlyContinue
    $allOld = @()
    foreach ($age in 90, 80, 70, 60) {
        $s = $nowSec - ($age * 86400)
        $allOld += $s
        Set-Content (Join-Path $queueDir "resume-run-$s.log") "old" -Encoding utf8
    }
    Remove-Item $runnerState -Force -ErrorAction SilentlyContinue
    $env:OPAL_RESUME_KEEP_DAYS = "14"
    $env:OPAL_RESUME_KEEP_AT_LEAST = "2"
    Invoke-Runner | Out-Null
    Remove-Item Env:\OPAL_RESUME_KEEP_DAYS, Env:\OPAL_RESUME_KEEP_AT_LEAST -ErrorAction SilentlyContinue
    $survivors = @(Get-ChildItem $queueDir -File | Where-Object { $_.Name -match '^resume-run-\d+\.log$' } |
                   Select-Object -ExpandProperty Name)
    Assert-That "a quiet fortnight still leaves the floor of diagnostics" ($survivors.Count -eq 2) `
        "got $($survivors.Count): $($survivors -join ', ')"
    Assert-That "and the floor keeps the newest, not an arbitrary two" `
        (($survivors -contains "resume-run-$($allOld[3]).log") -and ($survivors -contains "resume-run-$($allOld[2]).log")) `
        "kept: $($survivors -join ', ')"

    # --- the prompt an unattended run is handed --------------------------------
    # Asserted against the runner's SOURCE, not a captured run: the prompt file is
    # written after -DryRun returns, and the prompt is a here-string with no
    # interpolation, so the prose is the whole risk. Nothing checked it before
    # 2026-07-30 - the same untested-prose gap that let the autopilot gate spend
    # eight days telling every turn to use a retired directory.
    $runnerSrc = Get-Content $runner -Raw
    Assert-That "the unattended prompt points at RESUME.md and BACKLOG.md, not the retired queue" `
        (($runnerSrc -match 'Read docs/RESUME\.md first, then docs/BACKLOG\.md') -and ($runnerSrc -notmatch 'queue/todo|queue/blocked')) `
        "the prompt no longer names both files, or has grown a retired-queue reference"
    Assert-That "it requires the local gate before pushing" ($runnerSrc -match 'dev\.ps1 all before pushing') "prompt no longer names the gate"
    # The two lessons from the 2026-07-30 run that produced nothing durable. Both
    # are one sentence in the prompt and both cost a day when absent.
    Assert-That "it says to commit BEFORE verifying" ($runnerSrc -match 'Commit BEFORE verifying') `
        "the prompt lost the commit-first instruction - a run that verifies first can end with the fix only in the working tree"
    Assert-That "it forbids work that outlives the turn" ($runnerSrc -match 'outlives this turn') `
        "the prompt lost the background-job warning - a run_in_background job dies with the run and reports nothing"
    Assert-That "it still reserves maintainer decisions" ($runnerSrc -match 'reserved for the maintainer') "prompt no longer defers maintainer decisions"

    # One off switch for the whole family, not two.
    Remove-Item $runnerState -Force -ErrorAction SilentlyContinue
    New-Item $offSwitch -ItemType File -Force | Out-Null
    $out = Invoke-Runner
    Assert-That "AUTOPILOT.OFF stops the resume runner too" ($out -match "AUTOPILOT.OFF") "got: $out"
    Remove-Item $offSwitch -Force -ErrorAction SilentlyContinue

    # =========================================================================
    Write-Host "`nresume-runner: the launch itself" -ForegroundColor Cyan
    # =========================================================================
    # EVERY assertion above this point uses -DryRun, which returns before
    # Start-Process is ever reached. That is precisely how the runner shipped
    # unable to launch anything at all: `Start-Process -FilePath "claude"` does
    # not walk PATHEXT the way the shell prompt does, so it picked npm's
    # extensionless POSIX shim and died with "%1 is not a valid Win32
    # application" on all six real attempts over two days. Fully green tests,
    # a feature that never once worked.
    #
    # A stub launcher (OPAL_RESUME_CLAUDE_CMD) makes the real launch path
    # testable for free - a stub .cmd costs nothing, where a real `claude` would
    # charge the maintainer to run the test suite.
    $stubOut = Join-Path $sandbox "stub-claude-out.txt"
    $stubCmd = Join-Path $sandbox "stub-claude.cmd"
    @(
        '@echo off'
        "echo ARGS: %*> ""$stubOut"""
        "powershell -NoProfile -Command ""[Console]::In.ReadToEnd()"" >> ""$stubOut"""
    ) -join "`r`n" | Set-Content $stubCmd -Encoding ascii

    $env:OPAL_RESUME_CLAUDE_CMD = $stubCmd
    # The suite must not pin the developer's laptop awake for the life of a
    # test run. The wake lock gets its own assertions further down.
    $env:OPAL_RESUME_NO_WAKELOCK = '1'
    Remove-Item $runnerState, $stubOut -Force -ErrorAction SilentlyContinue
    Set-FakeWork
    Set-Status -FivePct 10 -SevenPct 10

    # MAINS-ONLY, added 2026-07-29. Both branches, because a gate that is stuck
    # closed looks exactly like a quiet night. Overridden rather than read from
    # the real machine so this passes on a laptop running off its battery.
    $env:OPAL_RESUME_POWER_OVERRIDE = 'battery'
    $onBattery = (& powershell.exe -NoProfile -ExecutionPolicy Bypass -File $runner 2>$null) -join "`n"
    Assert-That "an unattended run is refused on battery" ($onBattery -match "on battery") "got: $onBattery"
    Assert-That "and refusing costs no launch" ($onBattery -notmatch "launched") "got: $onBattery"
    $env:OPAL_RESUME_POWER_OVERRIDE = 'ac'

    $launchOut = (& powershell.exe -NoProfile -ExecutionPolicy Bypass -File $runner 2>$null) -join "`n"
    Assert-That "the real launch path actually launches" ($launchOut -match "launched\s+pid \d+") "got: $launchOut"

    # A launch that "succeeded" without the child ever running would look
    # identical in the log, so wait for the stub's own evidence.
    $deadline = (Get-Date).AddSeconds(30)
    while (-not (Test-Path $stubOut) -and (Get-Date) -lt $deadline) { Start-Sleep -Milliseconds 200 }
    $stubText = ""
    if (Test-Path $stubOut) {
        # The stub appends stdin after its args; give it a moment to finish.
        $deadline = (Get-Date).AddSeconds(30)
        do {
            Start-Sleep -Milliseconds 200
            $stubText = (Get-Content $stubOut -Raw -ErrorAction SilentlyContinue)
        } while (($stubText -notmatch "unattended") -and (Get-Date) -lt $deadline)
    }
    Assert-That "the launched process really ran" ($stubText -match "ARGS:") "stub wrote: $stubText"
    Assert-That "it runs at the documented unattended default (sonnet)" ($stubText -match "--model sonnet") "stub wrote: $stubText"
    Assert-That "it skips the trust dialog no human is there to answer" ($stubText -match "--dangerously-skip-permissions") "stub wrote: $stubText"

    # THE MULTI-LINE PROMPT REGRESSION. A .cmd is run by cmd.exe, which ends its
    # command line at the first newline - passing this prompt as an argument
    # would deliver line one and try to EXECUTE the rest. It goes over stdin
    # instead, so assert a late line of it actually arrived.
    Assert-That "the whole prompt arrives, not just its first line" `
        (($stubText -match "resuming unattended") -and ($stubText -match "docs/agent-operating-model\.md")) "stub wrote: $stubText"
    Assert-That "the prompt is not passed as an argument" ($stubText -notmatch "ARGS:.*resuming unattended") "stub wrote: $stubText"

    $st = Get-Content $runnerState -Raw -ErrorAction SilentlyContinue | ConvertFrom-Json
    Assert-That "a real launch records its pid, so the next hour can see it" ([int]$st.last_pid -gt 0) "state: $st"
    Assert-That "and stamps the cooldown clock" ([int64]$st.last_launch -gt 0) "state: $st"
    Remove-Item Env:\OPAL_RESUME_CLAUDE_CMD -ErrorAction SilentlyContinue

    # STOPPING a run has to kill the agent, not just the wrapper. The recorded
    # pid is cmd.exe (claude.cmd); on 2026-07-26 killing that alone left the
    # claude.exe underneath orphaned and still editing the worktree for five
    # more minutes, and its changes landed in an unrelated commit. So the stub
    # here deliberately has a CHILD, and both must die.
    # The wrapper is started here rather than by the runner on purpose: a
    # wrapper launched from the runner's own short-lived powershell dies with
    # it, which would make this assert nothing. What is under test is -Stop's
    # behaviour given a recorded pid, so a stand-in with a real child is the
    # honest fixture.
    $sleeperCmd = Join-Path $sandbox "stub-sleeper.cmd"
    @('@echo off', 'powershell -NoProfile -Command "Start-Sleep -Seconds 120"') -join "`r`n" |
        Set-Content $sleeperCmd -Encoding ascii
    $sleeper = Start-Process -FilePath $sleeperCmd -WindowStyle Hidden -PassThru
    $wrapperPid = $sleeper.Id
    $kids = @()
    $deadline = (Get-Date).AddSeconds(20)
    while ($kids.Count -eq 0 -and (Get-Date) -lt $deadline) {
        Start-Sleep -Milliseconds 300
        $kids = @(Get-CimInstance Win32_Process -Filter "ParentProcessId=$wrapperPid" -ErrorAction SilentlyContinue |
                  Where-Object { $_.Name -eq 'powershell.exe' })
    }
    Assert-That "the fixture really has a child process to orphan" ($kids.Count -gt 0) "no powershell child of pid $wrapperPid - this test proves nothing without one"

    @{ last_launch = ([DateTimeOffset]::UtcNow.ToUnixTimeSeconds()); last_pid = $wrapperPid } |
        ConvertTo-Json | Set-Content $runnerState -Encoding utf8
    $out = (& powershell.exe -NoProfile -ExecutionPolicy Bypass -File $runner -Stop) -join "`n"
    Assert-That "-Stop reports what it killed" ($out -match "killed pid $wrapperPid") "got: $out"
    Start-Sleep -Milliseconds 800
    Assert-That "-Stop kills the recorded wrapper" (-not (Get-Process -Id $wrapperPid -ErrorAction SilentlyContinue)) "pid $wrapperPid survived"

    # THE ORPHAN. Killing the recorded pid alone left claude.exe running and
    # editing the worktree for five more minutes on 2026-07-26.
    $orphans = @($kids | Where-Object { Get-Process -Id $_.ProcessId -ErrorAction SilentlyContinue })
    Assert-That "-Stop kills the agent underneath, leaving no orphan" ($orphans.Count -eq 0) "orphaned: $($orphans.ProcessId -join ', ')"

    $out = (& powershell.exe -NoProfile -ExecutionPolicy Bypass -File $runner -Stop) -join "`n"
    Assert-That "-Stop on an already-finished run says so instead of erroring" ($out -match "is not running") "got: $out"
    Remove-Item $runnerState -Force -ErrorAction SilentlyContinue
    $out = (& powershell.exe -NoProfile -ExecutionPolicy Bypass -File $runner -Stop) -join "`n"
    Assert-That "-Stop with nothing recorded is not an error either" ($out -match "no run recorded") "got: $out"

    # THE ACTUAL BUG, asked of the runner itself rather than of a stub: what
    # will it hand to Start-Process on this machine? Reimplementing the
    # resolution here would only assert that two copies of the same idea agree,
    # which is exactly what a bug in the real one looks like.
    if (@(Get-Command claude -All -ErrorAction SilentlyContinue).Count -gt 0) {
        $resolved = ((& powershell.exe -NoProfile -ExecutionPolicy Bypass -File $runner -WhichClaude 2>$null) -join "").Trim()
        Assert-That "resolves claude to something Windows can execute" `
            ($resolved -and (Test-Path $resolved) -and ($resolved -match '\.(cmd|bat|exe)$')) `
            "resolved to '$resolved'; PATH has: $((Get-Command claude -All | ForEach-Object { $_.Source }) -join ', ')"
    }

    Remove-Item Env:\OPAL_RESUME_REPO_ROOT -ErrorAction SilentlyContinue

    # =========================================================================
    Write-Host "`nunattended-run (wake lock + outcome reporting)" -ForegroundColor Cyan
    # =========================================================================
    # The 2026-07-28 death: a run launched at 21:45, was frozen by Modern
    # Standby at 21:46:43 mid-sentence, committed nothing, wrote a 0-byte log,
    # and the runner's own log still said "launched" and nothing else. Both
    # halves of that are tested here - the machine is held awake, and what the
    # run achieved is written down whatever happens.
    $wrapper = Join-Path $hooksDir "unattended-run.ps1"
    Assert-That "unattended-run.ps1 exists" (Test-Path $wrapper)

    $wrapQueue = Join-Path $sandbox "wrap-queue"
    New-Item -ItemType Directory -Path $wrapQueue -Force | Out-Null
    $wrapLog = Join-Path $wrapQueue "resume-runner.log"
    $wrapPrompt = Join-Path $wrapQueue "prompt.txt"
    Set-Content $wrapPrompt "hello" -Encoding utf8

    # A clean throwaway repo, NOT $repoRoot. These assertions used to run
    # against the real working tree, which was harmless only for as long as the
    # wrapper ignored whether that tree was dirty. It does not any more (see
    # run-left-uncommitted below), so pointing them at the live repo would make
    # them pass or fail on whatever happened to be uncommitted at the time.
    $wrapRepo = Join-Path $sandbox "wrap-repo"
    New-Item -ItemType Directory -Path $wrapRepo -Force | Out-Null
    & git -C $wrapRepo init --quiet 2>$null | Out-Null
    & git -C $wrapRepo config core.autocrlf false 2>$null | Out-Null
    & git -C $wrapRepo config user.email "test@example.invalid" 2>$null | Out-Null
    & git -C $wrapRepo config user.name "hook tests" 2>$null | Out-Null
    Set-Content (Join-Path $wrapRepo "file.txt") "base" -Encoding utf8
    & git -C $wrapRepo add -A 2>$null | Out-Null
    & git -C $wrapRepo commit -m "base" --quiet 2>$null | Out-Null

    # A stub that produces output, i.e. the healthy case.
    $talkerCmd = Join-Path $sandbox "stub-talker.cmd"
    @('@echo off', 'echo did some work') -join "`r`n" | Set-Content $talkerCmd -Encoding ascii
    $runOut = Join-Path $wrapQueue "run-ok.log"
    # No -NoWakeLock here on purpose: this is the one place the real
    # SetThreadExecutionState path runs. It is held for about a second, which is
    # the whole point - a failure to acquire must be visible, not assumed.
    & powershell.exe -NoProfile -ExecutionPolicy Bypass -File $wrapper `
        -ClaudeCmd $talkerCmd -PromptFile $wrapPrompt -RunLog $runOut -QueueDir $wrapQueue -RepoRoot $wrapRepo 2>&1 | Out-Null
    $wrapText = (Get-Content $wrapLog -Raw -ErrorAction SilentlyContinue)
    Assert-That "a finished run is written to the decision log" ($wrapText -match "finished") "log: $wrapText"
    Assert-That "the outcome says how long, how much output, and how many commits" `
        (($wrapText -match "exit 0") -and ($wrapText -match "\dB stdout") -and ($wrapText -match "commit")) "log: $wrapText"
    Assert-That "the wake lock was acquired, not silently skipped" ($wrapText -notmatch "wake-lock-failed") "log: $wrapText"
    Assert-That "the agent actually ran under the wrapper" `
        ((Get-Content $runOut -Raw -ErrorAction SilentlyContinue) -match "did some work") "run log: $(Get-Content $runOut -Raw -ErrorAction SilentlyContinue)"

    # THE 2026-07-28 SIGNATURE ITSELF: exits clean, instantly, having written
    # nothing. Indistinguishable from success in the old log format, which is
    # why that death went unnoticed for a day.
    Set-Content $wrapLog "" -Encoding utf8
    $mimeCmd = Join-Path $sandbox "stub-mute.cmd"
    @('@echo off', 'exit /b 0') -join "`r`n" | Set-Content $mimeCmd -Encoding ascii
    $runMute = Join-Path $wrapQueue "run-mute.log"
    & powershell.exe -NoProfile -ExecutionPolicy Bypass -File $wrapper `
        -ClaudeCmd $mimeCmd -PromptFile $wrapPrompt -RunLog $runMute -QueueDir $wrapQueue -RepoRoot $wrapRepo -NoWakeLock 2>&1 | Out-Null
    $muteText = (Get-Content $wrapLog -Raw -ErrorAction SilentlyContinue)
    Assert-That "a run that dies early is named as such, not reported as finished" `
        (($muteText -match "run-died-early") -and ($muteText -notmatch "`tfinished")) "log: $muteText"

    # THE 2026-07-30 SIGNATURE: 9.3 minutes, exit 0, output written, and the
    # only thing it produced was an uncommitted working tree. Logged as
    # "finished" with "0 new commit(s)" sitting in plain sight.
    Set-Content $wrapLog "" -Encoding utf8
    $runDirty = Join-Path $wrapQueue "run-dirty.log"
    $dirtyCmd = Join-Path $sandbox "stub-dirty.cmd"
    # The stub is the unattended run: it edits a tracked file and commits
    # nothing, exactly as the real one did.
    @('@echo off', 'echo worked on something', "echo edited > `"$wrapRepo\file.txt`"") -join "`r`n" |
        Set-Content $dirtyCmd -Encoding ascii
    & powershell.exe -NoProfile -ExecutionPolicy Bypass -File $wrapper `
        -ClaudeCmd $dirtyCmd -PromptFile $wrapPrompt -RunLog $runDirty -QueueDir $wrapQueue -RepoRoot $wrapRepo -NoWakeLock 2>&1 | Out-Null
    $dirtyText = (Get-Content $wrapLog -Raw -ErrorAction SilentlyContinue)
    Assert-That "a run that changes files and commits nothing is not reported as finished" `
        (($dirtyText -match "run-left-uncommitted") -and ($dirtyText -notmatch "`tfinished")) "log: $dirtyText"
    Assert-That "the outcome line says whether commits ever left the machine" `
        ($dirtyText -match "unpushed") "log: $dirtyText"

    # The other direction, which is what stops the verdict being useless noise:
    # the same stub with nothing left behind must still read as finished.
    & git -C $wrapRepo checkout -- . 2>$null | Out-Null
    Set-Content $wrapLog "" -Encoding utf8
    & powershell.exe -NoProfile -ExecutionPolicy Bypass -File $wrapper `
        -ClaudeCmd $talkerCmd -PromptFile $wrapPrompt -RunLog (Join-Path $wrapQueue "run-clean.log") `
        -QueueDir $wrapQueue -RepoRoot $wrapRepo -NoWakeLock 2>&1 | Out-Null
    $cleanText = (Get-Content $wrapLog -Raw -ErrorAction SilentlyContinue)
    Assert-That "a run over a clean tree is still just finished" `
        (($cleanText -match "finished") -and ($cleanText -notmatch "run-left-uncommitted")) "log: $cleanText"

    # A launcher that cannot run at all must say so rather than vanish - the
    # same class of bug as the "%1 is not a valid Win32 application" fortnight.
    Set-Content $wrapLog "" -Encoding utf8
    & powershell.exe -NoProfile -ExecutionPolicy Bypass -File $wrapper `
        -ClaudeCmd (Join-Path $sandbox "does-not-exist.cmd") -PromptFile $wrapPrompt `
        -RunLog (Join-Path $wrapQueue "run-missing.log") -QueueDir $wrapQueue -RepoRoot $wrapRepo -NoWakeLock 2>&1 | Out-Null
    $missText = (Get-Content $wrapLog -Raw -ErrorAction SilentlyContinue)
    Assert-That "an unlaunchable agent is reported, not swallowed" ($missText -match "run-failed") "log: $missText"

    # And the runner must be wired to the wrapper, not to claude directly -
    # otherwise every assertion above tests a script nothing calls.
    Assert-That "resume-runner launches through the wrapper" `
        ((Get-Content $runner -Raw) -match "unattended-run\.ps1") "the runner no longer references unattended-run.ps1"

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

    # Autopilot dying silently, 2026-07-27. The marker and session record both
    # vanished mid-session and the gate then allowed every stop with no output
    # at all, so the only symptom was the maintainer having to prompt for each
    # continuation. These pin the three cases the fix has to keep apart.
    $vanishDir = Join-Path $sandbox "vanished"
    New-Item -ItemType Directory -Path $vanishDir -Force | Out-Null
    $gateHook = Join-Path $hooksDir "autopilot-gate.ps1"
    $vanishPayload = '{"session_id":"vanish-probe","stop_hook_active":false}'

    # Never armed here: must stay a complete no-op, or every ordinary
    # conversation in a fresh clone gets nagged.
    $env:OPAL_AUTOPILOT_QUEUE_DIR = $vanishDir
    $fresh = Invoke-HookRaw $gateHook $vanishPayload
    Assert-That "a repo that never armed autopilot is untouched" ([string]::IsNullOrWhiteSpace($fresh)) "got: $fresh"

    # Armed at some point (the state file proves it), no marker, no recorded
    # ending: the failure itself.
    Set-Content (Join-Path $vanishDir ".autopilot-state.json") '{"vanish-probe":3}' -Encoding utf8
    $reported = Invoke-HookRaw $gateHook $vanishPayload
    Assert-That "a vanished autopilot is reported" ($reported -match '"decision":"block"') "got: $reported"
    Assert-That "the report says how to re-arm" ($reported -match "expires_at") "got: $reported"

    # Once only. A gate that keeps blocking on a confused state traps the user
    # in a loop, which is worse than the silence it replaces.
    $again = Invoke-HookRaw $gateHook $vanishPayload
    Assert-That "it reports once and then stays quiet" ([string]::IsNullOrWhiteSpace($again)) "got: $again"

    # Expiry must leave its reason behind, so a future disappearance is
    # attributable instead of guessed at - which is what cost an hour here.
    Remove-Item (Join-Path $vanishDir ".autopilot-ended.json") -Force -ErrorAction SilentlyContinue
    Set-Content (Join-Path $vanishDir "AUTOPILOT") '{"expires_at":1,"max_iterations":20}' -Encoding utf8
    Invoke-HookRaw $gateHook $vanishPayload | Out-Null
    $endedRaw = Get-Content (Join-Path $vanishDir ".autopilot-ended.json") -Raw -Encoding UTF8 -ErrorAction SilentlyContinue
    Assert-That "an expired autopilot records why it ended" ($endedRaw -match "expired") "got: $endedRaw"
    $env:OPAL_AUTOPILOT_QUEUE_DIR = $queueDir

    # The Noticed section had no consumer at all until 2026-07-27: entries only
    # accumulated. On the evening the maintainer asked "was passiert eigentlich
    # mit den notizen?", an all-blocked "Now" section had this gate concluding
    # there was no work while five real entries sat in the same file.
    $noticedRepo = Join-Path $sandbox "noticed-backlog"
    New-Item -ItemType Directory -Path $noticedRepo -Force | Out-Null
    $nbQueue = Join-Path $noticedRepo "queue"
    New-Item -ItemType Directory -Path $nbQueue -Force | Out-Null
    $armed = @{ expires_at = ([DateTimeOffset]::UtcNow.AddHours(4).ToUnixTimeSeconds()); max_iterations = 20 } | ConvertTo-Json -Compress
    Set-Content (Join-Path $nbQueue "AUTOPILOT") $armed -Encoding utf8

    $blockedBacklog = Join-Path $noticedRepo "all-blocked.md"
    Set-Content $blockedBacklog @"
# Backlog
## Now
### Something waiting on the maintainer
**Blocked:** needs a human decision.
## Noticed
- **A rough edge nobody has fixed.** Worth a look.
- Another thing seen in passing.
## Done recently
- old
"@ -Encoding utf8

    $noticedParsed = Get-NoticedItems -BacklogPath $blockedBacklog
    Assert-That "Get-NoticedItems reads the Noticed bullets" ($noticedParsed.Count -eq 2) "got $($noticedParsed.Count)"
    Assert-That "it stops at the next section" (($noticedParsed -join " ") -notmatch "old") "leaked into Done recently"

    # The wiring, not just the parser - the gap that shipped the stall watchdog
    # connected to nothing.
    #
    # Busy check off from here on: these assertions are about backlog logic, and
    # the gate's "is a long job running?" check reads the whole machine's
    # process table. It failed these three exactly once on 2026-07-29 and then
    # passed four consecutive runs - some process with the repo path on its
    # command line was briefly alive. The check itself gets its own tests below,
    # with a process started on purpose to trip it.
    $env:OPAL_AUTOPILOT_SKIP_BUSY_CHECK = '1'
    $env:OPAL_AUTOPILOT_QUEUE_DIR = $nbQueue
    $env:OPAL_AUTOPILOT_BACKLOG = $blockedBacklog
    $fellBack = Invoke-HookRaw $gateHook '{"session_id":"noticed-fallback"}'
    Assert-That "an all-blocked Now falls back to Noticed instead of stopping" ($fellBack -match '"decision":"block"') "got: $fellBack"
    Assert-That "the fallback says the work came from Noticed" ($fellBack -match "Noticed") "got: $fellBack"

    # --- the instructions the message itself carries -------------------------
    # This block exists because the message rotted unnoticed for eight days. It
    # was telling every autopilot turn to file blocked work in
    # .claude/queue/blocked/ - retired 2026-07-22 and gitignored, so anything
    # filed there survives neither a clone nor the loss of one machine. Nothing
    # asserted on the message's content, so nothing caught it. These are cheap
    # and they pin the parts that go stale: where work is tracked, and how the
    # run is allowed to end.
    Assert-That "the message does not send blocked work to the retired queue" `
        ($fellBack -notmatch 'queue/blocked') "the gate is still citing .claude/queue/blocked/: $fellBack"
    Assert-That "it does not cite the retired todo directory either" `
        ($fellBack -notmatch 'queue/todo') "the gate is still citing .claude/queue/todo/: $fellBack"
    Assert-That "it names docs/BACKLOG.md as where a blocked item goes" `
        ($fellBack -match 'BACKLOG\.md') "got: $fellBack"
    Assert-That "it still names the local gate as the check to run" `
        ($fellBack -match 'dev\.ps1 all') "got: $fellBack"
    # AUTOPILOT and AUTOPILOT.OFF are live files the hooks really use, so those
    # two references must NOT be scrubbed along with the retired directories.
    Assert-That "the off switch is still named, since that is the maintainer's way out" `
        ($fellBack -match 'AUTOPILOT\.OFF') "got: $fellBack"

    # Second-class, not promoted: with real work under "Now" the gate must cite
    # BACKLOG, never the rough-edge list.
    $liveBacklog = Join-Path $noticedRepo "live.md"
    Set-Content $liveBacklog @"
# Backlog
## Now
### Real actionable work
Not blocked at all.
## Noticed
- A rough edge nobody has fixed.
## Done recently
- old
"@ -Encoding utf8
    $env:OPAL_AUTOPILOT_BACKLOG = $liveBacklog
    $normal = Invoke-HookRaw $gateHook '{"session_id":"noticed-normal"}'
    Assert-That "real Now work still outranks Noticed" ($normal -match "Real actionable work") "got: $normal"
    Assert-That "and is not labelled as coming from Noticed" ($normal -notmatch "rough edges rather than commitments") "got: $normal"

    # Genuinely nothing left must still end the run - a gate that never lets go
    # is worse than one that stops early.
    $emptyBacklog = Join-Path $noticedRepo "empty.md"
    Set-Content $emptyBacklog @"
# Backlog
## Now
### Waiting
**Blocked:** needs a human.
## Noticed
## Done recently
- old
"@ -Encoding utf8
    $env:OPAL_AUTOPILOT_BACKLOG = $emptyBacklog
    $nothing = Invoke-HookRaw $gateHook '{"session_id":"noticed-empty"}'
    Assert-That "an all-blocked backlog with no notes still ends the run" ([string]::IsNullOrWhiteSpace($nothing)) "got: $nothing"

    # --- the busy check itself, with the switch back on ----------------------
    # Untested until 2026-07-29, and it is the branch that silently swallowed
    # three other assertions. It exists so a turn is not wasted blocking while a
    # long job runs: the harness re-invokes the assistant when a background
    # command finishes, so ending the turn is the cheap move.
    #
    # The fixture is cmd.exe with the repo root on its command line - the last
    # and broadest of the gate's four patterns, and the only one that can be
    # tripped without building a Go test binary. powershell.exe would prove
    # nothing: the gate deliberately excludes it, or every hook invocation would
    # look like a busy repo.
    Remove-Item Env:\OPAL_AUTOPILOT_SKIP_BUSY_CHECK -ErrorAction SilentlyContinue
    $env:OPAL_AUTOPILOT_BACKLOG = $liveBacklog
    $busyProc = Start-Process -FilePath 'cmd.exe' `
        -ArgumentList '/c', "ping -n 120 127.0.0.1 >nul & rem $repoRoot" `
        -WindowStyle Hidden -PassThru
    try {
        # Win32_Process must actually be able to see it, or the assertion below
        # passes for the wrong reason.
        $seen = $false
        $deadline = (Get-Date).AddSeconds(15)
        while (-not $seen -and (Get-Date) -lt $deadline) {
            Start-Sleep -Milliseconds 300
            $seen = [bool](Get-CimInstance Win32_Process -Filter "ProcessId=$($busyProc.Id)" -ErrorAction SilentlyContinue |
                           Where-Object { $_.CommandLine -like "*$repoRoot*" })
        }
        Assert-That "the busy-check fixture is visible to Win32_Process" $seen "pid $($busyProc.Id) has no command line the gate could match - this test proves nothing without one"

        $whileBusy = Invoke-HookRaw $gateHook '{"session_id":"busy-yes"}'
        Assert-That "a long job in this repo ends the turn instead of burning it" ([string]::IsNullOrWhiteSpace($whileBusy)) "got: $whileBusy"
    } finally {
        & taskkill.exe /PID $busyProc.Id /T /F 2>&1 | Out-Null
    }

    # And the other half: once it is gone, the same call must block again.
    # Without this the check could be stuck on and nothing would notice.
    $deadline = (Get-Date).AddSeconds(15)
    while ((Get-CimInstance Win32_Process -Filter "ProcessId=$($busyProc.Id)" -ErrorAction SilentlyContinue) -and (Get-Date) -lt $deadline) {
        Start-Sleep -Milliseconds 300
    }
    # Name the ambient condition rather than letting it masquerade as a gate
    # bug. This is the precondition whose absence produced an unexplained
    # one-off failure on 2026-07-29.
    $stillMatching = @(Get-CimInstance Win32_Process -ErrorAction SilentlyContinue | Where-Object {
        ($_.Name -eq 'go.exe' -and $_.CommandLine -and $_.CommandLine -match '\s(test|run)\s') -or
        ($_.Name -like '*.test.exe') -or
        ($_.Name -in @('opal-dl.exe', 'opal-downloader.exe')) -or
        ($_.CommandLine -and $_.CommandLine -like "*$repoRoot*" -and $_.Name -ne 'powershell.exe')
    })
    Assert-That "nothing else in this repo is running (precondition for the next assertion)" `
        ($stillMatching.Count -eq 0) `
        "these would legitimately make the gate stop: $(($stillMatching | ForEach-Object { "$($_.Name)($($_.ProcessId))" }) -join ', ')"

    $whenIdle = Invoke-HookRaw $gateHook '{"session_id":"busy-no"}'
    Assert-That "and with nothing running it continues normally" ($whenIdle -match '"decision":"block"') "got: $whenIdle"

    Remove-Item Env:\OPAL_AUTOPILOT_BACKLOG -ErrorAction SilentlyContinue
    $env:OPAL_AUTOPILOT_QUEUE_DIR = $queueDir

    # =========================================================================
    Write-Host "`npre-push-gate (PreToolUse)" -ForegroundColor Cyan
    # =========================================================================
    # This hook gates every push in the repo and had no tests at all until
    # 2026-07-30 - its own comment said "caught by the matcher tests below" and
    # there were none. What it gets wrong is invisible in the worst direction:
    # a matcher that under-matches means it silently never fires (which is
    # exactly what happened for months, see the hook's header), and one that
    # over-matches blocks ordinary commits.
    #
    # The real branch shells out to dev.ps1, which is what runs this suite, so
    # OPAL_PREPUSH_DEV_SCRIPT points it at a stub. The stub also records that it
    # ran, so "decided this was not a push" is proved by absence rather than
    # assumed from an exit code that a skipped run and a passing run share.
    $prepushHook = Join-Path $hooksDir "pre-push-gate.ps1"
    $prepushDir = Join-Path $sandbox "prepush"
    New-Item -ItemType Directory -Path $prepushDir -Force | Out-Null
    $ranMarker = Join-Path $prepushDir "dev-ran.txt"
    $stubPass = Join-Path $prepushDir "dev-pass.ps1"
    $stubFail = Join-Path $prepushDir "dev-fail.ps1"
    Set-Content $stubPass -Encoding utf8 -Value @"
"ran: `$args" | Add-Content '$ranMarker'
exit 0
"@
    Set-Content $stubFail -Encoding utf8 -Value @"
"ran: `$args" | Add-Content '$ranMarker'
Write-Host 'stub dev.ps1: pretending the suite failed'
exit 1
"@

    Assert-That "pre-push-gate.ps1 exists" (Test-Path $prepushHook)
    Assert-That "pre-push-gate is registered in settings.json" `
        ((Get-Content (Join-Path $repoRoot ".claude\settings.json") -Raw) -match "pre-push-gate")

    function Invoke-Prepush {
        <#  Returns the exit code, and whether the stub dev script ran. #>
        param([string]$Command, [string]$Stub = $stubPass)
        # Function-scoped, so it shadows the script's 'Stop' only in here. This
        # gate writes its refusal to stderr *by design*, and under 'Stop' that
        # comes back as a terminating NativeCommandError which aborted the whole
        # suite - the blocking test cannot assert on a refusal it is killed by.
        # 2>$null alone is not enough: the hook writes via [Console]::Error, and
        # the redirection still leaves PowerShell raising on the native exe.
        $ErrorActionPreference = 'Continue'
        Remove-Item $ranMarker -ErrorAction SilentlyContinue
        $env:OPAL_PREPUSH_DEV_SCRIPT = $Stub
        $payload = @{ tool_input = @{ command = $Command } } | ConvertTo-Json -Depth 5 -Compress
        $payload | & powershell.exe -NoProfile -ExecutionPolicy Bypass -File $prepushHook 2>$null | Out-Null
        $code = $LASTEXITCODE
        Remove-Item Env:\OPAL_PREPUSH_DEV_SCRIPT -ErrorAction SilentlyContinue
        return @{ Code = $code; Ran = (Test-Path $ranMarker) }
    }

    # --- the branch that must fire -----------------------------------------
    $r = Invoke-Prepush 'git push origin master'
    Assert-That "a plain push runs the check" $r.Ran "dev script did not run"
    Assert-That "and is allowed through when the check passes" ($r.Code -eq 0) "exit $($r.Code)"

    $r = Invoke-Prepush 'cd "C:\x\Opal_downloader" && git push -u origin some-branch'
    Assert-That "the compound form that once bypassed this gate for months is caught" $r.Ran "dev script did not run"

    $r = Invoke-Prepush 'git push --force-with-lease origin master'
    Assert-That "a force-push is gated too" $r.Ran "dev script did not run"

    $r = Invoke-Prepush 'git -C /some/repo push'
    Assert-That "git -C ... push is caught" $r.Ran "dev script did not run"

    # --- blocking, which is the half that must not fail open ---------------
    $r = Invoke-Prepush 'git push origin master' $stubFail
    Assert-That "a failing check blocks with exit 2, not 1" ($r.Code -eq 2) `
        "exit $($r.Code) - Claude Code only treats 2 as blocking, so any other code lets the push through"

    # --- the branch that must NOT fire -------------------------------------
    # Each of these ran dev.ps1 (~2 minutes) before this section existed, or
    # blocked a commit outright.
    $r = Invoke-Prepush 'git commit -m "push the parser harder"'
    Assert-That "a quoted commit message mentioning the word is not a push" (-not $r.Ran) "dev script ran"

    $heredoc = "git add docs/BACKLOG.md && git commit -q -F - <<'EOF'`nNote why the gate said push blocked`n`nFix it, then push again.`nEOF"
    $r = Invoke-Prepush $heredoc
    Assert-That "a heredoc commit body mentioning the word is not a push" (-not $r.Ran) `
        "dev script ran - the matcher's negated class is spanning newlines again, so the body glued onto the leading git"

    $r = Invoke-Prepush 'git push --dry-run origin master'
    Assert-That "a dry run changes nothing on the remote, so it is not gated" (-not $r.Ran) "dev script ran"

    $r = Invoke-Prepush 'git status'
    Assert-That "an unrelated git command is not a push" (-not $r.Ran) "dev script ran"

    $r = Invoke-Prepush 'npm run push-notifications'
    Assert-That "a non-git command containing the word is not a push" (-not $r.Ran) "dev script ran"

    # --- fail-open only where it cannot tell -------------------------------
    # Deliberately NOT routed through Invoke-Prepush: that helper wraps its
    # argument in a valid JSON payload, so handing it malformed text would test
    # the matcher against a harmless string and pass for the wrong reason.
    function Invoke-PrepushRaw {
        param([string]$Json, [string]$Stub = $stubPass)
        $ErrorActionPreference = 'Continue'
        Remove-Item $ranMarker -ErrorAction SilentlyContinue
        $env:OPAL_PREPUSH_DEV_SCRIPT = $Stub
        $Json | & powershell.exe -NoProfile -ExecutionPolicy Bypass -File $prepushHook 2>$null | Out-Null
        $code = $LASTEXITCODE
        Remove-Item Env:\OPAL_PREPUSH_DEV_SCRIPT -ErrorAction SilentlyContinue
        return @{ Code = $code; Ran = (Test-Path $ranMarker) }
    }

    $r = Invoke-PrepushRaw '{not json at all' $stubFail
    Assert-That "unreadable input fails open, so it cannot break every other Bash call" ($r.Code -eq 0) "exit $($r.Code)"
    Assert-That "and does not run the check on input it could not parse" (-not $r.Ran) "dev script ran"

    $r = Invoke-PrepushRaw '{"tool_input":{}}' $stubFail
    Assert-That "a payload carrying no command is not treated as a push" ($r.Code -eq 0) "exit $($r.Code)"

    # =========================================================================
    Write-Host "`nnoticed-gate (Stop)" -ForegroundColor Cyan
    # =========================================================================
    # Asks once per session for one thing noticed and not done. Every assertion
    # here is really about the same risk: a Stop hook that misfires traps the
    # user in a loop, which is far worse than a missed note.
    $noticedDir = Join-Path $sandbox "noticed"
    New-Item -ItemType Directory -Path $noticedDir -Force | Out-Null
    $env:OPAL_NOTICED_QUEUE_DIR = $noticedDir
    $noticedHook = Join-Path $hooksDir "noticed-gate.ps1"

    Assert-That "noticed-gate.ps1 exists" (Test-Path $noticedHook)
    Assert-That "noticed-gate is registered as a Stop hook" `
        ((Get-Content (Join-Path $repoRoot ".claude\settings.json") -Raw) -match "noticed-gate")

    $first = Invoke-HookRaw $noticedHook '{"session_id":"probe-a"}'
    Assert-That "it asks on the first stop of a session" ($first -match '"decision":"block"') "got: $first"
    Assert-That "the ask is for something noticed, not a summary of the work" ($first -match 'noticed but did not do')

    $second = Invoke-HookRaw $noticedHook '{"session_id":"probe-a"}'
    Assert-That "it does not ask twice in one session" ([string]::IsNullOrWhiteSpace($second)) "repeated: $second"

    $other = Invoke-HookRaw $noticedHook '{"session_id":"probe-b"}'
    Assert-That "a new session is asked again" ($other -match '"decision":"block"') "got: $other"

    # Another hook is already blocking, so the turn is not ending anyway.
    $active = Invoke-HookRaw $noticedHook '{"session_id":"probe-c","stop_hook_active":true}'
    Assert-That "it stays quiet when another hook already blocked the stop" ([string]::IsNullOrWhiteSpace($active)) "got: $active"

    $garbage = Invoke-HookRaw $noticedHook 'not json at all'
    Assert-That "unreadable input fails open" ([string]::IsNullOrWhiteSpace($garbage)) "blocked on garbage: $garbage"

    # The maintainer's off switch has to silence this too, or turning autopilot
    # off would still leave a hook holding the turn open.
    New-Item -ItemType File -Path (Join-Path $noticedDir "AUTOPILOT.OFF") -Force | Out-Null
    $offed = Invoke-HookRaw $noticedHook '{"session_id":"probe-d"}'
    Assert-That "AUTOPILOT.OFF silences it" ([string]::IsNullOrWhiteSpace($offed)) "got: $offed"
    Remove-Item (Join-Path $noticedDir "AUTOPILOT.OFF") -Force -ErrorAction SilentlyContinue

    Remove-Item Env:\OPAL_NOTICED_QUEUE_DIR -ErrorAction SilentlyContinue

    # The retired hook must be gone, not merely unreferenced.
    Assert-That "the superseded rate-limit-gate.ps1 is deleted" (-not (Test-Path (Join-Path $hooksDir "rate-limit-gate.ps1"))) "still present"
    Assert-That "nothing still references rate-limit-gate.ps1" ((Get-Content (Join-Path $repoRoot ".claude\settings.json") -Raw) -notmatch "rate-limit-gate")
}
finally {
    Remove-Item $sandbox -Recurse -Force -ErrorAction SilentlyContinue
    Remove-Item Env:\OPAL_AUTOPILOT_QUEUE_DIR -ErrorAction SilentlyContinue
    Remove-Item Env:\OPAL_RATE_LIMIT_STATUS -ErrorAction SilentlyContinue
    Remove-Item Env:\OPAL_NOTICED_QUEUE_DIR -ErrorAction SilentlyContinue
    # Leaking any of these would be worse than an ordinary stray variable: each
    # one DISABLES a guard. A leaked OPAL_RESUME_POWER_OVERRIDE=ac would let a
    # real unattended run start on battery, and a leaked
    # OPAL_AUTOPILOT_SKIP_BUSY_CHECK would have autopilot talking over a live
    # test run.
    Remove-Item Env:\OPAL_AUTOPILOT_SKIP_BUSY_CHECK -ErrorAction SilentlyContinue
    Remove-Item Env:\OPAL_RESUME_POWER_OVERRIDE -ErrorAction SilentlyContinue
    Remove-Item Env:\OPAL_RESUME_NO_WAKELOCK -ErrorAction SilentlyContinue
    Remove-Item Env:\OPAL_RESUME_CLAUDE_CMD -ErrorAction SilentlyContinue
    Remove-Item Env:\OPAL_RESUME_REPO_ROOT -ErrorAction SilentlyContinue
    Remove-Item Env:\OPAL_KEEPWARM_CMD -ErrorAction SilentlyContinue
    Remove-Item Env:\OPAL_UNATTENDED_RESUME -ErrorAction SilentlyContinue
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
