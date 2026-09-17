---
name: tenet
description: Run the tenet lint and act on what it finds. Use before every git commit, before declaring a branch or a task done, and whenever a tenet finding appears in output you are reading.
---

Run `tenet` on your staged changes before every commit, and `tenet --base <branch>`, the branch you started from and usually `main`, before you call a branch done. It lints the changed lines against the repository's `tenets.yml`, exiting 0 clean, 1 on a finding and 2 when the run broke; without `TYPESAFE_API_KEY`, or with no `tenet` on your PATH, it is broken, not clean, so say so rather than committing as though it had passed.

If the tenetlint plugin is installed, its hook runs the staged check when you commit; the answers are cached, so running it yourself first costs nothing and tells you sooner.

Read the report with `--format json`. Each entry in `findings` carries `file`, `line`, `tenet`, `probability`, `fail` and `message`, the tenet's own sentence; `next` says what to do about them all.

Take each finding in turn: open `file` at `line`, read the sentence in `message`, and change the code so the sentence holds. Rewrite the comment, drop the fallback, rename the thing.

When the code is right for a reason the tenet cannot see, exempt the line rather than contorting it: put `tenet:ignore <id>` in a comment on that line and write the reason after the id, in the same comment, where the words following it are left alone. `tenet:ignore-next-line <id>` covers the line below and `tenet:ignore-file <id>` covers the file.

Never edit `tenets.yml`, never lower a tenet's `fail`, never set `TENETLINT_SKIP` to get a commit through, and never reword or delete a rule to make a finding go away; those are the maintainers' to change, not yours. `tenet baseline` accepts what a codebase already had, so never run it over a finding your own change introduced, and `tenet check` is how a person tunes one. A `[baselined]` finding, in the report or in the `baselined` array, is accepted history, not yours to fix.

When you cannot tell whether a finding is right, leave it and say so in the commit message or in your report.

A worked example. The report, less its `stats` and `skipped`:

```json
{
  "version": 1,
  "findings": [
    {
      "file": "internal/cache/cache.go",
      "line": 8,
      "tenet": "comment-why",
      "probability": 0.94,
      "fail": 0.8,
      "message": "A comment says why the code exists or why it is written this way, not what the code does, what it used to do, or what its declaration already states."
    }
  ],
  "next": "fix the lines above or mark one with a tenet:ignore <id> directive, then commit again"
}
```

Line 8 and the line under it:

```go
	// loop over the entries and delete the expired ones
	for key, entry := range c.entries {
```

Replace the comment with the reason the sweep is there:

```go
	// Expiry is swept on read because the cache has no goroutine of its own.
	for key, entry := range c.entries {
```
