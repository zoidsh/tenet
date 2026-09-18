Pass `--format json` to the four commands that report on a run, the lint itself, `tenet init`, `tenet sync` and `tenet check`, written out rather than left to the default, because a harness that gives the command a pseudo-terminal is handed the text report instead. `tenet auth`, `tenet rules`, `tenet config` and `tenet hook install` print a listing rather than a result, take no such flag, and reject it.

### Running the lint

Run `tenet --format json` on your staged changes before every commit, and `tenet --base <branch> --format json`, the branch you started from and usually `main`, before you call a branch done. It lints the changed lines against the repository's `.tenet/config.yml`, exiting 0 clean, 1 on a finding and 2 when the run broke; with no key, from `tenet auth typesafe` or `TYPESAFE_API_KEY`, or with no `tenet` on your PATH, it is broken, not clean, so say so rather than committing as though it had passed.

If the tenet plugin is installed, its hook runs the staged check when you commit; the answers are cached, so running it yourself first costs nothing and tells you sooner.

### Reading the report

Each entry in `findings` carries `file`, `line`, `tenet`, `probability`, `fail` and `message`, the tenet's own sentence; `next` says what to do about them all.

Take each finding in turn: open `file` at `line`, read the sentence in `message`, and change the code so the sentence holds. Rewrite the comment, drop the fallback, rename the thing.

When you cannot tell whether a finding is right, leave it and say so in the commit message or in your report.

### When you believe a finding is wrong

Never write a `tenet:ignore`, `tenet:ignore-next-line` or `tenet:ignore-file` directive. A directive is a person's decision about a false positive they have confirmed, not your way past a finding.

Stop instead and ask. Show the line, quote the tenet's sentence from `message`, and say why you think the rule misfired on this code. The person then sharpens the tenet's criteria, writes the directive themselves with a reason, or tells you the finding was right and the code is what changes.

### What you never do while fixing findings

While you are fixing findings to get a commit through, `.tenet/` is not yours to touch: never edit the config, never lower a tenet's `fail`, never set `TENET_SKIP`, never reword or delete a rule to make a finding go away, and never write a directive. `tenet baseline` accepts what a codebase already had, so never run it over a finding your own change introduced. A `[baselined]` finding, in the report or in the `baselined` array, is accepted history, not yours to fix.

Setting tenet up and keeping it in step with the instruction files are the two jobs that do edit `.tenet/`, in the way the next three sections describe, and nowhere else.

### Setting tenet up

Do this when a person asks you to set tenet up. Installing the binary and running `tenet auth` stay with the person; the rest is one instruction to you.

1. Run `tenet auth --status`. With no key, ask the person to run `tenet auth` and stop there, because the key is theirs to paste.
2. Run `tenet init --format json`. It reads `AGENTS.md`, `CLAUDE.md` and the other instruction files and asks jev what each sentence is, so do not parse those files yourself. Read its `candidates`: each one carries the sentence, its `kind`, a `checkable_p` and whether it was `accepted`, and every accepted one is now a drafted tenet in `.tenet/config.yml`. Without `--format json` the same thing arrives as a table. Keep the drafts that are rules somebody meant, delete the rest, and say which you kept.
3. Run `tenet rules`. For each drafted tenet that says what a built-in rule already says, delete the draft and name that rule's id under `rules:`, as a block list, so that a comment holding the drafted `source` line, the file and the sentence it was read from, sits above the entry; `tenet sync` reads those comments to know which sentences the built-in rules already stand for. Two drafts often map to the one rule, which is one entry. Name a preset instead only when the drafts you deleted cover several rules of that preset. Then run `tenet config`, which prints what the file now resolves to and fails on one it cannot load.
4. Look at how the repository already runs its git hooks, whatever the tool is, and add tenet to it in that tool's own conventions: `tenet` as a pre-commit command, and `tenet --commit-msg` with the message file the tool passes as a commit-msg command. Only when nothing runs hooks there yet, run `tenet hook install`, which writes the two hooks itself and refuses when something else already manages them. Either way the built-in rules gate every commit from here on, and `--no-verify` is never an option for a commit you make. Steps 1 to 4 are under two minutes; calibration comes after them.
5. Calibrate each remaining custom tenet by the next section, one at a time.
6. Commit `.tenet/config.yml` and the examples.
7. Report as in the last section.

### Calibrating a tenet

A tenet is calibrated when labelled examples say what it catches and what it leaves alone. Keep them in `.tenet/examples/<id>.yml`, a bare YAML list whose entries take `label`, `lines`, `lang`, `note` and `code` and nothing else, because an unknown field fails the load:

```yaml
- label: violation
  lines: 3
  lang: go
  code: |
    func newClient(key string) *jev.Client {
        if verbose {
            fmt.Fprintf(os.Stderr, "asking jev with key %s\n", key)
        }
        return jev.New(key)
    }
- label: ok
  lang: go
  note: The status line says whether a key is set, never what it is.
  code: |
    func setOrNot(value string) string {
        if strings.TrimSpace(value) == "" {
            return "not set"
        }
        return "set"
    }
```

- The file is found by the tenet's id, so the tenet itself names nothing; `examples_from`, a path read relative to the directory the config is in, is for the examples you keep somewhere else.
- Twelve examples at least, roughly half of them `violation` with the `lines` a finding should land on and the rest `ok`. Mine them from this repository and its history where you can, and make them hard: include the innocent look-alikes a careless reading of the tenet would flag.
- A drafted tenet has no `criteria`, so write one: `true` describes what a violation looks like, `false` describes the innocent case that resembles it.
- Run `tenet check <id> --runs 3 --format json`. Read `verdict`, then `misjudged`, which names each example that landed on the wrong side of the cutoff by its first line, then `stability.crossed`. The verdict is the first pass alone, so an example in `crossed` moved across the cutoff between passes and counts as a miss.
- Edit `criteria.true` and `criteria.false` and nothing else. Never reword the tenet sentence and never move `fail`. A criterion names the shape the misjudged examples share, never their text, and a clause you add needs two examples behind it.
- Four attempts. A `verdict` of `sharp` with an empty `crossed` is the end of it; a tenet still `usable` or `blurry`, or one that keeps crossing, stays in the file with a comment saying what it is short of.
- A label you are not sure of gets a `note` saying what you were deciding, and a question for the person, rather than a guess.

The long form of this recipe, with what to do about each verdict, is `rules/README.md` in the tenet repository: https://github.com/zoidsh/tenet/blob/main/rules/README.md

### Keeping the config in step

Run `tenet sync --format json` whenever `CLAUDE.md`, `AGENTS.md` or another instruction file changes, including when the change is yours. It re-reads those files against the config and edits it in place, so never work the new sentences in by hand and never run `tenet init` again: init refuses a `.tenet/config.yml` that exists, and `--force` throws away every criterion and example anybody has written. `tenet sync --dry-run --format json` reports everything and writes nothing, which is how to see what a change to an instruction file would do before it does it.

`added` holds the sentences those files now say and the config did not, each already appended to `tenets:` as a draft carrying an `id`, a `tenet` and a `source`. Treat them exactly as you treat init's drafts, by steps 3 and 5 of Setting tenet up: map each to a built-in rule where one already says it, and calibrate the rest.

`changed` holds the rules an instruction file reworded. Each entry names the tenet, or the built-in `rule` its comment stands above, with the `old` sentence and the `new` one, and `text_updated` says whether the tenet's own sentence moved with it; where that is false somebody had edited the sentence and it stays as they wrote it. The criteria, examples and cutoff are kept either way, so run `tenet check <id> --runs 3 --format json` for each tenet named and calibrate it again by the section above if the verdict has slipped.

`stale` holds the tenets and `rules:` comments whose sentence no instruction file says any more. sync deletes none of them, because a rule may have moved to a file it does not read, or be one the person still wants. Delete the entries you are sure are gone, leave the ones you are not, and say in your report which you deleted and which you left.

When the person tells you a rule rather than writing it down, take their sentence as it stands, append one entry to `tenets:` by hand with `id`, `tenet` and `source`, and calibrate it by the section above before it gates anybody's commit.

Leave the tenets that are already calibrated alone. Examples are how you tell the two apart: a tenet with examples, inline or in `.tenet/examples/<id>.yml`, has been calibrated, and one with neither has not.

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
