# tenetlint

tenetlint is a command-line linter for the rules you wrote in English. It reads tenets such as "a comment says why, not what" from a `tenets.yml`, sends your staged changes to TypeSafe's jev model for judgement, and reports each violation with a file, a line and a probability, so the conventions in your CLAUDE.md become a gate you can run on every commit instead of a document nobody rereads.

Every path tenetlint prints, including the `file` field of `--format json`, is relative to the directory you ran it from, whatever part of the repository that is.

## Usage

Start with `tenetlint init`, which drafts a `tenets.yml` from the instruction files your agents already read. It splits each file into sentences and list items, asks jev what kind of instruction each one is and whether a diff alone settles it, and keeps the rules a diff is enough to judge; what describes your project rather than instructing anyone is reported and left out. A rule phrased as an instruction to the agent, such as "never print the key", is kept when the thing it forbids would be visible in the changed lines. Without `--from` it reads every one of `CLAUDE.md`, `AGENTS.md`, `.cursorrules`, `.cursor/rules/*.mdc`, `.github/copilot-instructions.md`, `.github/instructions/*.instructions.md` and `BUGBOT.md` that your repository has; with `--from path` it reads exactly the files you name. `--dry-run` prints the table without writing anything, `--force` replaces a `tenets.yml` that is already there, and `--format json` gives you every candidate with its probabilities.

```
CLAUDE.md
  line  kind       kind p  checkable p  tenet                          sentence
  7     process    0.98    0.17         -                              Setup: `mise install`. Every `go`, `golangci-lint` and `gor…
  15    process    0.80    0.75         never-print-key-commit         Never print the key or commit it.
  19    code-rule  1.00    0.81         never-code-does-name-smaller   Never what the code does; a name or a smaller function says…
```

Then read what it drafted, delete the rules you did not mean, and run `tenetlint`, which lints your staged changes. `tenetlint --base main` lints the whole branch instead, and naming paths lints those files whether or not they are staged. A finding at or above `--fail-on`, `warn` by default, exits 1; a broken run exits 2.

Finally, `tenetlint hook install` writes a pre-commit hook that runs the lint on every commit, and `tenetlint hook uninstall` takes it away again.

## Criteria

A tenet is judged by its sentence alone unless you give it criteria: a `true` description of what a violation looks like and a `false` description of what an innocent change looks like. `init` drafts no criteria, because they are the one part of a tenet the model cannot guess at, and they are the lever that moves a rule from roughly right to reliable. Add them to any tenet the lint gets wrong, in the words you would use to explain the call to a new reviewer.

```yaml
  - id: comment-why
    tenet: A comment says why the code exists or why it is written this way, not what the code does.
    criteria:
      true: A comment that restates what the code visibly does, narrates steps, or is a section label.
      false: The comment gives a reason, a constraint, a contract, or a warning about ordering that the code does not show.
```

Set `TYPESAFE_API_KEY` to your key. `TYPESAFE_BASE_URL` sends the requests to another host, such as a proxy or a local stand-in, and `TENETLINT_SKIP=1` makes the installed pre-commit hook exit without linting.

Pre-release: nothing here is stable yet, and the tenet format, the flags and the output may all change without notice.
