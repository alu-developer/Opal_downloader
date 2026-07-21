# Estimates recent token usage from Claude Code's own session transcripts, so
# the autopilot gate has a budget signal even when the real one is unavailable.
#
# WHY THIS EXISTS
#
# ~/.claude/rate-limit-status.json is the accurate source, but it is written
# only by the status line - which does NOT run in non-interactive or
# claude-desktop sessions. It was found 18 hours stale on 2026-07-21, still
# reporting "1%", while the account was in fact minutes from its 5-hour limit.
# So the gate was flying blind exactly when autonomy mattered.
#
# Checked before building this, all dead ends: the CLI exposes no usage
# command; `claude -p` does not refresh the file; the desktop app keeps no
# readable usage state; and ~/.claude.json's lastModelUsage is only written at
# session end.
#
# The transcripts, however, record `message.usage` per assistant message and
# are always present. Summing them over a rolling window is a genuine local
# signal - not the provider's own accounting, but the same shape.
#
# CALIBRATION, and its limits
#
# On 2026-07-21 the account hit its 5-hour limit. The rolling 5-hour weighted
# total at that moment was ~7.6M, which is what UsageCeiling5h below is set
# from. That is ONE observation on ONE plan. Treat the percentages as a
# trend indicator, not as the provider's number - and prefer the real file
# whenever it is fresh.

param(
    [string]$Out = "$env:USERPROFILE\.claude\usage-estimate.json"
)

$ErrorActionPreference = 'SilentlyContinue'

# Weighted tokens: output and cache-creation are the expensive parts; cache
# reads are deliberately excluded, as they dominate raw counts (374M vs 1M
# output in one session) while costing a fraction.
$UsageCeiling5h = 7600000
# Overridable so both branches of the gate can actually be exercised in a
# test, and so the ceiling can be re-calibrated without editing this file if
# a future limit hit gives a better observation.
if ($env:OPAL_USAGE_CEILING_5H) {
    $parsed = 0L
    if ([int64]::TryParse($env:OPAL_USAGE_CEILING_5H, [ref]$parsed) -and $parsed -gt 0) {
        $UsageCeiling5h = $parsed
    }
}

# NO 7-day ceiling is defined, on purpose. The 5-hour limit was actually hit
# on 2026-07-21, which gives one real calibration point. The 7-day limit never
# has been, so any number here would be invented - and a first attempt at
# 40M promptly reported 125% while the account was nowhere near its weekly
# cap. The 7-day total is therefore emitted raw, for a human to look at, and
# nothing gates on it.

$now = [DateTimeOffset]::UtcNow.ToUnixTimeSeconds()
$cut5h = $now - (5 * 3600)
$cut7d = $now - (7 * 24 * 3600)

$sum5h = 0L
$sum7d = 0L
$messages = 0

$projectsDir = Join-Path $env:USERPROFILE ".claude\projects"
if (-not (Test-Path $projectsDir)) {
    # Nothing to estimate from: write nothing rather than a misleading zero.
    exit 0
}

# Only files touched inside the 7-day window can contribute. Regex rather than
# ConvertFrom-Json per line: these transcripts run to thousands of lines each,
# and this has to finish inside a hook timeout.
$since = (Get-Date).AddDays(-8)
$files = Get-ChildItem -Path $projectsDir -Filter *.jsonl -Recurse -File -ErrorAction SilentlyContinue |
    Where-Object { $_.LastWriteTime -gt $since }

foreach ($file in $files) {
    foreach ($line in [System.IO.File]::ReadLines($file.FullName)) {
        if ($line -notmatch '"usage"') { continue }
        if ($line -notmatch '"timestamp":"([^"]+)"') { continue }
        $ts = $null
        try { $ts = [DateTimeOffset]::Parse($Matches[1]).ToUnixTimeSeconds() } catch { continue }
        if ($ts -lt $cut7d) { continue }

        $weighted = 0L
        if ($line -match '"output_tokens":(\d+)') { $weighted += [int64]$Matches[1] }
        if ($line -match '"cache_creation_input_tokens":(\d+)') { $weighted += [int64]$Matches[1] }
        if ($line -match '"input_tokens":(\d+)') { $weighted += [int64]$Matches[1] }
        if ($weighted -le 0) { continue }

        $messages++
        $sum7d += $weighted
        if ($ts -ge $cut5h) { $sum5h += $weighted }
    }
}

$pct5h = [math]::Round(100.0 * $sum5h / $UsageCeiling5h, 1)

$payload = [ordered]@{
    updated_at        = $now
    source            = "transcript-estimate"
    messages_counted  = $messages
    weighted_5h       = $sum5h
    weighted_7d       = $sum7d
    ceiling_5h        = $UsageCeiling5h
    estimated_pct_5h  = $pct5h
    note_7d           = "uncalibrated: the 7-day limit has never been hit, so weighted_7d is informational only and nothing gates on it"
}

try {
    $payload | ConvertTo-Json -Depth 5 | Set-Content $Out -Encoding utf8 -ErrorAction Stop
} catch { }

exit 0
