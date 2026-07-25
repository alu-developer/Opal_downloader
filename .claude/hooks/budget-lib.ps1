# Shared reader for ~/.claude/rate-limit-status.json.
#
# Dot-sourced by autopilot-gate.ps1 (Stop) and budget-guard.ps1 (PreToolUse).
# It exists because those two used to carry their own copies of this logic and
# DISAGREED: the Stop gate applied a per-window staleness rule, while
# rate-limit-gate.ps1 (now folded into budget-guard.ps1) had no freshness check
# at all and would happily gate on a number from a window that had already
# rolled over. docs/agent-operating-model.md flagged that as "worth fixing";
# this is the fix, and one implementation is the only way it stays fixed.
#
# THE CENTRAL FACT ABOUT THIS DATA: a reading is a FLOOR, not a measurement.
# The status line is the file's only writer, and the keep-warm process only
# re-stamps cached numbers while idle - real numbers refresh when a new `claude`
# process starts (hourly). So a reading can be up to an hour behind. Within a
# window usage only ever climbs, so "last seen 60%" means "now at least 60%".
# Treat it as a lower bound and never as "we have 40% left".

Set-StrictMode -Off

function Get-BudgetFloor {
    <#
    .SYNOPSIS
      Returns the last-known lower bound on 5-hour and 7-day usage.

    .OUTPUTS
      PSCustomObject with:
        Known      [bool]   - true if at least one window produced a usable number
        FiveHour   [double] - percent used, or $null if unusable
        SevenDay   [double] - percent used, or $null if unusable
        AgeSeconds [double] - age of the file, or $null if unreadable
        Reason     [string] - short human-readable explanation, for hook messages
    #>
    param(
        # OPAL_RATE_LIMIT_STATUS lets scripts/test-hooks.ps1 point the whole
        # hook family at a synthetic status file. Tests must never read the
        # real one: its numbers change under them, so assertions would pass or
        # fail depending on the maintainer's actual usage that hour.
        [string]$StatusPath = $(
            if ($env:OPAL_RATE_LIMIT_STATUS) { $env:OPAL_RATE_LIMIT_STATUS }
            else { Join-Path $env:USERPROFILE ".claude\rate-limit-status.json" }
        ),
        [int64]$Now = [DateTimeOffset]::UtcNow.ToUnixTimeSeconds()
    )

    $result = [pscustomobject]@{
        Known      = $false
        FiveHour   = $null
        SevenDay   = $null
        AgeSeconds = $null
        Reason     = "no rate-limit status file"
    }

    if (-not (Test-Path $StatusPath)) { return $result }

    try {
        $data = Get-Content $StatusPath -Raw -ErrorAction Stop | ConvertFrom-Json -ErrorAction Stop
    } catch {
        $result.Reason = "rate-limit status file unreadable"
        return $result
    }

    if ($null -eq $data.updated_at) {
        $result.Reason = "rate-limit status file has no updated_at"
        return $result
    }
    $age = $Now - [double]$data.updated_at
    $result.AgeSeconds = $age

    # Per-window usability, not one blanket freshness check.
    #
    # A window whose resets_at has PASSED rolled over after the file was
    # written, so its percentage describes a window that no longer exists.
    # Using it anyway is not merely inaccurate, it is self-perpetuating: a
    # stale-and-expired "92%" would gate every future run forever, long after
    # the real quota came back. So an expired window reads as unknown.
    #
    # A window still inside its period is usable at ANY age, because usage
    # only climbs within a window - the old number is a valid floor.
    #
    # resets_at missing entirely: fall back to plain freshness, since there is
    # no way to tell whether the window rolled over.
    foreach ($w in @(
            @{ Name = "FiveHour"; Node = $data.five_hour },
            @{ Name = "SevenDay"; Node = $data.seven_day })) {

        $node = $w.Node
        if ($null -eq $node -or $null -eq $node.used_percentage) { continue }

        $usable = $false
        if ($null -ne $node.resets_at) {
            if ([double]$node.resets_at -gt $Now) { $usable = $true }
        } elseif ($age -lt 600) {
            $usable = $true
        }

        if ($usable) {
            $result.($w.Name) = [double]$node.used_percentage
            $result.Known = $true
        }
    }

    if ($result.Known) {
        $mins = [math]::Round($age / 60)
        $parts = @()
        if ($null -ne $result.FiveHour) { $parts += "5h >=$($result.FiveHour)%" }
        if ($null -ne $result.SevenDay) { $parts += "7d >=$($result.SevenDay)%" }
        $result.Reason = ($parts -join ", ") + " (floor, read ${mins}m ago)"
    } else {
        $result.Reason = "no usable rate-limit reading (all windows expired or absent)"
    }

    return $result
}

function Get-BacklogItems {
    <#
    .SYNOPSIS
      Parses docs/BACKLOG.md's "### " headings (before "## Done recently")
      into items, each flagged Blocked if the first non-blank line of its body
      starts with "**Blocked:**".

    .DESCRIPTION
      Shared by autopilot-gate.ps1 (should a turn keep going?) and
      resume-runner.ps1 (should a whole new unattended session be launched?).
      Both used to count every "### " heading as work, with no way to tell a
      genuinely actionable item from one already written up as waiting on a
      maintainer decision or a live/attended session - so an all-blocked
      backlog still forced endless mid-turn nagging and hourly relaunches that
      could never make progress. "**Blocked:**" is prose already used
      elsewhere in this file for exactly this situation; this just makes it
      machine-readable too.

    .OUTPUTS
      Array of PSCustomObject { Title; Blocked }
    #>
    param([Parameter(Mandatory)][string]$BacklogPath)

    $items = @()
    if (-not (Test-Path $BacklogPath)) { return $items }

    $current = $null
    foreach ($line in @(Get-Content $BacklogPath -ErrorAction SilentlyContinue)) {
        if ($line -match '^##\s+Done recently') { break }
        if ($line -match '^###\s+(.+)$') {
            if ($null -ne $current) { $items += $current }
            $current = [pscustomobject]@{ Title = $Matches[1].Trim(); Blocked = $false; SeenBody = $false }
            continue
        }
        if ($null -ne $current -and -not $current.SeenBody) {
            if ($line.Trim() -eq '') { continue }
            if ($line -match '^\*\*Blocked:') { $current.Blocked = $true }
            $current.SeenBody = $true
        }
    }
    if ($null -ne $current) { $items += $current }
    return $items
}

function Get-BudgetRung {
    <#
    .SYNOPSIS
      Maps a budget floor onto an escalating severity rung.

    .DESCRIPTION
      The ladder is deliberately conservative because the input is a floor that
      may be an hour old: by the time "70%" is visible, the true figure can
      already be well past it. Rungs are about how much unsaved work is
      acceptable to be carrying, NOT about predicting the exact moment of the
      wall - that prediction is not possible with this data and attempting it
      is what produced the miscalibrated estimator removed on 2026-07-21.

        0 none      - nothing to say, hook stays silent
        1 notice    - past halfway; keep the resume note current
        2 checkpoint- commit WIP now, a kill from here costs the turn
        3 critical  - checkpoint immediately; refuse to start new subagents
    #>
    param($Budget)

    if ($null -eq $Budget -or -not $Budget.Known) { return 0 }

    $five = $Budget.FiveHour
    $seven = $Budget.SevenDay

    if (($null -ne $five -and $five -ge 80) -or ($null -ne $seven -and $seven -ge 85)) { return 3 }
    if (($null -ne $five -and $five -ge 70) -or ($null -ne $seven -and $seven -ge 80)) { return 2 }
    if (($null -ne $five -and $five -ge 50) -or ($null -ne $seven -and $seven -ge 65)) { return 1 }
    return 0
}
