Pass `--format json` on every `tenet` and `tenet check` call you make, written out rather than left to the default, because a harness that gives the command a pseudo-terminal is handed the text report instead.

### Running the lint

Run `tenet --format json` on your staged changes before every commit, and `tenet --base <branch> --format json`, the branch you started from and usually `main`, before you call a branch done. It lints the changed lines against the repository's `tenet.yml`, exiting 0 clean, 1 on a finding and 2 when the run broke; with no key, from `tenet auth typesafe` or `TYPESAFE_API_KEY`, or with no `tenet` on your PATH, it is broken, not clean, so say so rather than committing as though it had passed.

If the tenet plugin is installed, its hook runs the staged check when you commit; the answers are cached, so running it yourself first costs nothing and tells you sooner.

### Reading the report

Each entry in `findings` carries `file`, `line`, `tenet`, `probability`, `fail` and `message`, the tenet's own sentence; `next` says what to do about them all.

Take each finding in turn: open `file` at `line`, read the sentence in `message`, and change the code so the sentence holds. Rewrite the comment, drop the fallback, rename the thing.

When you cannot tell whether a finding is right, leave it and say so in the commit message or in your report.

### When you believe a finding is wrong

Never write a `tenet:ignore`, `tenet:ignore-next-line` or `tenet:ignore-file` directive. A directive is a person's decision about a false positive they have confirmed, not your way past a finding.

Stop instead and ask. Show the line, quote the tenet's sentence from `message`, and say why you think the rule misfired on this code. The person then sharpens the tenet's criteria, writes the directive themselves with a reason, or tells you the finding was right and the code is what changes.

### What you never do while fixing findings

While you are fixing findings to get a commit through, `tenet.yml` and `examples/` are not yours to touch: never edit `tenet.yml`, never lower a tenet's `fail`, never set `TENET_SKIP`, never reword or delete a rule to make a finding go away, and never write a directive. `tenet baseline` accepts what a codebase already had, so never run it over a finding your own change introduced. A `[baselined]` finding, in the report or in the `baselined` array, is accepted history, not yours to fix.

Setting tenet up and adding a rule are the two jobs that do edit `tenet.yml` and `examples/`, in the way the next two sections describe, and nowhere else.

### Setting tenet up

Do this when a person asks you to set tenet up, or when the repository has no `tenet.yml`. Installing the binary and running `tenet auth` stay with the person; the rest is one instruction to you.

1. Run `tenet auth --status`. With no key, ask the person to run `tenet auth` and stop there, because the key is theirs to paste.
2. Run `tenet init`. It reads `AGENTS.md`, `CLAUDE.md` and the other instruction files and asks jev what each sentence is, so do not parse those files yourself. Read the table it prints: every sentence it kept is now a drafted tenet in `tenet.yml`.
3. Run `tenet rules`. For each drafted tenet that says what a built-in rule already says, delete the draft and name the built-in id under `rules:`, or the preset that holds it when several drafts map into the one preset. Keep a comment with the source line the draft came from.
4. Run `tenet hook install`, so the built-in rules gate every commit from here on. Steps 1 to 4 are under two minutes; calibration comes after them.
5. Calibrate each remaining custom tenet by the recipe in `rules/README.md`:
   - Write at least twelve labelled examples in `examples/<id>.yml` and point the tenet at them with `examples_from: examples/<id>.yml`. Roughly half are `violation`, each with the `lines` a finding should land on, and the rest are `ok`. Mine them from this repository and its history where you can, and make them hard: include the innocent look-alikes a careless reading of the tenet would flag.
   - Run `tenet check <id> --runs 3 --format json` and read the misjudged examples.
   - Edit `criteria.true` and `criteria.false` and nothing else. Never reword the tenet sentence and never move `fail`. A criterion names the shape the misjudged examples share, never their text, and a clause you add needs two examples behind it.
   - Four attempts. A tenet that is not sharp after four stays in the file with a comment saying what it is short of.
   - A label you are not sure of gets a `note` saying what you were deciding, and a question for the person, rather than a guess.
6. Commit `tenet.yml` and the examples.
7. Report as in the last section.

### Adding a rule later

The same flow runs again whenever a rule arrives: a person asks for one, or `AGENTS.md` or `tenet.yml` has gained a sentence since the last run. Take only the new tenets through step 5 and leave the rest as they are. Examples are how you tell the two apart: a tenet with an `examples` list or an `examples_from` path has been calibrated, and one with neither has not, so calibrate it before it gates anybody's commit.

### Reporting

When a hook blocked your commit and you fixed the line it named, say so in one line of your report, so the person sees what it caught.

After a setup or a new rule, report it as a colleague would: how many sentences `tenet init` read, how many of them mapped to built-in rules, how many are custom tenets, which of those now read sharp, and one line saying the examples are there to read.

### A worked example

The report, less its `stats` and `skipped`:

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
  "next": "fix the lines above, then commit again"
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
