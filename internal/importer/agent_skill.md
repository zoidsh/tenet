Run `tenet` on your staged changes before every commit, and `tenet --base <default branch>` before you call a branch done. It lints the changed lines against the repository's `tenets.yml`, the rules its maintainers wrote in English. It exits 0 when it found nothing, 1 when it found something and 2 when the run itself broke; a run with no `TYPESAFE_API_KEY` is broken, not clean, so say so rather than committing as though the lint had passed.

Read the findings with `--format json`. Each is an entry in `findings` carrying `file`, `line`, `tenet`, `probability`, `fail` and `message`, where `message` is the tenet's own sentence; `next` is the single line saying what to do about all of them.

Take each finding in turn: open `file` at `line`, read the sentence in `message`, and change the code so the sentence holds. Rewrite the comment, drop the fallback, rename the thing, whatever that sentence asks for.

When the code is right for a reason the tenet cannot see, exempt the line rather than contorting it: put `tenet:ignore <id>` in a comment on that line and write the reason in the same comment. `tenet:ignore-next-line <id>` covers the line below and `tenet:ignore-file <id>` covers the file.

Never edit `tenets.yml`, never lower a tenet's `fail`, and never reword or delete a rule to make a finding go away; those are the maintainers' to change, not yours. `tenet check` measures a tenet against labelled examples and is how a human tunes one, so it is not a step in your work. When you cannot tell whether a finding is right, leave it and say so in the commit message or in your report.

A worked example. The finding:

```json
{"file": "internal/cache/cache.go", "line": 42, "tenet": "comment-why", "probability": 0.91, "fail": 0.8, "message": "A comment says why the code exists or why it is written this way, not what the code does."}
```

Line 42 and the line under it:

```go
// loop over the entries and delete the expired ones
for key, entry := range c.entries {
```

The comment narrates the loop the reader can already see, so replace it with the reason the sweep is there:

```go
// Expiry is swept on read because the cache has no goroutine of its own.
for key, entry := range c.entries {
```
