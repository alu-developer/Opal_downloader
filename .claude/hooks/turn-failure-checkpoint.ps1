# StopFailure hook: fires when a turn ends because of an API error - which is
# what a usage-limit kill looks like from inside the harness.
#
# WHY THIS EXISTS (incident, 2026-07-23)
# --------------------------------------
# A run was killed by the 5-hour limit and left NO record at all. Not a log
# line, not a marker, nothing. Working out what had happened meant comparing
# commit timestamps against rate-limit window arithmetic, hours later. A
# failure mode the maintainer explicitly cares about was, until this hook,
# completely invisible after the fact.
#
# The Stop hook cannot cover this: a killed turn never reaches Stop. StopFailure
# is the only event that fires on this path.
#
# TWO JOBS
# --------
# 1. Record what happened, durably, so the next session (and the maintainer)
#    can see it instead of inferring it.
# 2. Preserve uncommitted work, WITHOUT touching the working tree.
#
# On (2): `git stash create` builds a commit object capturing the current WIP
# and prints its SHA while changing nothing - no stash ref, no index change, no
# checkout. Pointing a ref at it under refs/wip-checkpoints/ makes it survive
# gc and show up in `git log --all`, still without touching any branch or the
# tree. So recovery is possible after a kill, and a run that was NOT killed is
# entirely unaffected. Deliberately not a real commit on a branch: committing
# half-finished work unattended, at an arbitrary instant, is a side effect on
# the maintainer's repo that this hook has no business causing.
#
# Stdout and exit code are ignored for StopFailure, so this exists purely for
# its side effects. It must never throw.

$ErrorActionPreference = 'SilentlyContinue'
Set-StrictMode -Off

try {
    $errorType = "unknown"
    $errorMessage = ""
    $sessionId = "unknown"
    $raw = ""
    try {
        $raw = [Console]::In.ReadToEnd()
        if ($raw) {
            $payload = $raw | ConvertFrom-Json -ErrorAction Stop
            if ($payload.error_type) { $errorType = [string]$payload.error_type }
            if ($payload.error_message) { $errorMessage = [string]$payload.error_message }
            if ($payload.session_id) { $sessionId = [string]$payload.session_id }
        }
    } catch { }

    $queueDir = $env:OPAL_AUTOPILOT_QUEUE_DIR
    if (-not $queueDir) { $queueDir = Join-Path $PSScriptRoot "..\queue" }
    if (-not (Test-Path $queueDir)) {
        New-Item -ItemType Directory -Path $queueDir -Force | Out-Null
    }

    $repoRoot = (Resolve-Path (Join-Path $PSScriptRoot "..\..")).Path

    # --- capture WIP without disturbing anything ------------------------------
    $wipRef = $null
    $wipSha = $null
    $dirty = @()
    try {
        Push-Location $repoRoot
        $dirty = @(& git status --porcelain 2>$null)
        if ($dirty.Count -gt 0) {
            $sha = (& git stash create "autopilot WIP checkpoint after $errorType" 2>$null)
            if ($sha) {
                $wipSha = ([string]$sha).Trim()
                if ($wipSha) {
                    $wipRef = "refs/wip-checkpoints/$([DateTimeOffset]::UtcNow.ToUnixTimeSeconds())"
                    & git update-ref $wipRef $wipSha 2>$null | Out-Null
                }
            }
        }
    } catch { } finally { Pop-Location }

    $nowUnix = [DateTimeOffset]::UtcNow.ToUnixTimeSeconds()
    $nowIso = (Get-Date).ToUniversalTime().ToString("yyyy-MM-ddTHH:mm:ssZ")

    # Was this a usage limit, as opposed to any other API error? Matched loosely
    # on purpose: the exact wording is not contractual, and a false positive
    # only produces a slightly wrong sentence in a note.
    $blob = "$errorType $errorMessage"
    $looksRateLimited = $blob -match '(?i)rate.?limit|usage limit|quota|429|too many requests|upgrade to|resets? at'

    $record = [ordered]@{
        recorded_at        = $nowUnix
        recorded_at_iso    = $nowIso
        session_id         = $sessionId
        error_type         = $errorType
        error_message      = $errorMessage
        looks_rate_limited = [bool]$looksRateLimited
        dirty_file_count   = $dirty.Count
        wip_commit         = $wipSha
        wip_ref            = $wipRef
    }

    # Budget floor at the moment of death - the single most useful number for
    # deciding afterwards whether the guard's thresholds were calibrated right.
    try {
        $lib = Join-Path $PSScriptRoot "budget-lib.ps1"
        if (Test-Path $lib) {
            . $lib
            $b = Get-BudgetFloor
            $record.budget_at_failure = $b.Reason
        }
    } catch { }

    $json = $record | ConvertTo-Json -Depth 5
    Set-Content -Path (Join-Path $queueDir "LAST_FAILURE.json") -Value $json -Encoding utf8

    # Append-only history, so a repeat pattern is visible rather than each
    # failure overwriting the evidence of the last one.
    $logLine = "$nowIso`t$errorType`trate_limited=$looksRateLimited`tdirty=$($dirty.Count)`twip=$wipSha`t$($errorMessage -replace '\s+', ' ')"
    Add-Content -Path (Join-Path $queueDir "turn-failures.log") -Value $logLine -Encoding utf8
} catch { }

exit 0
