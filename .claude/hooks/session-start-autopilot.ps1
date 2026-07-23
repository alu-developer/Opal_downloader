# SessionStart hook: arms autopilot automatically instead of requiring the
# maintainer to run the PowerShell snippet from docs/agent-operating-model.md
# by hand at the start of every session, and hands the new session whatever
# the previous one left behind.
#
# WHY: the autopilot Stop hook (autopilot-gate.ps1) only engages when
# .claude/queue/AUTOPILOT exists, and that file was previously created
# manually. In practice it was rarely armed, so autopilot rarely ran even for
# sessions opened correctly in this directory. This closes that gap for the
# one case a hook CAN close: a session whose settings.json is actually
# loaded, i.e. one started in this directory. It cannot help a session opened
# elsewhere and pointed here by path - project settings are fixed to the
# directory the session started in, with no later re-scoping. See
# docs/agent-operating-model.md for that half of the problem.
#
# Only fires on a brand-new session (matcher "startup"), not resume/clear/
# compact/fork, so it never resets an in-flight session's budget.
#
# Since 2026-07-23 it also does the recovery half of the usage-limit incident
# fix: it reports a previous turn killed by the API (recorded by
# turn-failure-checkpoint.ps1), surfaces docs/RESUME.md, and refuses to arm a
# full autonomous stretch on a budget that is already spent - arming 4h/20
# continuations that the Stop gate will veto on its very first check is just
# noise dressed up as readiness.
#
# FAIL-OPEN: any error here must never block a session from starting.

$ErrorActionPreference = 'SilentlyContinue'
Set-StrictMode -Off

$queueDir = $env:OPAL_AUTOPILOT_QUEUE_DIR
if (-not $queueDir) { $queueDir = Join-Path $PSScriptRoot "..\queue" }
$marker = Join-Path $queueDir "AUTOPILOT"
$offSwitch = Join-Path $queueDir "AUTOPILOT.OFF"
$failureFile = Join-Path $queueDir "LAST_FAILURE.json"

# Respect the maintainer's off switch unconditionally - never re-arm over it.
if (Test-Path $offSwitch) { exit 0 }

if (-not (Test-Path $queueDir)) {
    try { New-Item -ItemType Directory -Path $queueDir -Force | Out-Null } catch { exit 0 }
}

$notes = @()

# --- did the last turn die on us? ---------------------------------------------
# Consumed and cleared here: it describes one specific past event, and leaving
# it in place would re-announce the same dead turn at every future startup.
if (Test-Path $failureFile) {
    try {
        $f = Get-Content $failureFile -Raw -ErrorAction Stop | ConvertFrom-Json -ErrorAction Stop
        $what = if ($f.looks_rate_limited) { "hit the usage limit" } else { "failed with $($f.error_type)" }
        $line = "PREVIOUS TURN DID NOT FINISH: a turn on $($f.recorded_at_iso) $what and was killed mid-run"
        if ($f.budget_at_failure) { $line += " (budget floor at the time: $($f.budget_at_failure))" }
        $line += "."
        if ($f.wip_commit) {
            $line += " Uncommitted work at that moment ($($f.dirty_file_count) file(s)) was captured as commit $($f.wip_commit)"
            if ($f.wip_ref) { $line += ", kept alive at $($f.wip_ref)" }
            $line += ". Inspect it with ``git show $($f.wip_commit)`` and restore with ``git stash apply $($f.wip_commit)`` if it is worth keeping - the working tree was NOT modified, so check whether that work is already present before applying anything."
        } else {
            $line += " The working tree was clean at that moment, so no uncommitted work was lost."
        }
        $notes += $line
    } catch { }
    Remove-Item $failureFile -Force -ErrorAction SilentlyContinue
}

# --- where was the last session up to? ----------------------------------------
$repoRoot = (Resolve-Path (Join-Path $PSScriptRoot "..\..")).Path
$resumePath = Join-Path $repoRoot "docs\RESUME.md"
if (Test-Path $resumePath) {
    try {
        $resume = (Get-Content $resumePath -Raw -ErrorAction Stop).Trim()
        # The placeholder state is a file whose only content is its own header
        # plus the "nothing in flight" line; treat that as no note at all.
        if ($resume -and $resume -notmatch '(?m)^_Nothing in flight\.') {
            $notes += "docs/RESUME.md is not empty - a previous session left work in flight. Read it before starting anything new:`n`n$resume"
        }
    } catch { }
}

# --- budget-aware arming ------------------------------------------------------
$expiresHours = 4
$maxIterations = 20
$budgetNote = $null
$armed = $true

try {
    $lib = Join-Path $PSScriptRoot "budget-lib.ps1"
    if (Test-Path $lib) {
        . $lib
        $budget = Get-BudgetFloor
        $rung = Get-BudgetRung $budget
        if ($rung -ge 3) {
            # The Stop gate stops at 5h>=75 / 7d>=80, which rung 3 already
            # exceeds. Arming here would create a marker whose first check
            # vetoes it.
            $armed = $false
            $budgetNote = "Autopilot NOT armed: $($budget.Reason), which is past the point where the Stop gate would refuse to continue anyway. Work normally, at whatever pace the maintainer asks for, and keep docs/RESUME.md current - budget is not a reason to refuse work, only a reason not to run unattended."
            Remove-Item $marker -Force -ErrorAction SilentlyContinue
        } elseif ($rung -eq 2) {
            $expiresHours = 2
            $maxIterations = 6
            $budgetNote = "Autopilot armed SHORT (${expiresHours}h, max $maxIterations continuations) because $($budget.Reason). Commit in small increments and keep docs/RESUME.md current."
        }
    }
} catch { }

if ($armed) {
    $expiresAt = [DateTimeOffset]::UtcNow.AddHours($expiresHours).ToUnixTimeSeconds()
    $cfg = @{ expires_at = $expiresAt; max_iterations = $maxIterations } | ConvertTo-Json -Compress
    try { $cfg | Set-Content $marker -Encoding utf8 -ErrorAction Stop } catch { exit 0 }
    if (-not $budgetNote) {
        $budgetNote = "Autopilot armed automatically for this session (expires in ${expiresHours}h, max $maxIterations continuations - see docs/agent-operating-model.md)."
    }
}

$context = @($budgetNote) + $notes + @(
    "Read docs/BACKLOG.md and start on the top unblocked item without waiting to be asked. Do not stop at the end of a task to ask whether to continue; the Stop hook will push back on that. Stop only for a genuine human decision (see the operating model doc's 'what still needs a human' section).",
    "Keep docs/RESUME.md current as you work - it is what survives a turn being killed mid-run."
) | Where-Object { $_ }

$out = @{
    hookSpecificOutput = @{
        hookEventName    = "SessionStart"
        additionalContext = ($context -join "`n`n")
    }
} | ConvertTo-Json -Depth 5 -Compress
Write-Output $out
exit 0
