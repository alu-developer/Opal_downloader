# Resume note

Scratch state for work that is **in flight right now**. Kept in git so it
survives a killed turn, a dead session, and a fresh clone.

`docs/BACKLOG.md` says what should happen and stays tidy. This file is allowed
to be messy: it is the thought that would otherwise only exist in a context
window, and a context window does not survive the usage limit being hit
mid-turn.

**Keep it current while working**, not at the end — the end is exactly the part
that does not always arrive. Update it whenever the answer to "what am I doing
and what's next" changes materially. When the work lands, clear it back to the
placeholder line below.

The SessionStart hook reads this file and hands it to the next session. The
scheduled resume runner also treats a non-placeholder file here as "there is
work", so leaving stale content in it will wake an unattended run for nothing.

---

**Verifying the inline-preview blocker. Maintainer approved building it
(2026-07-27); the code is committed (`6c75e98`) and is NOT yet proven safe.**

What it does: aborts `document` requests that are under `/opal/FolderResource/`
**and** in a subframe — OPAL's inline file previews, ~30 MB per course per
discovery pass, which nothing in this package ever reads (there is no iframe
handling at all). `OPAL_KEEP_FILE_PREVIEWS=1` restores the old behaviour.

**The verification, and it is the whole point.** A file *count* is not
acceptable evidence here: the 2026-07-26 concurrency work lost nine files while
counts looked normal, and the 2026-07-21 poll change lost files byte-for-byte
identically to the unfixed code. So `internal/scraper/filelist_probe_test.go`
writes every file's course, section, name and URL, sorted, to a diffable file.

Run both, then diff. An empty diff is the only acceptable result:

    OPAL_FILELIST=before OPAL_KEEP_FILE_PREVIEWS=1 go test ./internal/scraper/ -run TestFileListSnapshot -v -timeout 30m
    OPAL_FILELIST=after                            go test ./internal/scraper/ -run TestFileListSnapshot -v -timeout 30m
    diff "tmp/filelist-before.txt" "tmp/filelist-after.txt"

**State right now:** the `before` (ground truth, previews kept) run is in
flight. `after` has not been run. Expect ~345 files.

**If the diff is not empty, revert `6c75e98` rather than tuning the filter** —
losing files is the failure mode this whole project fears most, and a
narrower abort condition that still loses one file is not an improvement. One
clean pair of runs is also not proof; repeat before believing it.
