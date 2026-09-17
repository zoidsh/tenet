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

## Checking a tenet

`tenetlint check` tells you whether a tenet is phrased well enough to lint with, by running it over examples you have labelled yourself. Give a tenet an `examples` list, each with a `label` of `violation` or `ok`, the `code` it is about, and on a violation the `lines` a finding should land on: one line, or a `[first, last]` pair when the violation spans several and naming any line of it is right. Keep them in a sibling file with `examples_from: examples/comment-why.yml` when they crowd the config out, which is what this repository does for its own four tenets. Examples are never shown to the model and never enter a tenet's hash, so adding one costs you nothing in the lint cache.

```yaml
    examples:
      - label: violation
        lines: 2
        code: |
          func add(a, b int) int {
              // add a and b
              return a + b
          }
      - label: violation
        lines: [3, 5]
        code: |
          func total(rows []Row) int {
              sum := 0
              // walk the rows and add up
              // the amount on each one
              // into sum
              for _, r := range rows {
                  sum += r.Amount
              }
              return sum
          }
      - label: ok
        code: |
          // snappy v0.0.4 panics on a zero-length block, so short-circuit here.
          if size == 0 {
              return &Frame{Kind: head[0]}, nil
          }
```

Then run `tenetlint check`, which judges every example of every tenet that has any, or `tenetlint check comment-why` for one of them. The answers are cached under the same cache the lint uses, so a re-run after an edit costs only the examples whose question changed.

```
comment-why: sharp
  examples          8 (4 violation, 4 ok)
  auc               1.00
  accuracy          1.00 at threshold 0.50, 0.88 at confident 0.70
  mean probability  violation 0.84, ok 0.14, gap 0.69
  location          4 of 4 lines named (1.00)
```

Choose the examples as carefully as the wording: they are what the numbers mean. This repository leaves one case out of `comment-why` on purpose, a function whose only comment is a `TODO`, because the model scores it 0.20 and the tenet never says where it stands on TODOs; a tenet that has not taken a position cannot be measured on one.

The AUC is the chance the tenet scores a violation above an innocent example, which is what says whether the wording separates them at all; the accuracies say whether your `threshold` and `confident` are the right places to cut. A tenet is `sharp` when nothing lands on the wrong side of its threshold, `usable` when the ranking is still good enough to lint with, and `blurry` when it is not; under six examples, reported as `too few examples`, there is nothing worth measuring. Every misjudged example is listed with its probability and its first line, so the next edit to the criteria has something to aim at, and one line of advice names what usually moves the numbers: a `false` criterion when the innocent examples score high, a `true` criterion when the violations score low, and a rewrite of the sentence itself when both sit in the middle. `check` reports and never fails: it exits 0 whatever the numbers say, and 2 only when the config or the API is broken. `--format json` gives the same numbers for a script, `--min-examples` moves the bar, and `--no-cache` asks again.

Set `TYPESAFE_API_KEY` to your key. `TYPESAFE_BASE_URL` sends the requests to another host, such as a proxy or a local stand-in, and `TENETLINT_SKIP=1` makes the installed pre-commit hook exit without linting.

Pre-release: nothing here is stable yet, and the tenet format, the flags and the output may all change without notice.
