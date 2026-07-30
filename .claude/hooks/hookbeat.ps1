# Dot-sourced by every wired hook. Records that the hook actually ran.
#
# WHY (maintainer's request, 2026-07-30): "ich muss dir nicht immer sagen: ...
# da hook nicht funktioniert". A hook that stops firing is invisible - it does
# not error, it does not log, it simply never speaks again, and everything
# downstream looks exactly like "there was nothing to say". That is not a
# hypothetical: on 2026-07-27 the autopilot gate had been dead all session and
# the only thing that noticed was the maintainer, hours later.
#
# The existing .session-heartbeat.json (budget-guard.ps1) answers a different
# question - "is somebody working in this tree" - and only that one hook writes
# it. This writes one file PER HOOK, so silence can be attributed.
#
# Fail-open, always. A heartbeat is diagnostics; a hook must never break
# because its bookkeeping did. Every call site wraps this in try/catch too.

# Resolves the beats directory the same way every sibling piece of hook state
# does (the AUTOPILOT marker, .session-heartbeat.json, resume-runner.log):
# $env:OPAL_AUTOPILOT_QUEUE_DIR first, the real repo's .claude/queue second.
# Getting this wrong is not cosmetic - before this existed, every beat write
# went straight to the real .claude/queue/.hookbeats regardless of the test
# suite's sandboxed queue dir, so running scripts/test-hooks.ps1 silently
# refreshed the real liveness beats with test-invocation timestamps. That
# would have made Test-HookLiveness below unable to ever see a truly dead
# hook, since the test suite's own runs kept "healing" it.
function Get-HookBeatsDir {
    param([string] $RepoRoot)
    if ($env:OPAL_AUTOPILOT_QUEUE_DIR) { return (Join-Path $env:OPAL_AUTOPILOT_QUEUE_DIR ".hookbeats") }
    if (-not $RepoRoot) { $RepoRoot = Split-Path -Parent (Split-Path -Parent $PSScriptRoot) }
    return (Join-Path $RepoRoot ".claude/queue/.hookbeats")
}

function Write-HookBeat {
    param(
        [Parameter(Mandatory = $true)][string] $Name,
        [string] $RepoRoot
    )
    try {
        $dir = Get-HookBeatsDir -RepoRoot $RepoRoot
        if (-not (Test-Path $dir)) { New-Item -ItemType Directory -Path $dir -Force | Out-Null }
        # One file per hook, so two hooks firing at once never contend. Last
        # write wins by design: only the most recent beat is ever read.
        [pscustomobject]@{
            hook = $Name
            at   = (Get-Date).ToString('o')
            pid  = $PID
        } | ConvertTo-Json -Compress | Set-Content (Join-Path $dir "$Name.json") -Encoding utf8 -ErrorAction Stop
    } catch {
        # Deliberately silent. See the fail-open note above.
    }
}

# Reads every beat back. Returns a hashtable name -> [datetime], skipping
# anything unreadable rather than throwing: a corrupt beat file must degrade to
# "this hook has not been seen", which is the same conclusion as a missing one
# and is the safe direction (it reports a possible problem instead of hiding
# one).
function Get-HookBeats {
    param([string] $RepoRoot)
    $beats = @{}
    try {
        $dir = Get-HookBeatsDir -RepoRoot $RepoRoot
        if (-not (Test-Path $dir)) { return $beats }
        foreach ($f in Get-ChildItem $dir -Filter '*.json' -ErrorAction Stop) {
            try {
                $j = Get-Content $f.FullName -Raw -ErrorAction Stop | ConvertFrom-Json -ErrorAction Stop
                if ($j.hook -and $j.at) { $beats[[string]$j.hook] = [datetime]::Parse($j.at) }
            } catch { }
        }
    } catch { }
    return $beats
}

# Hooks expected to fire on (almost) every turn: autopilot-gate and
# noticed-gate are unconditional Stop hooks, budget-guard is an unrestricted
# PreToolUse hook. A commit can only land via a tool call inside a turn that
# then hit Stop, so if one of these three has gone quiet since before the
# newest commit, it did not fire during the turn that produced that commit -
# which cannot happen while it is still wired correctly.
#
# session-start-autopilot (fires only on a brand-new session) and
# turn-failure-checkpoint / pre-push-gate (fire only on specific rare events -
# a killed turn, a push) are deliberately excluded: long gaps between their
# beats are the normal case, not a failure, and flagging them would be exactly
# the kind of check nobody ends up trusting (see docs/work-quality.md).
$HighFrequencyHookNames = @('autopilot-gate', 'noticed-gate', 'budget-guard')

function Test-HookLiveness {
    <#
    .SYNOPSIS
      Flags high-frequency hooks whose last beat predates the newest commit.

    .OUTPUTS
      Array of human-readable strings, one per hook that looks dead. Empty if
      none do - including when there are no beats and no commits to compare
      against (a fresh checkout has nothing to be suspicious of yet).
    #>
    param(
        [string] $RepoRoot,
        [Parameter(Mandatory = $true)][datetime] $LatestCommitAt
    )
    $dead = @()
    $beats = Get-HookBeats -RepoRoot $RepoRoot
    foreach ($name in $HighFrequencyHookNames) {
        if (-not $beats.ContainsKey($name)) {
            $dead += "$name has never recorded a beat, but commits already exist"
            continue
        }
        if ($beats[$name] -lt $LatestCommitAt) {
            $dead += "$name last beat $($beats[$name].ToString('u')), older than the newest commit ($($LatestCommitAt.ToString('u')))"
        }
    }
    return $dead
}
