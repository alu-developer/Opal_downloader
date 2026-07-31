# Show how much of the plan's rate-limit windows is still free.
#
# This is a wrapper. All the logic lives in ratelimit_core.py, because it used
# to live here as well, in a second copy - and on 2026-07-31 that copy was
# rebuilt without the rule that a window past its reset time is meaningless. It
# then reported 87% used against a real ~0%. One tested implementation, shared
# with statusline.py, is the fix; see ratelimit_core.py and test_ratelimit.py.
#
#   -Force     fetch even if the cache is still fresh (use sparingly - the
#              usage endpoint 429s if polled hard)
#   -NoFetch   show the cache as it is, never touch the network

param([switch]$Force, [switch]$NoFetch)

$ErrorActionPreference = 'Stop'

# `python` is normally on PATH via the WindowsApps shim, but that shim turns
# into a Store-install stub if Python is ever removed. Fall back to the py
# launcher rather than failing with a confusing error.
function Get-Python {
    foreach ($candidate in 'python', 'py') {
        $command = Get-Command $candidate -ErrorAction SilentlyContinue
        if ($command) { return $command.Source }
    }
    return $null
}

$python = Get-Python
if (-not $python) { throw "No Python interpreter found on PATH; cannot read usage data." }

# $PSScriptRoot, not a hardcoded path: the helpers must travel with this file.
$arguments = @((Join-Path $PSScriptRoot 'ratelimit_show.py'))
if ($Force) { $arguments += '--force' }
if ($NoFetch) { $arguments += '--no-fetch' }

& $python @arguments
exit $LASTEXITCODE
