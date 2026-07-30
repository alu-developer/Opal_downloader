# Resume note

Scratch state for work that is **in flight right now**. Kept in git so it
survives a killed turn, a dead session, and a fresh clone.

`docs/BACKLOG.md` says what should happen and stays tidy. This file is allowed
to be messy: it is the thought that would otherwise only exist in a context
window, and a context window does not survive the usage limit being hit
mid-turn.

**Keep it current while working**, not at the end - the end is exactly the part
that does not always arrive. Update it whenever the answer to "what am I doing
and what's next" changes materially. When the work lands, clear it back to the
placeholder line below.

The SessionStart hook reads this file and hands it to the next session. The
scheduled resume runner also treats a non-placeholder file here as "there is
work", so leaving stale content in it will wake an unattended run for nothing.

---

**2026-07-30: auditing `docs/setup-friction.md`'s priority table against the
code.** Ran `scripts/test-fresh-install.ps1` end to end - all automatable checks
pass, but it emits NOTEs for friction that looks already fixed.

Item 2 ("document `go build -o opal-downloader.exe .` for Windows") is **already
in README.md** at lines 54-60, including a Windows note. So the table lists
shipped work as pending, which is how a later session ends up redoing it or
believing the tool still has the trap.

Suspected also-done, to verify one by one against the code before touching the
doc: item 3 (offline `status`), 4 (wrapped Playwright errors - the live run did
print a friendly line before the raw one), 6 (`init` "Next steps" ordering -
observed output already puts editing config.yaml first), 7 (`setup`
meta-command). `CLAUDE.md` lists `setup` and `status` as real subcommands.

Also worth fixing: `test-fresh-install.ps1`'s step-5 NOTE reports the
extensionless-binary trap as if undocumented, when the README documents it.

Nothing committed yet this iteration.
