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

# Record that this hook ran at all, so a hook that silently stops firing can be
# attributed instead of looking like "nothing to report" (see hookbeat.ps1).
# try/catch because diagnostics must never be able to break the gate itself.
try { . (Join-Path $PSScriptRoot 'hookbeat.ps1'); Write-HookBeat -Name 'session-start-autopilot' } catch { }

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

# --- did the unattended resume runner fail to start anything? -----------------
# resume-runner.ps1 runs on a Windows scheduled task with no human attached. Its
# only output is a line in resume-runner.log, and nobody reads that file - which
# is how a broken launch path (it invoked npm's extensionless POSIX shim, which
# Windows cannot execute) survived two days and six failed hourly attempts while
# every gate above it reported healthy. A watchdog whose failures are invisible
# is worse than none: it looks like a working safety net.
#
# So: report unreported launch-failed lines to the next session that opens, once
# each. Timestamped high-water mark rather than deleting the log, because the
# log is also the evidence for diagnosing whatever went wrong.
$resumeLog = Join-Path $queueDir "resume-runner.log"
$reportState = Join-Path $queueDir ".resume-report-state.json"
if (Test-Path $resumeLog) {
    try {
        $lastSeen = ""
        if (Test-Path $reportState) {
            $rs = Get-Content $reportState -Raw -ErrorAction Stop | ConvertFrom-Json -ErrorAction Stop
            if ($rs.last_reported) { $lastSeen = [string]$rs.last_reported }
        }
        $failures = @()
        $newest = $lastSeen
        foreach ($line in @(Get-Content $resumeLog -Tail 200 -ErrorAction Stop)) {
            # "<iso8601>`t<decision>`t<detail>"
            $parts = $line -split "`t"
            if ($parts.Count -lt 2 -or $parts[1] -ne "launch-failed") { continue }
            if ($parts[0] -le $lastSeen) { continue }
            $failures += $line
            if ($parts[0] -gt $newest) { $newest = $parts[0] }
        }
        if ($failures.Count -gt 0) {
            $detail = ($failures | Select-Object -Last 3) -join "`n"
            $notes += "THE SCHEDULED RESUME RUNNER COULD NOT START A SESSION: $($failures.Count) launch-failed entry/entries in .claude/queue/resume-runner.log since this was last reported. Its gates decided a resume was warranted and then the launch itself failed, so no unattended work happened. Treat this as a bug to fix, not a log line. Most recent:`n`n$detail"
            @{ last_reported = $newest } | ConvertTo-Json -Compress |
                Set-Content $reportState -Encoding utf8 -ErrorAction SilentlyContinue
        }
    } catch { }
}

# --- has a high-frequency hook gone silent? ------------------------------------
# The self-monitoring half of hookbeat.ps1: a hook that stops firing produces
# no error and no log line, so the only way to notice is comparing its last
# beat against something that could only have happened if it were still
# running - here, the newest commit (see Test-HookLiveness for why that's a
# safe comparison). This is exactly the class of failure that cost
# 2026-07-27: the autopilot gate had been dead all session and only the
# maintainer, hours later, noticed.
$repoRoot = (Resolve-Path (Join-Path $PSScriptRoot "..\..")).Path
try {
    $beatLib = Join-Path $PSScriptRoot "hookbeat.ps1"
    if (Test-Path $beatLib) {
        . $beatLib
        $latestCommitUnix = & git -C $repoRoot log -1 --format=%ct 2>$null
        if ($latestCommitUnix -match '^\d+$') {
            $latestCommitAt = [DateTimeOffset]::FromUnixTimeSeconds([int64]$latestCommitUnix).UtcDateTime
            $deadHooks = @(Test-HookLiveness -RepoRoot $repoRoot -LatestCommitAt $latestCommitAt)
            if ($deadHooks.Count -gt 0) {
                $notes += "SELF-AUDIT: possible dead hook(s) - " + ($deadHooks -join '; ') + ". A hook that silently stops firing looks identical to nothing-to-report; treat this as a bug to fix (see docs/work-quality.md), not a log line to skim past."
            }
        }
    }
} catch { }

# --- where was the last session up to? ----------------------------------------
$resumePath = Join-Path $repoRoot "docs\RESUME.md"
if (Test-Path $resumePath) {
    try {
        # -Encoding UTF8 is required: RESUME.md has no BOM, and without it
        # Windows PowerShell 5.1 reads the file as the system ANSI codepage,
        # mangling every non-ASCII character (e.g. em dashes) before this
        # text is embedded verbatim into the hook's JSON output.
        $resume = (Get-Content $resumePath -Raw -Encoding UTF8 -ErrorAction Stop).Trim()
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

# An unattended run started by resume-runner.ps1 gets a much smaller allowance
# than a session the maintainer is sitting in front of. Nobody is watching it,
# so its worst case has to be bounded by construction rather than by someone
# noticing. The `claude` CLI has no --max-turns, so the iteration cap in this
# marker IS the bound.
$unattended = ($env:OPAL_UNATTENDED_RESUME -eq "1")
if ($unattended) {
    $expiresHours = 2
    $maxIterations = 5
}

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
        } elseif ($rung -eq 2 -and -not $unattended) {
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
    if ($unattended) {
        $budgetNote = "UNATTENDED RESUME RUN. Autopilot armed tight (${expiresHours}h, max $maxIterations continuations) because nobody is watching this one. Nothing you leave uncommitted survives it, and decisions reserved for the maintainer stay reserved - write the open question into docs/BACKLOG.md and move on rather than deciding it yourself."
    } elseif (-not $budgetNote) {
        $budgetNote = "Autopilot armed automatically for this session (expires in ${expiresHours}h, max $maxIterations continuations - see docs/agent-operating-model.md)."
    }
}

# --- too many tokens for too little: budget floor vs. commits since last start -
# The second of the three symptoms in the maintainer's 2026-07-30 request
# ("hier zu viele tokens... usw"), and the harder one: there is no per-session
# token count available to a hook, but the budget floor (see budget-lib.ps1)
# is already trusted elsewhere in this repo as a usage proxy. Comparing it
# across sessions, against how many commits landed in between, operationalizes
# "spent a lot, shipped nothing" without needing a number hooks cannot get.
#
# Deliberately conservative, matching Get-BudgetRung's own philosophy: only
# flags a *rise* within the SAME window (a window reset - usage only climbs
# within a window, so Now < Prev means it rolled over - is not a signal and is
# skipped), gated behind both a real threshold (15 points - a meaningful
# fraction of a whole rung) and zero commits, and only compared against a
# previous state whose commit is a real ancestor of HEAD (a rebase, an amend,
# or a first-ever run all fall back to "no baseline yet" rather than guessing).
try {
    $auditState = Join-Path $queueDir ".session-budget-audit.json"
    $nowHead = (& git -C $repoRoot rev-parse HEAD 2>$null)
    if ($budget -and $budget.Known -and $nowHead) {
        $prev = $null
        if (Test-Path $auditState) {
            try { $prev = Get-Content $auditState -Raw -ErrorAction Stop | ConvertFrom-Json -ErrorAction Stop } catch { $prev = $null }
        }
        if ($prev -and $prev.commit) {
            & git -C $repoRoot merge-base --is-ancestor $prev.commit $nowHead 2>$null
            if ($LASTEXITCODE -eq 0) {
                $commitsSince = $null
                $commitsRaw = (& git -C $repoRoot rev-list --count "$($prev.commit)..$nowHead" 2>$null)
                if ($commitsRaw -match '^\d+$') { $commitsSince = [int]$commitsRaw }

                if ($null -ne $commitsSince -and $commitsSince -eq 0) {
                    foreach ($w in @(
                            @{ Name = "5h"; Prev = $prev.five_hour; Now = $budget.FiveHour }
                            @{ Name = "7d"; Prev = $prev.seven_day; Now = $budget.SevenDay })) {
                        if ($null -eq $w.Prev -or $null -eq $w.Now) { continue }
                        if ($w.Now -lt $w.Prev) { continue }  # window reset, not a signal
                        $rise = $w.Now - $w.Prev
                        if ($rise -ge 15) {
                            $notes += "SELF-AUDIT: the $($w.Name) budget floor rose from $($w.Prev)% to $($w.Now)% since the last session started, but 0 commits landed in between. That is the 'too many tokens for too little' pattern the maintainer flagged 2026-07-30 - worth asking what the previous session actually spent that turn on."
                        }
                    }
                }
            }
        }
        @{
            commit     = $nowHead
            five_hour  = $budget.FiveHour
            seven_day  = $budget.SevenDay
            recorded_at = (Get-Date).ToString('o')
        } | ConvertTo-Json -Compress | Set-Content $auditState -Encoding utf8 -ErrorAction SilentlyContinue
    }
} catch { }

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
