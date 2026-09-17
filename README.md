# tenetlint

[![CI](https://github.com/zoidsh/tenetlint/actions/workflows/ci.yml/badge.svg)](https://github.com/zoidsh/tenetlint/actions/workflows/ci.yml)

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

Then read what it drafted, delete the rules you did not mean, and run `tenetlint`, which lints your staged changes. `tenetlint --base main` lints the whole branch instead, and naming paths lints those files whether or not they are staged. Any finding exits 1; a clean run exits 0 and a broken one exits 2.

Finally, `tenetlint hook install` writes a pre-commit hook that runs the lint on every commit, and `tenetlint hook uninstall` takes it away again.

## Pass or fail

A tenet is one cutoff: `fail`, 0.8 unless the tenet says otherwise. The model answers each window with a probability, and at or above the cutoff it is a finding, under it nothing at all. There is no severity, no warning tier and no flag that lets a finding through, because a rule that is not worth failing a commit over is a rule whose cutoff is in the wrong place. `tenetlint check` is where you find that place: it measures a tenet against examples you have labelled and tells you what each cutoff would cost you, and `fail` in the tenet or in an `override` is where you write the answer down. `--verbose` lists the near misses, every tenet that came within 0.2 under its cutoff on a window, which is what a cutoff you are about to lower is really about.

```
internal/cache/cache.go:42: comment-why (p=0.91)
internal/judge/judge.go:118: no-fallback (p=0.86)

comment-why  A comment says why the code exists or why it is written this way, not what the code does.
no-fallback  Do not add fallbacks, default-to-something-that-works-ish behavior, or silent degradation paths.

2 findings · 7 windows, 9 calls, 4 cached · $0.0031 · 2.4s
fix the lines above or mark one with a tenet:ignore <id> directive, then commit again
```

## Configuration

A `tenets.yml` composes what will run out of the rules that ship inside the binary and the ones you write yourself. `tenetlint rules` lists the built-in rules with their tags and the presets that include them, `tenetlint rules comment-why` prints one of them in full, and `tenetlint presets` lists the presets, each a named list of rule ids. A rule may sit in several presets.

```yaml
version: 1
presets: [agent-hygiene]        # built-in presets, expanded in order
rules: [comment-why]            # individual built-in rules, added after presets
disable: [no-mocking]           # removed after expansion, by id
override:                       # per-id patches applied last
  no-fallback:
    fail: 0.9
    include: ["**/*.go"]
tenets: [...]                   # your own tenets, as before
```

The order is the order of that file: the presets in the order you list them, then the rules, then your own tenets, then the disables, then the overrides. A tenet of your own that carries a built-in id replaces that rule wholesale, where it stood, so moving a rule into your config to reword it does not reorder the report. An override patches only the fields it names and leaves the rest of the rule alone. An id that arrives twice, an unknown preset, rule, disable or override id, and a config that resolves to no tenets at all are each an error that names what it found. `tenetlint config` prints what your file resolves to, with the origin, cutoff and include globs of every tenet that will run.

```
tenets.yml

id                origin         fail  include
comment-why       agent-hygiene  0.80  **/*.go
no-fallback       agent-hygiene  0.80  **/*.go
```

`tenetlint init --preset agent-hygiene` writes a config that names that preset and nothing else, which is also what `init` writes when it finds no instruction file to read; add `--from` to draft your own rules into the same file underneath it.

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

`tenetlint check` tells you whether a tenet is phrased well enough to lint with, by running it over examples you have labelled yourself. Give a tenet an `examples` list, each with a `label` of `violation` or `ok`, the `code` it is about, and on a violation the `lines` a finding should land on: one line, or a `[first, last]` pair when the violation spans several and naming any line of it is right. Keep them in a sibling file with `examples_from: examples/comment-why.yml` when they crowd the config out. A built-in rule keeps its examples in the `examples.yml` beside its `rule.yml` under `rules/<id>/`, and `tenetlint check --builtin` measures every rule that ships in the binary, whatever your config turns on. Examples are never shown to the model and never enter a tenet's hash, so adding one costs you nothing in the lint cache.

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
  examples          14 (7 violation, 7 ok)
  auc               1.00
  accuracy          1.00 at fail 0.80 · 1.00 at 0.70, 1.00 at 0.80, 0.71 at 0.90
  mean probability  violation 0.89, ok 0.19, gap 0.71
  location          7 of 7 lines named (1.00)
```

Choose the examples as carefully as the wording: they are what the numbers mean. Where a label is a call the tenet's sentence does not obviously make, write the reason in the example's `note`; `comment-why` carries a function whose only comment is a `TODO`, labelled `ok` with a note saying that a TODO restates nothing, because the tenet is about a comment that repeats the code. `rules/README.md` is how the built-in rules were built, step by step, and is the recipe to follow for one of your own.

The AUC is the chance the tenet scores a violation above an innocent example, which is what says whether the wording separates them at all; the accuracy row says how the tenet's own `fail` does and what 0.70, 0.80 and 0.90 would have done with the same examples, which is the whole of what moving it buys. A tenet is `sharp` when nothing lands on the wrong side of its cutoff, `usable` when the ranking is still good enough to lint with, and `blurry` when it is not; under six examples, reported as `too few examples`, there is nothing worth measuring. Every misjudged example is listed with its probability and its first line, so the next edit to the criteria has something to aim at, and one line of advice names what usually moves the numbers: a lower `fail`, and which value, when the violations cluster just under the cutoff; a higher one when an innocent example reaches it; a `false` criterion when the innocent examples score high, a `true` criterion when the violations score low, and a rewrite of the sentence itself when both sit in the middle. `check` reports and never fails: it exits 0 whatever the numbers say, and 2 only when the config or the API is broken. `--format json` gives the same numbers for a script, `--min-examples` moves the bar, and `--no-cache` asks again. `--runs 3` judges every example three times, leaving the cache out of it so the passes are independent, and adds a `stability` line per tenet: the largest standard deviation it saw over any one example, and every example whose probability landed on both sides of the cutoff between passes. The numbers above that line are still the first pass's, so asking for several passes does not change what one of them says. It is what tells you whether a verdict sitting near `fail` is a verdict or a coin toss, and it is the evidence a cutoff of its own should rest on. Unlike the lint, `check` does not split an oversized request: an example longer than one request's token budget comes back as an API error rather than being judged in halves, so keep an example to the piece of code the tenet is about.

Set `TYPESAFE_API_KEY` to your key. `TYPESAFE_BASE_URL` sends the requests to another host, such as a proxy or a local stand-in, and `TENETLINT_SKIP=1` makes the installed pre-commit hook exit without linting. Without `--format`, output is text on a terminal and JSON anywhere else, because what reads a pipe is a script or an agent; `TENETLINT_FORMAT=text` or `json` settles it either way.

Pre-release: nothing here is stable yet, and the tenet format, the flags and the output may all change without notice.
