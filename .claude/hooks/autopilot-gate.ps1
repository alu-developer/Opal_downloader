# Stop hook: keeps an autonomous work session going instead of ending the turn
# and waiting for the user to type "weiter".
#
# Only engages when .claude/queue/AUTOPILOT exists (see docs/agent-operating-model.md).
# Without that marker this hook is a no-op, so ordinary conversations - asking a
# question, getting an answer, stopping - behave exactly as before.
#
# FAIL-OPEN BY DESIGN: any error, missing file, unreadable JSON or unexpected
# state exits 0 (= allow the stop). Trapping the user in a loop is far worse
# than occasionally stopping early, so every uncertain path ends the turn.

$ErrorActionPreference = 'SilentlyContinue'

function Allow-Stop { exit 0 }

# --- read hook input (session id lets us count iterations per session) --------
$sessionId = "unknown"
try {
    $raw = [Console]::In.ReadToEnd()
    if ($raw) {
        $payload = $raw | ConvertFrom-Json -ErrorAction Stop
        if ($payload.session_id) { $sessionId = [string]$payload.session_id }
    }
} catch { }

# Overridable so the hook can be exercised against a throwaway directory.
# This is not a convenience: on 2026-07-21 a verification run ended with
# `rm -f .claude/queue/AUTOPILOT .autopilot-state.json .autopilot-session.json`
# to clean up its own artefacts, and killed the REAL autopilot run - deleting
# the session record too, which is exactly what defeats the restore-on-delete
# protection. Tests must set OPAL_AUTOPILOT_QUEUE_DIR and never touch the real
# queue directory.
$queueDir = $env:OPAL_AUTOPILOT_QUEUE_DIR
if (-not $queueDir) { $queueDir = Join-Path $PSScriptRoot "..\queue" }
$marker = Join-Path $queueDir "AUTOPILOT"
$sessionRecord = Join-Path $queueDir ".autopilot-session.json"
$offSwitch = Join-Path $queueDir "AUTOPILOT.OFF"
$statePath = Join-Path $queueDir ".autopilot-state.json"
$endRecord = Join-Path $queueDir ".autopilot-ended.json"

# Autopilot ending is recorded, always, with the reason. Until 2026-07-27 it
# was not, and the consequence was a silent failure the maintainer had to
# notice by hand: they asked "warum muss ich dich schon wieder anschreiben?"
# after a whole session in which this gate never blocked once. A watchdog whose
# death is silent is worse than none, because it looks like a working one - the
# same lesson resume-runner.ps1's launch-failed reporting already learned.
function End-Autopilot([string]$reason) {
    try {
        @{ reason = $reason; at = [DateTimeOffset]::UtcNow.ToUnixTimeSeconds() } |
            ConvertTo-Json -Compress | Set-Content $endRecord -Encoding utf8 -ErrorAction Stop
    } catch { }
    Remove-Item $marker, $sessionRecord -Force -ErrorAction SilentlyContinue
    exit 0
}

# The maintainer's off switch, and the ONLY early exit. Honoured first and
# unconditionally.
if (Test-Path $offSwitch) {
    End-Autopilot "the maintainer created AUTOPILOT.OFF"
}

# Effective config: the marker if present, otherwise a live session record.
#
# The fallback is deliberate. On 2026-07-21 the assistant ended three
# autonomous runs early by deleting the marker itself and justifying it
# afterwards - the exact behaviour autopilot exists to prevent, and the old
# version of this hook literally told it how ("to end autopilot, delete
# ..."). Ending the run is not the assistant's call to make: the guards
# below decide, or the maintainer does via AUTOPILOT.OFF. So a deleted
# marker no longer ends anything while the session record is still live; it
# is simply restored.
$cfg = $null
if (Test-Path $marker) {
    try { $cfg = Get-Content $marker -Raw -ErrorAction Stop | ConvertFrom-Json -ErrorAction Stop } catch { $cfg = $null }
}
if ($null -eq $cfg -and (Test-Path $sessionRecord)) {
    try { $cfg = Get-Content $sessionRecord -Raw -ErrorAction Stop | ConvertFrom-Json -ErrorAction Stop } catch { $cfg = $null }
}
if ($null -eq $cfg) {
    # Nothing to run on. Two very different situations, and telling them apart
    # is the whole point: autopilot was never armed here (ordinary
    # conversation - stay a no-op), or it was armed, vanished, and nobody was
    # told. The state file is the evidence: it only exists once this gate has
    # actually counted an iteration in this repo.
    #
    # Reported exactly once - writing the end record before blocking means a
    # second stop sails through, so a confused state can never trap the user in
    # a loop. Fail-open still wins over being right.
    if ((Test-Path $statePath) -and -not (Test-Path $endRecord)) {
        try {
            @{ reason = "vanished: no marker, no session record, no recorded ending"; at = [DateTimeOffset]::UtcNow.ToUnixTimeSeconds() } |
                ConvertTo-Json -Compress | Set-Content $endRecord -Encoding utf8 -ErrorAction Stop
        } catch { Allow-Stop }
        $msg = "AUTOPILOT STOPPED WITHOUT A REASON BEING RECORDED. .claude/queue/AUTOPILOT and .autopilot-session.json are both gone, but nothing recorded why - so this gate has been allowing every stop silently, which is why the maintainer has had to prompt you to continue.`n`nDo not just carry on: say this happened, in your reply. Then re-arm by writing {`"expires_at`": <unix seconds, now + 4h>, `"max_iterations`": 20} to .claude/queue/AUTOPILOT, and check whether docs/BACKLOG.md's `"Now`" items are all marked **Blocked:** - an all-blocked backlog makes this gate correctly conclude there is no work, which looks identical to autopilot being broken.`n`nThis message is shown once and never again."
        Write-Output (@{ decision = "block"; reason = $msg } | ConvertTo-Json -Depth 5 -Compress)
        exit 0
    }
    Allow-Stop
}

$now = [DateTimeOffset]::UtcNow.ToUnixTimeSeconds()

# --- hard expiry: autopilot is always time-boxed ------------------------------
if ($null -eq $cfg.expires_at) { Allow-Stop }
if ($now -ge [int64]$cfg.expires_at) {
    End-Autopilot "expired at $($cfg.expires_at)"
}

$maxIterations = 20
if ($null -ne $cfg.max_iterations) { $maxIterations = [int]$cfg.max_iterations }

# --- iteration cap, per session ----------------------------------------------
$state = $null
if (Test-Path $statePath) {
    try { $state = Get-Content $statePath -Raw -ErrorAction Stop | ConvertFrom-Json -ErrorAction Stop } catch { $state = $null }
}
if ($null -eq $state) { $state = [pscustomobject]@{} }

$count = 0
if ($state.PSObject.Properties.Name -contains $sessionId) { $count = [int]$state.$sessionId }
if ($count -ge $maxIterations) { Allow-Stop }

# --- rate-limit budget --------------------------------------------------------
# Keep the real status file fresh first. The status line is its only writer and
# does not run in non-interactive or claude-desktop sessions, so without this
# the file is routinely hours old - it was found 18h stale reporting "1%" while
# the account sat at 46%. See rate-limit-keepwarm.ps1.
#
# A transcript-derived estimate was tried here and REMOVED on 2026-07-21: it
# put the 5h window at 83.5% when the real figure was 46%, i.e. it would have
# stopped autonomous work for no reason. A miscalibrated budget signal that
# gates work is worse than no signal.
$keepwarm = Join-Path $PSScriptRoot "rate-limit-keepwarm.ps1"
if (Test-Path $keepwarm) {
    # & on the script path, never a child powershell.exe: a child inherits
    # this hook's stdin and blocks forever trying to read it.
    #
    # -NoWait because a cold launch's 42s confirmation wait exceeds this hook's
    # own timeout, which used to kill the gate mid-wait and silently end
    # autopilot. This turn uses the previous floor reading; the next gets the
    # fresh one.
    & $keepwarm -NoWait | Out-Null
}

# Per-window staleness handling now lives in budget-lib.ps1, shared with the
# PreToolUse guard, because two copies of this rule had already drifted apart
# once (the old rate-limit-gate.ps1 had no freshness check at all).
$rateKnown = $false
$five = $null
$seven = $null
try {
    $lib = Join-Path $PSScriptRoot "budget-lib.ps1"
    if (Test-Path $lib) {
        . $lib
        $budget = Get-BudgetFloor -Now $now
        $rateKnown = $budget.Known
        $five = $budget.FiveHour
        $seven = $budget.SevenDay

        if (($null -ne $five) -and ($five -ge 75)) { Allow-Stop }
        if (($null -ne $seven) -and ($seven -ge 80)) { Allow-Stop }
    }
} catch { }

if (-not $rateKnown -and $count -ge 8) {
    # No usable budget reading at all: allow a short autonomous stretch, not a
    # long one.
    Allow-Stop
}

# --- already waiting on a background job? ------------------------------------
# Blocking the stop here would waste a whole turn: when a backgrounded command
# finishes, the harness re-invokes the assistant on its own. So if a long job
# belonging to this repo is still running, let the turn end and let the
# completion notification do the waking.
# Matching on the repo path does not work: `go test ./internal/scraper/` is
# spawned with a RELATIVE package path, so the repo root never appears in the
# command line. Match the shapes of long job this repo actually starts
# instead. A false positive only ends the turn early (autopilot resumes on the
# next user message), which is the harmless direction.
#
# OPAL_AUTOPILOT_SKIP_BUSY_CHECK exists because this gate reads the machine's
# whole process table, which no test controls. Three assertions in
# scripts/test-hooks.ps1 depend on reaching the backlog logic below, and on
# 2026-07-29 they failed once and then passed four runs in a row - something
# with the repo path on its command line happened to be alive for one run. A
# suite that fails on a coin flip trains you to re-run it instead of reading
# it, which is how the last silent breakage survived a week. Tests that are
# not about this check switch it off; the two that ARE about it (same file)
# leave it on and start a real process to trip it.
$repoRoot = (Resolve-Path (Join-Path $PSScriptRoot "..\..")).Path
if ($env:OPAL_AUTOPILOT_SKIP_BUSY_CHECK -ne '1') {
    try {
        $procs = Get-CimInstance Win32_Process -ErrorAction Stop
        $busy = $procs | Where-Object {
            ($_.Name -eq 'go.exe' -and $_.CommandLine -and $_.CommandLine -match '\s(test|run)\s') -or
            ($_.Name -like '*.test.exe') -or
            ($_.Name -in @('opal-dl.exe', 'opal-downloader.exe')) -or
            ($_.CommandLine -and $_.CommandLine -like "*$repoRoot*" -and $_.Name -ne 'powershell.exe')
        }
        if ($busy) { Allow-Stop }
    } catch { }
}

# --- is there actually work left? --------------------------------------------
# Reads docs/BACKLOG.md, which replaced .claude/queue/todo/ on 2026-07-22.
# This used to count *.md files in the queue's todo directory; when the
# backlog migration emptied that directory, the gate would have silently
# started allowing every stop - autopilot dead with nothing reporting it.
# Exactly the half-migration this whole cleanup was about, so the counter
# moved with the work it counts.
#
# An item is a "### " heading appearing before the "## Done recently"
# section, MINUS any flagged "**Blocked:**" (Get-BacklogItems, budget-lib.ps1)
# - a heading needing a maintainer decision or a live/attended session is not
# something continuing the turn can make progress on, and counting it as work
# forced endless nagging with nowhere to go. Everything from "Done recently"
# onward is history, not work.
# Overridable for the same reason OPAL_AUTOPILOT_QUEUE_DIR is: the Noticed
# fallback below cannot be exercised end to end against the real file, since
# whether it fires depends on the repo's own backlog happening to be
# all-blocked. Testing only the parser would leave the wiring unchecked, which
# is how the stall watchdog shipped connected to nothing.
$backlog = $env:OPAL_AUTOPILOT_BACKLOG
if (-not $backlog) { $backlog = Join-Path $repoRoot "docs\BACKLOG.md" }
if (-not (Test-Path $backlog)) { Allow-Stop }
try {
    $todo = @(Get-BacklogItems -BacklogPath $backlog | Where-Object { -not $_.Blocked } | ForEach-Object { $_.Title })
} catch { Allow-Stop }
# Nothing actionable under "Now"? Fall back to the Noticed section before
# giving up. It is the list a Stop hook fills with one thing seen and not done
# per session, and until 2026-07-27 nothing ever consumed it - so an
# all-blocked "Now" made this gate conclude there was no work while five real
# entries sat in the same file. That is exactly the state the maintainer hit
# when they had to prompt for every continuation.
#
# Second-class on purpose: the Noticed section says in its own words that its
# entries are "not commitments". They are only reached when nothing under "Now"
# is actionable, and the reason text below says which list the work came from,
# so a Noticed item can never be mistaken for a committed one.
$fromNoticed = $false
if ($todo.Count -eq 0) {
    try { $todo = @(Get-NoticedItems -BacklogPath $backlog) } catch { $todo = @() }
    $fromNoticed = $todo.Count -gt 0
}
if ($todo.Count -eq 0) { Allow-Stop }

# --- continue ----------------------------------------------------------------
# Persist the config so a later self-deletion of the marker cannot end the
# run, and restore the marker if it has already gone missing.
try { $cfg | ConvertTo-Json -Depth 5 | Set-Content $sessionRecord -Encoding utf8 -ErrorAction Stop } catch { }
if (-not (Test-Path $marker)) {
    try { $cfg | ConvertTo-Json -Depth 5 | Set-Content $marker -Encoding utf8 -ErrorAction Stop } catch { }
}

$state | Add-Member -NotePropertyName $sessionId -NotePropertyValue ($count + 1) -Force
try { $state | ConvertTo-Json -Depth 5 | Set-Content $statePath -Encoding utf8 -ErrorAction Stop } catch { Allow-Stop }

$names = ($todo | Select-Object -First 5) -join "; "
$listLabel = "item(s) remain in docs/BACKLOG.md"
if ($fromNoticed) {
    $listLabel = "item(s) remain, and every `"Now`" item is blocked - so these come from BACKLOG's `"Noticed`" section, which is a list of rough edges rather than commitments. Pick one worth doing, do it, and delete its entry; if none is worth doing, delete the ones that no longer matter and say so"
}
$budgetNote = "budget unknown (no usable rate-limit reading)"
if ($rateKnown) { $budgetNote = "budget ok (5h $five%, 7d $seven%)" }

$reason = @"
AUTOPILOT is on ($($count + 1)/$maxIterations this session, $budgetNote), and $($todo.Count) $($listLabel): $names

Do not stop to ask whether to continue. Pick the highest-value remaining task yourself and work it end to end: implement, run scripts/dev.ps1 all, verify against the task's own acceptance criteria, then commit and push. CI runs on every push to master, and the pre-push gate runs the full local suite first. A PR is fine when the change genuinely wants review, but it is not required - the last 25 commits went straight to master, and demanding one for a doc fix is the ceremony the queue was retired for.

Keep docs/RESUME.md pointing at what you are actually doing right now, and commit each piece as it becomes correct. A turn killed by the usage limit never reaches this hook, so anything not written down by then is gone.

Rules that still apply: never report a criterion as verified without exercising it; label anything unverified explicitly; a negative result is a valid outcome to report and file. Stop early only if a task genuinely needs a human decision - if so, mark that item **Blocked:** in docs/BACKLOG.md with the open question written down, then continue with the next task.

Ending this run is NOT your call. Deleting .claude/queue/AUTOPILOT does not work - the hook restores it. The guards above end the run (expiry, iteration cap, rate limits, or no unblocked item left in docs/BACKLOG.md), or the maintainer does by creating .claude/queue/AUTOPILOT.OFF. If you believe it should stop early, say so in your reply and keep working; do not act on it yourself.

"Budget", "this session is long", and "the next task deserves a fresh start" are not stop conditions. They are the rationalisations used to end three earlier runs.
"@

$out = @{ decision = "block"; reason = $reason } | ConvertTo-Json -Depth 5 -Compress
Write-Output $out
exit 0
