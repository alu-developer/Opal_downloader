# Restarts work by itself once the usage budget has recovered.
#
# WHY (maintainer's request, 2026-07-23): they asked to be restarted
# automatically at a suitable time, so that recovering from a usage limit does
# not mean opening a fresh session by hand. The mid-turn budget guard and the
# StopFailure checkpoint made a usage-limit kill CHEAP; they did not make work
# RESUME. That still needed a human. This closes it.
#
# THE DESIGN CONSTRAINT THAT SHAPES EVERYTHING HERE
# -------------------------------------------------
# A resume mechanism that costs tokens to decide "nothing to do" becomes its
# own budget problem - it would be firing precisely when budget is the scarce
# thing. The previous design (a recurring CronCreate job) could not avoid that:
# every fire is a model turn, even one that immediately concludes there is
# nothing to do.
#
# So every gate below runs in PowerShell and costs NOTHING. A `claude` process
# is started only when the budget is genuinely healthy AND there is genuinely
# work AND nothing is already running. A quiet hour costs zero tokens.
#
# BOUNDED BY CONSTRUCTION
# -----------------------
# Unattended runs must not be able to spend without limit:
#   - OPAL_UNATTENDED_RESUME=1 makes session-start-autopilot.ps1 arm a small
#     iteration cap instead of the usual 20.
#   - --model sonnet, per the documented default in
#     docs/agent-operating-model.md 2. Opus is a deliberate escalation the
#     maintainer makes in person; an unattended run does not get to make it.
#   - A cooldown means a failing run cannot re-launch in a tight loop.
#
# OFF SWITCH: .claude/queue/AUTOPILOT.OFF disables this exactly as it disables
# autopilot. One switch, not two.
#
# Exits 0 always. Prints one line saying what it decided, which is what lands in
# the Task Scheduler log.

param(
    # Report the decision without launching anything.
    [switch]$DryRun,
    # Skip the budget and cooldown gates. For testing the launch path only.
    [switch]$Force,
    # Print the resolved `claude` launcher and exit. Exists so the test suite
    # can assert against THIS resolution rather than reimplementing it - see
    # the comment on Resolve-ClaudeLauncher for why that distinction matters.
    [switch]$WhichClaude,
    # Stop the unattended run this script started, agent included.
    [switch]$Stop
)

$ErrorActionPreference = 'SilentlyContinue'
Set-StrictMode -Off

function Resolve-ClaudeLauncher {
    # WHY THIS EXISTS AT ALL, i.e. why not just -FilePath "claude":
    #
    # `Start-Process` does not resolve a bare name the way the PowerShell prompt
    # does. The prompt walks PATHEXT and finds claude.cmd; Start-Process hands
    # the raw string to the Windows loader, which takes the FIRST matching file
    # on PATH regardless of extension. npm installs three side by side:
    #
    #     claude       <- extensionless POSIX shim, for Git Bash. Not a PE binary.
    #     claude.cmd   <- the Windows entry point
    #     claude.ps1
    #
    # So the loader picked the shim and failed with "%1 is not a valid Win32
    # application" - every single time, for two days, while the gates below
    # reported healthy and the log said "launch-failed" to nobody. The feature
    # was never once able to do the one thing it exists for.
    if ($env:OPAL_RESUME_CLAUDE_CMD) { return $env:OPAL_RESUME_CLAUDE_CMD }
    foreach ($c in @(Get-Command claude -All -ErrorAction SilentlyContinue)) {
        if ($c.CommandType -eq 'Application' -and $c.Source -match '\.(cmd|bat|exe)$') { return $c.Source }
    }
    return $null
}

if ($WhichClaude) { Write-Output (Resolve-ClaudeLauncher); exit 0 }

function Say { param([string]$Decision, [string]$Detail = "")
    $stamp = (Get-Date).ToUniversalTime().ToString("yyyy-MM-ddTHH:mm:ssZ")
    $line = "$stamp`t$Decision"
    if ($Detail) { $line += "`t$Detail" }
    Write-Output $line
    try { Add-Content -Path $script:logPath -Value $line -Encoding utf8 } catch { }
}

# OPAL_RESUME_REPO_ROOT lets the tests point the work-detection gates at a
# synthetic docs/ tree. Without it they would read the real RESUME.md and
# BACKLOG.md, so "is there work?" assertions would pass or fail depending on
# what happens to be in flight that day.
$repoRoot = $env:OPAL_RESUME_REPO_ROOT
if (-not $repoRoot) { $repoRoot = (Resolve-Path (Join-Path $PSScriptRoot "..\..")).Path }
$queueDir = $env:OPAL_AUTOPILOT_QUEUE_DIR
if (-not $queueDir) { $queueDir = Join-Path $repoRoot ".claude\queue" }
if (-not (Test-Path $queueDir)) {
    try { New-Item -ItemType Directory -Path $queueDir -Force | Out-Null } catch { exit 0 }
}

$script:logPath = Join-Path $queueDir "resume-runner.log"
$statePath = Join-Path $queueDir ".resume-runner-state.json"
$offSwitch = Join-Path $queueDir "AUTOPILOT.OFF"

$now = [DateTimeOffset]::UtcNow.ToUnixTimeSeconds()

# --- gate 1: the maintainer's off switch --------------------------------------
if (Test-Path $offSwitch) { Say "skip" "AUTOPILOT.OFF present"; exit 0 }

# --- housekeeping: keep the per-launch files bounded --------------------------
# Every launch leaves a resume-run-<stamp>.log, a .log.err (usually empty) and a
# resume-prompt-<stamp>.txt. 42 of them had accumulated with no expiry. Same
# shape and the same two rails as the checkpoint-ref prune in
# turn-failure-checkpoint.ps1: an age cutoff, plus a floor so a quiet fortnight
# cannot leave zero diagnostics behind, and a strict name match so nothing
# without a timestamp in it is ever a candidate.
#
# resume-runner.log itself is deliberately NOT in scope: it is the append-only
# decision log and the only continuous record of what this runner decided.
$keepDays = 14
$keepAtLeast = 10
if ($env:OPAL_RESUME_KEEP_DAYS) { $keepDays = [int]$env:OPAL_RESUME_KEEP_DAYS }
if ($env:OPAL_RESUME_KEEP_AT_LEAST) { $keepAtLeast = [int]$env:OPAL_RESUME_KEEP_AT_LEAST }
try {
    $cutoff = $now - ([long]$keepDays * 86400)
    $stamped = @(Get-ChildItem -Path $queueDir -File -ErrorAction SilentlyContinue |
        ForEach-Object {
            if ($_.Name -match '^resume-(?:run|prompt)-(\d+)\.(?:log|log\.err|txt)$') {
                [pscustomobject]@{ File = $_.FullName; Stamp = [long]$Matches[1] }
            }
        } | Where-Object { $_ })
    # Group by launch so the floor counts launches, not files - three files per
    # launch would otherwise make "keep 10" mean "keep 3 runs".
    $launches = @($stamped | Select-Object -ExpandProperty Stamp -Unique | Sort-Object -Descending)
    if ($launches.Count -gt $keepAtLeast) {
        $doomedStamps = @($launches | Select-Object -Skip $keepAtLeast | Where-Object { $_ -lt $cutoff })
        $removed = 0
        foreach ($s in $doomedStamps) {
            foreach ($f in @($stamped | Where-Object { $_.Stamp -eq $s })) {
                try { Remove-Item $f.File -Force -ErrorAction Stop; $removed++ } catch { }
            }
        }
        if ($removed -gt 0) { Say "pruned" "$removed per-launch file(s) from $($doomedStamps.Count) run(s) older than ${keepDays}d" }
    }
} catch { }

# --- load state ---------------------------------------------------------------
$state = $null
if (Test-Path $statePath) {
    try { $state = Get-Content $statePath -Raw -ErrorAction Stop | ConvertFrom-Json -ErrorAction Stop } catch { $state = $null }
}
if ($null -eq $state) { $state = [pscustomobject]@{ last_launch = 0; last_pid = 0 } }

# --- stopping a run, correctly ------------------------------------------------
# The recorded pid is the cmd.exe wrapper (claude.cmd), NOT the agent. On
# 2026-07-26 a run was stopped by killing that pid alone, and the claude.exe
# underneath survived as an orphan and went on editing the worktree for another
# five minutes - its changes landed in someone else's commit. Kill the tree.
if ($Stop) {
    if (-not ($state.last_pid -and [int]$state.last_pid -gt 0)) { Say "stop" "no run recorded"; exit 0 }
    $target = [int]$state.last_pid
    if (-not (Get-Process -Id $target -ErrorAction SilentlyContinue)) { Say "stop" "recorded run (pid $target) is not running"; exit 0 }
    & taskkill.exe /PID $target /T /F 2>&1 | Out-Null
    Start-Sleep -Milliseconds 500
    $still = [bool](Get-Process -Id $target -ErrorAction SilentlyContinue)
    if ($still) { Say "stop-failed" "pid $target survived taskkill /T /F" }
    else { Say "stop" "killed pid $target and its descendants" }
    exit 0
}

# --- gate 2: is a previous unattended run still going? ------------------------
# Launching a second one would have two agents editing the same worktree.
if ($state.last_pid -and [int]$state.last_pid -gt 0) {
    $p = Get-Process -Id ([int]$state.last_pid) -ErrorAction SilentlyContinue
    if ($p -and $p.ProcessName -match 'claude|powershell') {
        Say "skip" "previous resume run still active (pid $($state.last_pid))"
        exit 0
    }
}

# --- gate 2b: is a human's session working in this tree right now? ------------
# Gate 2 only knows about runs THIS script started. It says nothing about the
# maintainer having a session open - and on 2026-07-26, the first hour the
# launch path actually worked, this fired into a worktree an interactive session
# was already editing. Two agents, one tree, no lock between them.
#
# budget-guard.ps1 stamps a heartbeat on every tool call, so "working" means a
# recent stamp. Deliberately NOT "is any claude process alive": the keep-warm
# process is permanently alive and idle, so that test would never let this
# launch again - the same shape as the deadlock in gate 4.
#
# Fails toward launching, not toward silence: a session that dies leaves a stamp
# that ages out within the window, so this can never wedge shut permanently. An
# idle open session is the accepted false negative - it is not editing anything.
if (-not $Force) {
    $heartbeat = Join-Path $queueDir ".session-heartbeat.json"
    if (Test-Path $heartbeat) {
        try {
            $hb = Get-Content $heartbeat -Raw -ErrorAction Stop | ConvertFrom-Json -ErrorAction Stop
            $age = $now - [int64]$hb.at
            if ($age -ge 0 -and $age -lt 1200) {
                Say "skip" "a session is active in this tree ($([math]::Round($age / 60))m since its last tool call)"
                exit 0
            }
        } catch { }
    }
}

# --- gate 2c: is someone mid-edit in this tree? -------------------------------
# Gate 2b only sees sessions that write the heartbeat, and only sessions OPENED
# in this repo do - the project settings that install that hook are fixed to the
# directory the session started in. A session started elsewhere and pointed here
# by path is invisible to it.
#
# That is not theoretical. On 2026-07-29, while a session opened in the home
# directory was editing these very hooks, the hourly tick fired, saw no
# heartbeat, and launched an unattended agent into the same worktree. It made
# two commits alongside a human's uncommitted edits. Nothing detected it; the
# collision was noticed by hand.
#
# A recently-touched dirty tree is the signal the heartbeat misses, because it
# does not depend on the other session having any hooks at all.
#
# THE AGE WINDOW IS THE WHOLE DESIGN. "Dirty" alone would wedge this shut
# forever the first time an unattended run died leaving half an edit behind -
# the permanent-silence failure this file's header exists to prevent. Stale
# changes age out of the window within the hour and stop blocking anything;
# only active editing does.
if (-not $Force) {
    try {
        $porcelain = @(& git -C $repoRoot status --porcelain 2>$null)
        if ($LASTEXITCODE -eq 0 -and $porcelain.Count -gt 0) {
            $newest = 0
            foreach ($line in $porcelain) {
                if ($line.Length -lt 4) { continue }
                $p = $line.Substring(3)
                # Renames arrive as "old -> new"; the new path is the one on disk.
                if ($p -match ' -> ') { $p = ($p -split ' -> ')[-1] }
                $p = $p.Trim('"')
                $full = Join-Path $repoRoot $p
                try {
                    $t = (Get-Item -LiteralPath $full -Force -ErrorAction Stop).LastWriteTimeUtc
                    $secs = [DateTimeOffset]::new($t, [TimeSpan]::Zero).ToUnixTimeSeconds()
                    if ($secs -gt $newest) { $newest = $secs }
                } catch { }   # deleted files have no mtime, and that is fine
            }
            $editAge = $now - $newest
            if ($newest -gt 0 -and $editAge -ge 0 -and $editAge -lt 1800) {
                Say "skip" "the worktree was edited $([math]::Round($editAge / 60))m ago - something is working here that gate 2b cannot see"
                exit 0
            }
        }
    } catch { }   # not a git repo, no git on PATH: fail toward launching
}

# --- gate 3: cooldown ---------------------------------------------------------
# A run that dies immediately must not turn into a relaunch loop that drains
# the budget faster than working would have.
$cooldownSeconds = 7200
if (-not $Force) {
    $since = $now - [int64]$state.last_launch
    if ([int64]$state.last_launch -gt 0 -and $since -lt $cooldownSeconds) {
        $mins = [math]::Round(($cooldownSeconds - $since) / 60)
        Say "skip" "cooldown, ${mins}m remaining"
        exit 0
    }
}

# --- gate 3b: mains power ------------------------------------------------------
# An unattended run on battery is a run that will be frozen by Modern Standby
# within minutes and lose everything it had not committed - see
# unattended-run.ps1's header for the 2026-07-28 post-mortem. The wake lock this
# runner now holds keeps the machine up, which is exactly what must NOT happen
# to a laptop running on its battery in a bag. So: mains only. A missed hour on
# battery costs nothing; the next tick after it is plugged in picks the work up.
#
# The override exists so the test suite can exercise both branches on whatever
# power state the developer's machine happens to be in - the same reason
# OPAL_RESUME_CLAUDE_CMD exists.
function Test-OnACPower {
    switch ($env:OPAL_RESUME_POWER_OVERRIDE) {
        'ac'      { return $true }
        'battery' { return $false }
    }
    try {
        Add-Type -AssemblyName System.Windows.Forms -ErrorAction Stop
        $status = [System.Windows.Forms.SystemInformation]::PowerStatus.PowerLineStatus
        # Unknown means no battery was found, i.e. a desktop. Treat that as
        # mains: refusing to run on every desktop would be a worse failure than
        # the one this gate prevents.
        return ($status -ne [System.Windows.Forms.PowerLineStatus]::Offline)
    } catch { return $true }
}
if (-not $Force) {
    if (-not (Test-OnACPower)) { Say "skip" "on battery - an unattended run would be frozen by standby, and holding the machine awake on battery is worse"; exit 0 }
}

# --- gate 4: budget -----------------------------------------------------------
# The whole point: only resume once the budget has actually recovered. Gates on
# the WORST window, so a healthy 5h window cannot mask an exhausted 7d one -
# which is the exact shape of the situation this was built in (5h reset to ~0%
# while 7d sat at 86%).
if (-not $Force) {
    $lib = Join-Path $PSScriptRoot "budget-lib.ps1"
    if (-not (Test-Path $lib)) { Say "skip" "budget-lib.ps1 missing"; exit 0 }
    . $lib
    $budget = Get-BudgetFloor

    # THE DEADLOCK THIS AVOIDS, because it is not obvious and nearly shipped:
    #
    # rate-limit-status.json is written by the status line, which only runs
    # inside a live `claude` session. This runner exists precisely for when
    # there is no live session - so nothing is refreshing the file while it
    # waits. It sits frozen at the numbers from the moment the last session
    # died.
    #
    # Frozen numbers are fine while a window is still running: usage only
    # climbs, so the old figure is a valid floor and "still too high" stays
    # true. But once BOTH windows' resets_at have passed, every reading
    # becomes unusable (an expired window describes a window that no longer
    # exists), Get-BudgetFloor reports Known=false - and the moment that
    # happens is exactly the moment the quota came back.
    #
    # Refusing to guess there would have meant refusing forever: it needs
    # fresh numbers to decide it may start a session, but only a session
    # produces fresh numbers. The hourly task would have logged "refusing to
    # guess" until someone noticed, which is the silent-uselessness this whole
    # hook family was written to stamp out.
    #
    # So an unusable reading is not a reason to give up, it is the one moment
    # worth spending a keep-warm launch on. -Force because the reuse path
    # would hand back the same stale process: an idle `claude` re-stamps its
    # cached figures without ever learning new ones.
    if (-not $budget.Known) {
        $keepwarm = $env:OPAL_KEEPWARM_CMD
        if (-not $keepwarm) { $keepwarm = Join-Path $PSScriptRoot "rate-limit-keepwarm.ps1" }
        if (Test-Path $keepwarm) {
            Say "refreshing" "no usable reading (likely a window just rolled over) - forcing a keep-warm sync"
            & $keepwarm -Force | Out-Null
            $budget = Get-BudgetFloor
        }
    }

    if (-not $budget.Known) { Say "skip" "no usable budget reading even after a refresh - refusing to guess"; exit 0 }
    $rung = Get-BudgetRung $budget
    if ($rung -ge 2) { Say "skip" "budget not recovered ($($budget.Reason))"; exit 0 }
    $budgetNote = $budget.Reason
} else {
    $budgetNote = "forced"
}

# --- gate 5: is there actually work? ------------------------------------------
$hasWork = $false
$resumePath = Join-Path $repoRoot "docs\RESUME.md"
if (Test-Path $resumePath) {
    # -Encoding UTF8: RESUME.md has no BOM, so without this Windows
    # PowerShell 5.1 would read it via the system ANSI codepage instead.
    $resume = (Get-Content $resumePath -Raw -Encoding UTF8 -ErrorAction SilentlyContinue)
    if ($resume -and $resume -notmatch '(?m)^_Nothing in flight\.') { $hasWork = $true }
}
if (-not $hasWork) {
    $backlog = Join-Path $repoRoot "docs\BACKLOG.md"
    if (Test-Path $backlog) {
        # Get-BacklogItems (budget-lib.ps1) excludes items flagged
        # "**Blocked:**" - a heading waiting on a maintainer decision or a
        # live/attended session is not something an unattended relaunch can
        # make progress on, so it must not count as a reason to spend one.
        # Dot-sourced here rather than relying on gate 4's copy: -Force skips
        # gate 4 entirely, which would leave the function undefined.
        $lib = Join-Path $PSScriptRoot "budget-lib.ps1"
        if (Test-Path $lib) {
            . $lib
            try {
                if (@(Get-BacklogItems -BacklogPath $backlog | Where-Object { -not $_.Blocked }).Count -gt 0) {
                    $hasWork = $true
                }
            } catch { }
        }
    }
}
if (-not $hasWork) { Say "skip" "nothing in RESUME.md or BACKLOG.md"; exit 0 }

# --- launch -------------------------------------------------------------------
$prompt = @"
You are resuming unattended, started by the scheduled resume runner because the token budget recovered after an earlier run was cut short.

Read docs/RESUME.md first, then docs/BACKLOG.md, and continue that work. Do not re-derive what those files already record, and do not re-run measurements they already contain.

This run is bounded and unsupervised. That means:
- Commit each piece as it becomes correct, and keep docs/RESUME.md current as you go. Anything not written down when this run ends is lost.
- Commit BEFORE verifying, not after. On 2026-07-30 a run of this kind wrote a correct fix, planned to commit once a live test confirmed it, and ended with the fix uncommitted in the working tree while docs/RESUME.md claimed otherwise. A commit survives; a working tree on an unattended machine survives by luck. If verification then fails, amend or revert - that is cheap, and losing the work is not.
- Do not start anything that outlives this turn. A background job launched with run_in_background dies with this process: the same run lost a live verification that way, and its log stopped mid-login with no exit marker. If a check takes longer than the work, commit first and leave the check as the next action in docs/RESUME.md.
- Run scripts/dev.ps1 all before pushing.
- Do NOT make decisions reserved for the maintainer (see docs/agent-operating-model.md, "What still needs a human"). If the top item needs one, write the open question into docs/BACKLOG.md and move to the next item instead.
- If docs/RESUME.md says the work is finished, clear it back to its placeholder line and pick the top unblocked backlog item.
"@

if ($DryRun) { Say "would-launch" $budgetNote; exit 0 }

$claudeCmd = Resolve-ClaudeLauncher
if (-not $claudeCmd) { Say "launch-failed" "no executable claude (.cmd/.exe) found on PATH"; exit 0 }

# The prompt goes in over stdin, not as an argument. A .cmd is executed by
# cmd.exe, which treats a newline in its command line as end-of-command - the
# multi-line prompt below would arrive truncated at line one, and the rest
# would be interpreted as commands. `claude -p` with no query argument reads
# the prompt from stdin, which has no such hazard and no quoting rules.
$stamp = [DateTimeOffset]::UtcNow.ToUnixTimeSeconds()
$runLog = Join-Path $queueDir "resume-run-$stamp.log"
$promptFile = Join-Path $queueDir "resume-prompt-$stamp.txt"
$launched = $null

# NOT `claude` directly, as it was until 2026-07-29. The agent is started by
# unattended-run.ps1, which holds a wake lock for the life of the run and then
# writes what the run actually achieved into this same log. Launching the agent
# bare meant Modern Standby froze it ~90 seconds in and nothing ever said so;
# the whole post-mortem is in that script's header.
#
# The recorded pid is now this wrapper. -Stop already kills the whole tree
# (taskkill /T, added after the 2026-07-26 orphan incident), so the extra layer
# costs nothing there, and gate 2's process-name check already accepts a
# powershell wrapper.
$wrapper = Join-Path $PSScriptRoot "unattended-run.ps1"
if (-not (Test-Path $wrapper)) { Say "launch-failed" "unattended-run.ps1 missing next to the runner"; exit 0 }

try {
    Set-Content -Path $promptFile -Value $prompt -Encoding utf8 -ErrorAction Stop
    $env:OPAL_UNATTENDED_RESUME = "1"
    $wrapperArgs = @(
        '-NoProfile', '-ExecutionPolicy', 'Bypass', '-WindowStyle', 'Hidden', '-File', "`"$wrapper`"",
        '-ClaudeCmd', "`"$claudeCmd`"",
        '-PromptFile', "`"$promptFile`"",
        '-RunLog', "`"$runLog`"",
        '-QueueDir', "`"$queueDir`"",
        '-RepoRoot', "`"$repoRoot`""
    )
    if ($env:OPAL_RESUME_NO_WAKELOCK -eq '1') { $wrapperArgs += '-NoWakeLock' }
    $launched = Start-Process -FilePath 'powershell.exe' `
        -ArgumentList $wrapperArgs `
        -WorkingDirectory $repoRoot `
        -WindowStyle Hidden -PassThru -ErrorAction Stop
} catch {
    Say "launch-failed" $_.Exception.Message
    exit 0
}

$state = [pscustomobject]@{ last_launch = $now; last_pid = $launched.Id }
try { $state | ConvertTo-Json -Depth 5 | Set-Content $statePath -Encoding utf8 -ErrorAction Stop } catch { }

Say "launched" "pid $($launched.Id), $budgetNote, log $runLog"
exit 0
