# PreToolUse backstop for the queue-run rate-limit gate (see ~/.claude/skills/queue-run/SKILL.md).
# Blocks new Agent-tool launches once usage is high enough that the gate's own
# budget formula would allow zero new tasks anyway. Advisory budget math stays
# in the skill prompt; this is only the hard stop so a launch can't slip
# through if that prompt guidance gets missed in a long run.
param(
    [string]$Path = "$env:USERPROFILE\.claude\rate-limit-status.json"
)

$ErrorActionPreference = 'SilentlyContinue'

if (-not (Test-Path $Path)) { exit 0 }

try {
    $data = Get-Content $Path -Raw -ErrorAction Stop | ConvertFrom-Json -ErrorAction Stop
} catch {
    exit 0
}

$five = $data.five_hour.used_percentage
$seven = $data.seven_day.used_percentage

$blockFive = ($null -ne $five) -and ($five -ge 80)
$blockSeven = ($null -ne $seven) -and ($seven -ge 85)

if (-not ($blockFive -or $blockSeven)) { exit 0 }

$reasonParts = @()
if ($blockFive) { $reasonParts += "5-hour usage at $five% (>=80)" }
if ($blockSeven) { $reasonParts += "7-day usage at $seven% (>=85)" }
$reason = "queue-run rate-limit gate: " + ($reasonParts -join "; ") + ". Let already-running tasks finish; don't start new ones until usage drops."

$output = @{
    hookSpecificOutput = @{
        hookEventName            = "PreToolUse"
        permissionDecision       = "deny"
        permissionDecisionReason = $reason
    }
} | ConvertTo-Json -Depth 5 -Compress

Write-Output $output
exit 0
