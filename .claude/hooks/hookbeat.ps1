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

function Write-HookBeat {
    param(
        [Parameter(Mandatory = $true)][string] $Name,
        [string] $RepoRoot
    )
    try {
        if (-not $RepoRoot) { $RepoRoot = Split-Path -Parent (Split-Path -Parent $PSScriptRoot) }
        $dir = Join-Path $RepoRoot ".claude/queue/.hookbeats"
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
        if (-not $RepoRoot) { $RepoRoot = Split-Path -Parent (Split-Path -Parent $PSScriptRoot) }
        $dir = Join-Path $RepoRoot ".claude/queue/.hookbeats"
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
