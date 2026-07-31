# PreToolUse hook: the only rate-limit check that runs DURING a turn.
#
# WHY THIS EXISTS (incident, 2026-07-23)
# --------------------------------------
# An autonomous run was killed mid-turn by the 5-hour usage limit. Every guard
# the repo had was on the Stop hook, i.e. BETWEEN turns - so the session sailed
# past its budget inside a single long turn with nothing watching, and the
# session record showed only 1-2 autopilot continuations against a cap of 20.
# The gate never got a chance to fire. Reconstructing what had even happened
# meant reading commit timestamps against window-reset arithmetic, because
# nothing recorded the kill either (see turn-failure-checkpoint.ps1 for that
# half).
#
# WHAT THIS CAN AND CANNOT DO
# ---------------------------
# It cannot predict the wall. The underlying data is a stale-by-up-to-an-hour
# floor (see budget-lib.ps1), and the one previous attempt at a precise
# estimator was removed the day it was written for reporting 83.5% against a
# real 46%. So this does NOT try to stop work at exactly the right moment.
#
# Instead it makes hitting the wall cheap: as the floor climbs it tells the
# assistant, mid-turn, to commit what it has and write down where it got to.
# A kill then costs the current turn instead of the session's whole train of
# thought. Cheap-to-lose work is the achievable goal; perfect avoidance is not.
#
# COST DISCIPLINE
# ---------------
# This runs before EVERY tool call, so it must stay a couple of file reads and
# nothing else. In particular it never invokes rate-limit-keepwarm.ps1 - that
# launches a process and can block for 42s, which is fine once per turn from
# the Stop gate and catastrophic here. And its own advice is throttled: an
# unthrottled reminder on every tool call would burn the very budget it is
# trying to protect.
#
# FAIL-OPEN: any error exits 0 with no output, which lets the tool call proceed.

$ErrorActionPreference = 'SilentlyContinue'
Set-StrictMode -Off

# Record that this hook ran at all, so a hook that silently stops firing can be
# attributed instead of looking like "nothing to report" (see hookbeat.ps1).
# try/catch because diagnostics must never be able to break the gate itself.
try { . (Join-Path $PSScriptRoot 'hookbeat.ps1'); Write-HookBeat -Name 'budget-guard' } catch { }

function Allow-Silently { exit 0 }

# --- input --------------------------------------------------------------------
$sessionId = "unknown"
$toolName = ""
try {
    $raw = [Console]::In.ReadToEnd()
    if ($raw) {
        $payload = $raw | ConvertFrom-Json -ErrorAction Stop
        if ($payload.session_id) { $sessionId = [string]$payload.session_id }
        if ($payload.tool_name) { $toolName = [string]$payload.tool_name }
    }
} catch { }

$queueDir = $env:OPAL_AUTOPILOT_QUEUE_DIR
if (-not $queueDir) { $queueDir = Join-Path $PSScriptRoot "..\queue" }
if (-not (Test-Path $queueDir)) {
    try { New-Item -ItemType Directory -Path $queueDir -Force | Out-Null } catch { Allow-Silently }
}

# --- heartbeat: "somebody is working in this tree right now" -------------------
# For resume-runner.ps1. It used to check only whether a PREVIOUS UNATTENDED run
# was alive, which said nothing about the maintainer having a session open - so
# the first hour its launch path actually worked (2026-07-26), it started a
# second agent into a worktree an interactive session was already editing.
#
# This hook is the right place to stamp it because it fires on every tool call:
# a session doing work stamps continuously, and an idle `claude` (the keep-warm
# process, which is permanently alive and must NOT count as work) never stamps
# at all. Checking for live `claude` processes instead would have deadlocked on
# exactly that.
#
# Deliberately before the budget check below, which returns early on a healthy
# budget - that is when a session is most likely to be running.
try {
    @{ at = [DateTimeOffset]::UtcNow.ToUnixTimeSeconds(); session_id = $sessionId } |
        ConvertTo-Json -Compress |
        Set-Content (Join-Path $queueDir ".session-heartbeat.json") -Encoding utf8 -ErrorAction Stop
} catch { }

$lib = Join-Path $PSScriptRoot "budget-lib.ps1"
if (-not (Test-Path $lib)) { Allow-Silently }
. $lib

$budget = Get-BudgetFloor
$rung = Get-BudgetRung $budget

# Rung 1 is deliberately silent (2026-07-31). It fired at 50% of a 5-hour
# window - i.e. most of a normal working session - and said nothing that
# "commit as you go" doesn't already cover. Every one of those notices was a
# mid-turn interruption bought for no information, and the accumulated effect
# was a run that behaved as though it were always nearly out of budget.
if ($rung -le 1) { Allow-Silently }

$now = [DateTimeOffset]::UtcNow.ToUnixTimeSeconds()

# NOTE: this hook does not deny anything. It used to hard-deny the Agent tool at
# rung 3, inherited from the old rate-limit-gate.ps1. That was removed on
# 2026-07-31: denying the one tool that parallelises work is a strange way to
# spend less, and the deny arrived exactly when a run most needed to finish
# something. Advice only - the assistant decides what to do with it.

# --- throttle -----------------------------------------------------------------
# Speak up when the rung first rises, then at most every 15 minutes while it
# holds. Re-notifying on every tool call would be self-defeating.
$statePath = Join-Path $queueDir ".budget-guard-state.json"

$state = $null
if (Test-Path $statePath) {
    try { $state = Get-Content $statePath -Raw -ErrorAction Stop | ConvertFrom-Json -ErrorAction Stop } catch { $state = $null }
}
if ($null -eq $state) { $state = [pscustomobject]@{} }

$prevRung = 0
$lastEmit = 0
if ($state.PSObject.Properties.Name -contains $sessionId) {
    $entry = $state.$sessionId
    if ($null -ne $entry.rung) { $prevRung = [int]$entry.rung }
    if ($null -ne $entry.last_emit) { $lastEmit = [int64]$entry.last_emit }
}

$throttleSeconds = 900
$shouldEmit = ($rung -gt $prevRung) -or (($now - $lastEmit) -ge $throttleSeconds)
if (-not $shouldEmit) { Allow-Silently }

$state | Add-Member -NotePropertyName $sessionId -NotePropertyValue ([pscustomobject]@{
        rung      = $rung
        last_emit = $now
    }) -Force
try { $state | ConvertTo-Json -Depth 5 | Set-Content $statePath -Encoding utf8 -ErrorAction Stop } catch { }

# --- the advice ---------------------------------------------------------------
$common = @"
This is a budget floor, not a measurement: the real figure is at least this and may be an hour's worth higher. Do not reason about "how much is left".
"@

switch ($rung) {
    2 {
        $msg = @"
BUDGET CHECKPOINT - $($budget.Reason).

$common

A usage-limit kill from here would end the turn instantly, with no Stop hook. So: commit whatever is already correct, and make sure docs/RESUME.md names what you are in the middle of. A WIP commit is recoverable; an uncommitted tree plus a lost context window is not.

That is the whole ask. Keep working on what you were working on - at full size. Do not switch to smaller tasks, do not skip the harder half, do not stop.
"@
    }
    default {
        $msg = @"
BUDGET CRITICAL - $($budget.Reason).

$common

Assume the turn may be killed at any moment, so commit what is correct now and keep docs/RESUME.md pointing at the next concrete action. That is what survives a kill.

Then carry on with the same work. Budget is not a stop condition and not a reason to shrink the task - being savable is about how often you commit, not about how much you attempt.
"@
    }
}

$out = @{
    hookSpecificOutput = @{
        hookEventName     = "PreToolUse"
        additionalContext = $msg
    }
} | ConvertTo-Json -Depth 5 -Compress
Write-Output $out
exit 0
