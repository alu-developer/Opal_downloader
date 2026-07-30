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
if ($rung -le 0) { Allow-Silently }

$now = [DateTimeOffset]::UtcNow.ToUnixTimeSeconds()

# --- hard stop on new subagents at the top rung -------------------------------
# Folded in from the old rate-limit-gate.ps1, which was wired to the Agent
# matcher alone. Kept as a real deny (not just advice) because a subagent is
# the one action that can multiply spend without the assistant seeing it climb.
# Unlike its predecessor this now runs through the shared floor logic, so it no
# longer gates on a window that has already rolled over.
if ($rung -ge 3 -and $toolName -eq 'Agent') {
    $reason = "Rate-limit guard: $($budget.Reason). Not starting new subagents - " +
              "finish and checkpoint the work already in flight instead."
    $out = @{
        hookSpecificOutput = @{
            hookEventName            = "PreToolUse"
            permissionDecision       = "deny"
            permissionDecisionReason = $reason
        }
    } | ConvertTo-Json -Depth 5 -Compress
    Write-Output $out
    exit 0
}

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
    1 {
        $msg = @"
BUDGET NOTICE - $($budget.Reason).

$common

Nothing to change yet beyond working in savable increments: commit each piece as it becomes correct rather than batching several up, and keep docs/RESUME.md pointing at what you are actually doing right now.
"@
    }
    2 {
        $msg = @"
BUDGET CHECKPOINT - $($budget.Reason).

$common

A usage-limit kill from here would end the turn instantly, with no Stop hook and no chance to save anything. Before your next substantial step:

1. Commit whatever is already correct, even if the task is unfinished. A WIP commit is recoverable; an uncommitted working tree plus a lost context window is not.
2. Update docs/RESUME.md: the task, what is done, the immediate next action, and anything you learned that is not yet written down anywhere else.

Then carry on. Prefer decisive single experiments over exploratory back-and-forth, and filter command output at the source rather than reading long logs into context.
"@
    }
    default {
        $msg = @"
BUDGET CRITICAL - $($budget.Reason).

$common

Assume the turn may be killed at any moment. Right now, before anything else: commit what is correct and write docs/RESUME.md so the next session resumes without re-deriving anything.

After that, keep going but stay permanently savable - land small commits, avoid starting work that only pays off if a long turn completes, and do not launch subagents (that is denied at this level).

Do NOT stop the run over this. Budget is not a stop condition; the Stop gate decides when a run ends, or the maintainer does.
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
