# opal-downloader

**Editing this file needs my permission.**


## PRs, Branches and so on.

**No PRs.** Never open one, never close one, never merge one. If an open PR
turns up on GitHub, say so and leave it — that call is the maintainer's.

**No standing branches.** Work lands as a direct commit on `master`, pushed.

**Everything that writes works in its own worktree — sessions included.**
Decided 2026-08-11, after four sessions inside twenty minutes shared one
checkout and the autopilot kept spending whole cycles on "the tree is dirty,
stopping". Call `EnterWorktree` before your first write, with **no** name —
a fixed name collides with the other sessions doing the same. Subagents that
write: `isolation: "worktree"`. Read-only work — answering a question, a review
that files nothing — stays in the main checkout and takes no worktree.

**The main checkout is nobody's workspace.** Because nothing writes there it can
always be fast-forwarded, so after a push run `git -C <repo> pull --ff-only`:
what the maintainer looks at should be current.

**A worktree can run the real app**, and must, when the work needs it.
`sync.lock` and the browser login profile live under `~/`, shared by every
worktree, so "one crawl at a time" still holds across them. What a worktree
lacks is the gitignored pair: copy `config.yaml` in from the main checkout and
`go build` its own `main.exe`. Use a scratch `download_path`, never the
maintainer's real folder.

**Finishing means pushing.** Commit, `git push origin HEAD:master`, then
`ExitWorktree` with `action: "remove"` — even if the work is partial or tests
fail, and then say plainly what is broken. A worktree left behind is invisible
work, which is the whole reason PRs are gone. If the push is rejected because
someone landed first: `git fetch origin`, `git rebase origin/master`, push
again — and on a conflict in `docs/BACKLOG.md` keep **both** sides, never drop
anyone else's entries.


## important stuff

**"Unattended runs can log in, because 2FA can be done by TU-Fast"**
TU-Fast is installed in the dedicated login profile and completes
credentials *and* 2FA by itself — `login`/`sync`/`list` trigger it automatically
when the saved session is stale, with nobody at the machine. Live-verified
2026-08-01: expired state → auto-login → 8 courses in 3.7s, no click. So never
report "needs the maintainer for 2FA or fresh cookies".

**Generally: if you need a live crawl, then do one, don't ask me**
Live crawls against the real account are wanted, not rationed. Never park an item as
"waits for a fresh day", "needs real-account load", or "no question answerable
without a live run". Just run it. Write the prediction down first, then run.

**Don't leave technical details to me**
You should decide for your own, what is better. You can ask me about product-questions, but decide the rest for yourself. 

**If you fix something: rather make something old that is bad new/change it, then just adding something new**
I want to try to reduce slop and complexity-building.

Concretely, every change is judged on what the codebase looks like afterwards,
not on how small the diff was:

- When the code you touch is old and already complicated, leave it **simpler**
  than you found it. Reshaping or deleting what is in the way beats adding a
  layer next to it, even when that means changing more lines than the fix
  strictly needed. A bigger diff that removes a concept is cheaper than a
  three-line diff that adds one.
- Before adding a mechanism, look for the one that nearly does the job
  already and extend it. Two mechanisms for one problem is the thing I am
  trying to avoid — a new flag, knob, config key, file or abstraction for a
  one-off is almost always the wrong answer.
- Say in your report what you removed, merged or reshaped, not just what you
  added. "Nothing to simplify here" is a fine answer when it is true; silence
  about it is not.
- If a feature cannot be made simple enough to be worth it, say so plainly
  and offer to drop it instead of carrying it. I would rather lose a
  nice-to-have than keep machinery nobody can hold in their head.