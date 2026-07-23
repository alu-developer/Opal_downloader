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
    [switch]$Force
)

$ErrorActionPreference = 'SilentlyContinue'
Set-StrictMode -Off

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

# --- load state ---------------------------------------------------------------
$state = $null
if (Test-Path $statePath) {
    try { $state = Get-Content $statePath -Raw -ErrorAction Stop | ConvertFrom-Json -ErrorAction Stop } catch { $state = $null }
}
if ($null -eq $state) { $state = [pscustomobject]@{ last_launch = 0; last_pid = 0 } }

# --- gate 2: is a previous unattended run still going? ------------------------
# Launching a second one would have two agents editing the same worktree.
if ($state.last_pid -and [int]$state.last_pid -gt 0) {
    $p = Get-Process -Id ([int]$state.last_pid) -ErrorAction SilentlyContinue
    if ($p -and $p.ProcessName -match 'claude|powershell') {
        Say "skip" "previous resume run still active (pid $($state.last_pid))"
        exit 0
    }
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
    if (-not $budget.Known) { Say "skip" "no usable budget reading - refusing to guess"; exit 0 }
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
    $resume = (Get-Content $resumePath -Raw -ErrorAction SilentlyContinue)
    if ($resume -and $resume -notmatch '(?m)^_Nothing in flight\.') { $hasWork = $true }
}
if (-not $hasWork) {
    $backlog = Join-Path $repoRoot "docs\BACKLOG.md"
    if (Test-Path $backlog) {
        foreach ($line in @(Get-Content $backlog -ErrorAction SilentlyContinue)) {
            if ($line -match '^##\s+Done recently') { break }
            if ($line -match '^###\s+') { $hasWork = $true; break }
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
- Run scripts/dev.ps1 all before pushing.
- Do NOT make decisions reserved for the maintainer (see docs/agent-operating-model.md, "What still needs a human"). If the top item needs one, write the open question into docs/BACKLOG.md and move to the next item instead.
- If docs/RESUME.md says the work is finished, clear it back to its placeholder line and pick the top unblocked backlog item.
"@

if ($DryRun) { Say "would-launch" $budgetNote; exit 0 }

$runLog = Join-Path $queueDir "resume-run-$([DateTimeOffset]::UtcNow.ToUnixTimeSeconds()).log"
$launched = $null
try {
    $env:OPAL_UNATTENDED_RESUME = "1"
    # --model sonnet: the documented default. An unattended run does not get to
    # escalate to Opus - that is the maintainer's call, made in person.
    # --dangerously-skip-permissions: required for any unattended launch, same
    # reasoning as rate-limit-keepwarm.ps1 (it skips the startup trust dialog).
    $launched = Start-Process -FilePath "claude" `
        -ArgumentList @('-p', $prompt, '--model', 'sonnet', '--dangerously-skip-permissions') `
        -WorkingDirectory $repoRoot `
        -RedirectStandardOutput $runLog `
        -RedirectStandardError "$runLog.err" `
        -WindowStyle Hidden -PassThru -ErrorAction Stop
} catch {
    Say "launch-failed" $_.Exception.Message
    exit 0
}

$state = [pscustomobject]@{ last_launch = $now; last_pid = $launched.Id }
try { $state | ConvertTo-Json -Depth 5 | Set-Content $statePath -Encoding utf8 -ErrorAction Stop } catch { }

Say "launched" "pid $($launched.Id), $budgetNote, log $runLog"
exit 0
