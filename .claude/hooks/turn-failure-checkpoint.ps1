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
        # .TrimStart([char]0xFEFF): found 2026-07-31 running the full hook
        # suite (not a single hook in isolation) - some PowerShell host paths
        # pipe a UTF-8 BOM ahead of the JSON on stdin, which is not valid JSON
        # and made ConvertFrom-Json fail silently, so every field below fell
        # back to its default ("unknown") with no error surfaced anywhere.
        # Same fix applied to every other hook with this identical read.
        $raw = [Console]::In.ReadToEnd().TrimStart([char]0xFEFF)
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
    # Test seam. Without it the suite would have to exercise ref pruning against
    # the real repo, and a suite that runs on every push must not be able to
    # delete the maintainer's actual recovery points to prove it can delete
    # things.
    if ($env:OPAL_CHECKPOINT_REPO_ROOT) { $repoRoot = $env:OPAL_CHECKPOINT_REPO_ROOT }

    # --- capture WIP without disturbing anything ------------------------------
    $wipRef = $null
    $wipSha = $null
    $dirty = @()
    try {
        Push-Location $repoRoot
        $dirty = @(& git status --porcelain 2>$null)
        if ($dirty.Count -gt 0) {
            $sha = (& git stash create "WIP checkpoint after $errorType" 2>$null)
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

    # --- keep the checkpoint set bounded --------------------------------------
    # 322 refs had piled up over seven days by 2026-07-30 (~46/day), each
    # pinning a whole tree so git can never collect it, and none had ever been
    # read after the day it was written. Pruned here by the writer rather than by
    # a script somebody has to remember, so the set is bounded by construction.
    #
    # Age AND a floor, not age alone: a quiet fortnight would otherwise delete
    # every recovery point the repo has, which is precisely when the oldest
    # surviving checkpoint is the only one left to matter. The ref name is the
    # creation timestamp, so age needs no extra git call.
    $prunedCount = 0
    $keepDays = 14
    $keepAtLeast = 20
    if ($env:OPAL_CHECKPOINT_KEEP_DAYS) { $keepDays = [int]$env:OPAL_CHECKPOINT_KEEP_DAYS }
    if ($env:OPAL_CHECKPOINT_KEEP_AT_LEAST) { $keepAtLeast = [int]$env:OPAL_CHECKPOINT_KEEP_AT_LEAST }
    try {
        Push-Location $repoRoot
        $allRefs = @(& git for-each-ref --format='%(refname)' refs/wip-checkpoints/ 2>$null | Where-Object { $_ })
        if ($allRefs.Count -gt $keepAtLeast) {
            $cutoff = $nowUnix - ([long]$keepDays * 86400)
            $stamped = $allRefs | ForEach-Object {
                $stamp = 0L
                if ($_ -match 'refs/wip-checkpoints/(\d+)$') { $stamp = [long]$Matches[1] }
                [pscustomobject]@{ Ref = $_; Stamp = $stamp }
            } | Sort-Object -Property Stamp -Descending
            # Stamp -gt 0 skips any ref whose name is not a timestamp: unknown
            # provenance is a reason to leave it alone, not to guess its age.
            $doomed = @($stamped | Select-Object -Skip $keepAtLeast |
                        Where-Object { $_.Stamp -gt 0 -and $_.Stamp -lt $cutoff })
            foreach ($d in $doomed) {
                & git update-ref -d $d.Ref 2>$null | Out-Null
                if ($LASTEXITCODE -eq 0) { $prunedCount++ }
            }
        }
    } catch { } finally { Pop-Location }

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
        checkpoints_pruned = $prunedCount
    }

    $json = $record | ConvertTo-Json -Depth 5
    Set-Content -Path (Join-Path $queueDir "LAST_FAILURE.json") -Value $json -Encoding utf8

    # Append-only history, so a repeat pattern is visible rather than each
    # failure overwriting the evidence of the last one.
    $logLine = "$nowIso`t$errorType`trate_limited=$looksRateLimited`tdirty=$($dirty.Count)`twip=$wipSha`t$($errorMessage -replace '\s+', ' ')"
    Add-Content -Path (Join-Path $queueDir "turn-failures.log") -Value $logLine -Encoding utf8
} catch { }

exit 0
