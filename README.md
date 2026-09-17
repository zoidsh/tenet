# tenetlint

[![CI](https://github.com/zoidsh/tenetlint/actions/workflows/ci.yml/badge.svg)](https://github.com/zoidsh/tenetlint/actions/workflows/ci.yml)

tenetlint is a command-line linter for the rules you wrote in English. It reads tenets such as "a comment says why, not what" from a `tenets.yml`, sends your staged changes to TypeSafe's jev model for judgement, and reports each violation with a file, a line and a probability, so the conventions in your CLAUDE.md become a gate you can run on every commit.

## Install

The command is `tenet`, and every path below but the one from source installs `tenetlint` beside it as the same program under its old name. An unrelated npm package, `@jeikeilim/tenet`, also provides a `tenet` command, so if you have that one installed, call this one `tenetlint`.

Homebrew, on macOS and on Linux. tenetlint ships as a cask with a `binary` stanza, which a recent Homebrew installs on both:

```
brew install zoidsh/tap/tenetlint
```

npm, which carries the binary for your platform as an optional dependency and runs no install script:

```
npm install -g tenetlint
npx tenetlint
```

As a devDependency, so everyone working on the repository gets the same tenetlint:

```
npm install --save-dev tenetlint
```

The installer script, which puts the binary in `~/.local/bin`, or in `TENETLINT_INSTALL_DIR` when you set it, and never asks for sudo:

```
curl -fsSL https://raw.githubusercontent.com/zoidsh/tenetlint/main/install.sh | sh
```

From source, which needs a Go toolchain and gives you `tenet` alone, since the package is now `cmd/tenet`:

```
go install github.com/zoidsh/tenetlint/cmd/tenet@latest
```

Through the [pre-commit](https://pre-commit.com) framework, which builds tenetlint from source itself, fetching a Go toolchain of its own if the machine has none:

```yaml
repos:
  - repo: https://github.com/zoidsh/tenetlint
    rev: main
    hooks:
      - id: tenetlint
      - id: tenetlint-commit-msg
```

In GitHub Actions, where the action downloads the release binary for the runner and lints the pull request against its base:

```yaml
      - uses: actions/checkout@v5
        with:
          fetch-depth: 0

      - uses: zoidsh/tenetlint@v1
        with:
          api-key: ${{ secrets.TYPESAFE_API_KEY }}
```

Every release tarball, and the `checksums.txt` that covers them, is on [GitHub Releases](https://github.com/zoidsh/tenetlint/releases).

## Quick start

Set `TYPESAFE_API_KEY` to your key, which comes from a TypeSafe account at [typesafe.ai](https://typesafe.ai). Then, from the root of your repository:

1. Draft a `tenets.yml` from the instruction files your agents already read.

   ```
   tenet init
   ```

2. Read what it drafted, delete the rules you did not mean, and print what the file now resolves to.

   ```
   tenet config
   ```

3. Lint your staged changes.

   ```
   tenet
   ```

4. Install the hooks, so every commit is linted from here on.

   ```
   tenet hook install
   ```

`tenet init` splits each instruction file into sentences and list items, asks jev what kind of instruction each one is and whether a diff alone settles it, and keeps the rules a diff is enough to judge; what describes your project rather than instructing anyone is reported and left out. A rule phrased as an instruction to the agent, such as "never print the key", is kept when the thing it forbids would be visible in the changed lines.

Without `--from` it reads every one of `CLAUDE.md`, `AGENTS.md`, `.cursorrules`, `.cursor/rules/*.mdc`, `.github/copilot-instructions.md`, `.github/instructions/*.instructions.md` and `BUGBOT.md` that your repository has; with `--from path` it reads exactly the files you name, and the flag is repeatable. `--dry-run` prints the table without writing anything, `--force` replaces a `tenets.yml` that is already there, `--config path` writes somewhere other than the repository root, and `--format json` gives you every candidate with its probabilities.

```
CLAUDE.md
  line  kind       kind p  checkable p  tenet                          sentence
  3     context    1.00    0.10         -                              A Go CLI that lints code against English rules, judged by T…
  7     process    0.99    0.21         -                              Setup: `mise install`. Every `go`, `golangci-lint` and `gor…
  8     process    0.98    0.05         -                              Test: `go test -race ./...`
  10    process    0.99    0.06         -                              Release build check: `goreleaser build --snapshot --clean`
  11    process    1.00    0.07         -                              Before reporting a branch done: `tenet --base main`, which …
  12    process    1.00    0.07         -                              Before a release: `tenet .`, a full sweep, because diff-sco…
  16    process    0.50    0.43         -                              Tests that call the real jev API are skipped unless `TYPESA…
  16    process    0.80    0.32         -                              They cost money and need the network, so they are never par…
  16    process    0.74    0.77         never-print-key-commit         Never print the key or commit it.
  20    code-rule  1.00    0.65         comment-says-only-code-cannot  A comment says only what the code cannot: why a constraint …
  20    code-rule  1.00    0.82         never-code-does-name-smaller   Never what the code does; a name or a smaller function says…
  24    process    1.00    0.10         -                              This repo lifts nothing from the global rules: code changes…

12 candidates, 3 tenets, nothing written (--dry-run) · 1 calls, 0 cached · $0.0002 · 0.7s
```

`tenet` on its own lints your staged changes. `tenet --base main` lints the working tree against that git ref instead, and naming paths lints those files whether or not they are staged. `tenet hook install` writes a pre-commit hook that runs the lint on every commit and a commit-msg hook that lints the message, and `tenet hook uninstall` takes them away again.

## Pass or fail

A tenet is one cutoff: `fail`, 0.8 unless the tenet says otherwise. A window is a slice of one file small enough to ask the model about in a single call, at most 254 lines of it. The model answers each window with a probability, and at or above the cutoff it is a finding, under it nothing at all. Any finding exits 1; a clean run exits 0 and a broken one exits 2.

There is no severity, no warning tier and no flag that lets a finding through, because a rule that is not worth failing a commit over is a rule whose cutoff is in the wrong place. `tenet check` is where you find that place: it measures a tenet against examples you have labelled and tells you what each cutoff would cost you, and `fail` in the tenet or in an `override` is where you write the answer down. `--verbose` lists the near misses, every tenet that came within 0.2 under its cutoff on a window, which is what a cutoff you are about to lower is really about.

```
internal/cache/cache.go:11: comment-why (p=0.94)
internal/judge/judge.go:15: no-fallback (p=0.96)

comment-why  A comment says why the code exists or why it is written this way, not what the code does, what it used to do, or what its declaration already states.
no-fallback  Do not add fallbacks, default-to-something-that-works-ish behavior, or silent degradation paths. Either the operation succeeds as intended, or it raises an actionable error.

2 findings · 2 windows, 2 calls, 5 cached · $0.0001 · 0.8s
fix the lines above or mark one with a tenet:ignore <id> directive, then commit again
```

## Directives

Three directives exempt code from a tenet. `tenet:ignore` exempts the line it is written on, `tenet:ignore-next-line` the line below it, and `tenet:ignore-file` the whole file, wherever in that file you put it; the first line is the usual place, but it is not a rule. Each takes an optional comma-separated list of tenet ids and exempts only those; with no list it exempts every tenet. A directive that names an id your `tenets.yml` does not define, or a `tenet:ignore-` keyword that is not one of the three, fails the run rather than silently exempting nothing, because a typo you cannot see is worse than a run you have to fix.

```go
x := fallback() // tenet:ignore no-fallback

// tenet:ignore-next-line comment-why
y := 1 // set y to one
```

A directive only counts inside a comment, so a string, a test fixture or a sentence about directives does not quietly exempt the file it sits in. In code that means after a line comment marker, or between a block comment's markers, in the language the file's extension names; in a language tenetlint does not know it counts anywhere on the line. The check is textual rather than a parse, so a marker inside a string literal opens a comment as far as tenetlint is concerned and a directive after it counts.

In Markdown, YAML and other prose and data files a directive counts at the start of a line, after list markers, whitespace or the format's own comment marker, or inside an `<!-- -->` comment; a sentence that quotes one mid-line does not count. In a commit message it counts anywhere. The directive and its id list are cut out of the line before anything is sent to the model, and the line numbers you are shown are the ones in your file. A mention that does not count is left where it is.

## Configuration

A `tenets.yml` composes what will run out of the rules that ship inside the binary and the ones you write yourself. `tenet rules` lists the built-in rules with their tags and the presets that include them, `tenet rules comment-why` prints one of them in full, and `tenet presets` lists the presets, each a named list of rule ids. A rule may sit in several presets, and a rule in none is named under `rules:`.

```
agent-hygiene  The habits a coding agent slips into when nobody reads the diff.
  comment-why, no-mocking, no-transcript-comment, no-placeholder-phrase, assertion-justified, no-fallback

unslop-prose  The prose an LLM writes into a README when nobody rewrites the draft.
  project-specific, no-generic-conclusion, no-metaphor-noun, no-false-contrast
```

```yaml
version: 1
presets: [agent-hygiene]        # built-in presets, expanded in order
rules: [no-defensive-nil]       # individual built-in rules, added after presets
disable: [no-mocking]           # removed after expansion, by id
override:                       # per-id patches applied last
  no-defensive-nil:
    fail: 0.9
    include: ["**/*.go"]
    kind: [code]                # code, prose, data or commit; the globs narrow further
tenets: [...]                   # your own tenets, written out in full
```

A tenet's `kind` is how it says what it is about. Every file is code, prose or data, read off its name: `.md`, `.rst`, `.txt` and the like, everything under `locales/` and `i18n/` whatever it is serialised as, and a README, CHANGELOG, CONTRIBUTING or LICENSE are prose; `.json`, `.yml`, `.toml`, `.csv` and lock files are data; anything else under `docs/` is prose, so a `docs/api.json` stays data; everything else is code.

A commit message is a kind of its own, `commit`, read off the literal name `COMMIT_EDITMSG` it is linted under. The model is told which it is looking at, so a document is judged as a document rather than as source code, and a tenet that names a kind is asked only about files of that kind, on top of its include and exclude globs. A tenet that names none is asked about everything its globs match.

The order is the order of that file: the presets in the order you list them, then the rules, then your own tenets, then the disables, then the overrides. A tenet of your own that carries a built-in id replaces that rule wholesale, where it stood, so moving a rule into your config to reword it does not reorder the report. An override patches only the fields it names and leaves the rest of the rule alone.

An id that arrives twice, an unknown preset, rule, disable or override id, and a config that resolves to no tenets at all are each an error that names what it found. `tenet config` prints what your file resolves to, with the origin, kinds, cutoff and include globs of every tenet that will run.

```
$ tenet config --config tenets.yml
tenets.yml

id                     origin         kind  fail  include
comment-why            agent-hygiene  code  0.80  **/*.go, **/*.ts, **/*.tsx, **/*.py
no-mocking             agent-hygiene  code  0.80  **/*_test.go, **/*.test.ts, **/*.test.tsx, **/*.spec.ts, **/*.spec.tsx, **/test_*.py, **/*_test.py
no-transcript-comment  agent-hygiene  code  0.80  **/*.go, **/*.ts, **/*.tsx, **/*.py
no-placeholder-phrase  agent-hygiene  code  0.80  **/*.go, **/*.ts, **/*.tsx, **/*.py
assertion-justified    agent-hygiene  code  0.80  **/*.go, **/*.ts, **/*.tsx, **/*.py
no-fallback            agent-hygiene  code  0.80  **/*.go, **/*.ts, **/*.tsx, **/*.py
no-defensive-nil       rules          code  0.80  **/*.go, **/*.ts, **/*.tsx, **/*.py
```

`tenet init --preset agent-hygiene` writes a config that names that preset and nothing else, which is also what `init` writes when it finds no instruction file to read; add `--from` to draft your own rules into the same file underneath it.

A tenet is judged by its sentence alone unless you give it criteria: a `true` description of what a violation looks like and a `false` description of what an innocent change looks like. `init` drafts no criteria, because they are the one part of a tenet the model cannot guess at, and the part that most changes what the model answers. Add them to any tenet the lint gets wrong, in the words you would use to explain the call to a new reviewer.

```yaml
  - id: comment-why
    tenet: A comment says why the code exists or why it is written this way, not what the code does.
    criteria:
      true: A comment that restates what the code visibly does, narrates steps, or is a section label.
      false: The comment gives a reason, a constraint, a contract, or a warning about ordering that the code does not show.
```

## Checking a tenet

`tenet check` tells you whether a tenet is phrased well enough to lint with, by running it over examples you have labelled yourself. Give a tenet an `examples` list, each with a `label` of `violation` or `ok`, the `code` it is about, and on a violation the `lines` a finding should land on: one line, or a `[first, last]` pair when the violation spans several and naming any line of it is right.

Keep them in a sibling file with `examples_from: examples/comment-why.yml` when they crowd the config out. A built-in rule keeps its examples in the `examples.yml` beside its `rule.yml` under `rules/<id>/`, and `tenet check --builtin` measures every rule that ships in the binary, whatever your config turns on. Examples are never shown to the model and never enter a tenet's hash, so adding one costs you nothing in the lint cache.

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

Then run `tenet check`, which judges every example of every tenet that has any, or `tenet check comment-why` for one of them. The answers are cached under the same cache the lint uses, so a re-run after an edit costs only the examples whose question changed. The AUC in what it prints is the chance the tenet scores a violation above an innocent example, which is what says whether the wording separates them at all.

```
comment-why: sharp
  examples          14 (7 violation, 7 ok)
  auc               1.00
  accuracy          1.00 at fail 0.80 · 1.00 at 0.70, 1.00 at 0.80, 0.71 at 0.90
  mean probability  violation 0.89, ok 0.19, gap 0.71
  location          7 of 7 lines named (1.00)

1 tenet over 14 examples · 0 calls, 14 cached · $0.0000 · 0.0s
```

The accuracy row says how the tenet's own `fail` does and what 0.70, 0.80 and 0.90 would have done with the same examples, which is the whole of what moving it buys.

A tenet is `sharp` when nothing lands on the wrong side of its cutoff, `usable` when the ranking is still good enough to lint with, and `blurry` when it is not; under six examples, reported as `too few examples`, there is nothing worth measuring. Every misjudged example is listed with its probability and its first line, so the next edit to the criteria has something to aim at.

One line of advice names what usually moves the numbers: a lower `fail`, and which value, when the violations cluster just under the cutoff; a higher one when an innocent example reaches it; a `false` criterion when the innocent examples score high, a `true` criterion when the violations score low, and a rewrite of the sentence itself when both sit in the middle.

`check` reports and never fails: it exits 0 whatever the numbers say, and 2 only when the config or the API is broken. `--format json` gives the same numbers for a script, `--min-examples` moves the bar, and `--no-cache` asks again. Unlike the lint, `check` does not split an oversized request: an example longer than one request's token budget comes back as an API error rather than being judged in halves, so keep an example to the piece of code the tenet is about.

`--runs 3` judges every example three times, leaving the cache out of it so the passes are independent, and adds a `stability` line per tenet: the largest standard deviation it saw over any one example, and every example whose probability landed on both sides of the cutoff between passes. The numbers above that line are still the first pass's, so asking for several passes does not change what one of them says. It is what tells you whether a verdict sitting near `fail` is a verdict or a coin toss, and it is the evidence a cutoff of its own should rest on.

Where a label is a call the tenet's sentence does not obviously make, write the reason in the example's `note`; `comment-why` carries a function whose only comment is a `TODO`, labelled `ok` with a note saying that a TODO restates nothing, because the tenet is about a comment that repeats the code. `rules/README.md` is how the built-in rules were built, step by step, and is the recipe to follow for one of your own.

## Adopting on an existing codebase

A first full sweep of code nobody wrote against these tenets finds things nobody is going to fix today, which is no reason to leave the rules off. `tenet baseline .` judges the whole tree, writes what it found to `.tenetlint-baseline.json`, and says how many findings it accepted; commit that file. The hook and CI then pass over every finding it holds and block only the ones your branch adds.

`tenet --show-baselined` lists the accepted ones alongside, marked `[baselined]` and still passing, when you want to see what is waiting, which in JSON is a `baselined` array beside `findings`, of the same shape. `--baseline path` reads a file other than the default one, and `--no-baseline` is a run that honours nothing, which is the sweep to do before a release.

An entry is matched by the file, the tenet and a hash of the offending line with the lines around it, so it survives the code above it moving and is gone the moment the line itself is edited. As the old findings get fixed, `tenet baseline --prune .` rewrites the file with only the entries the run still produces and says how many it dropped; it never accepts anything new. The file records the scope it was written over, and a prune from a narrower one stops rather than drop the entries it never looked at, naming both scopes.

```
$ tenet baseline .
wrote 2 findings to .tenetlint-baseline.json

$ tenet .
0 findings, 2 baselined · 5 windows, 0 calls, 10 cached · $0.0000 · 0.0s
```

## Commit messages

`tenet --commit-msg <file>` lints a commit message rather than code. The message is of kind `commit`, so `kind: [commit]` is how a tenet says it is about the message and nothing else, and it is linted as a file named `COMMIT_EDITMSG`, which an include glob can name instead.

The comment lines git strips itself, and everything below a `>8` scissors line, are gone before the model sees any of it, and the lines that are left keep the numbers your editor showed them under. A finding says `reword the message, which git kept in .git/COMMIT_EDITMSG, then commit again`, and a config with no tenet for the message costs nothing, because there is nothing to ask.

`tenet hook install` writes this as the `commit-msg` hook, which git runs after `pre-commit`, so the code is judged first and the message only once the code passes.

```yaml
  - id: commit-subject
    tenet: The commit subject is in the imperative mood and says what changed for a reader, not which functions were touched.
    kind: [commit]
```

## For agents

Without `--format`, output is text on a terminal and JSON anywhere else, because what reads a pipe is a script or an agent. The JSON carries `findings`, the `next` line that says what to do about them, `stats`, `skipped`, and a `baselined` array when `--show-baselined` asked for one.

```json
{
  "version": 1,
  "findings": [
    {
      "file": "internal/cache/cache.go",
      "line": 11,
      "tenet": "comment-why",
      "probability": 0.94,
      "fail": 0.8,
      "message": "A comment says why the code exists or why it is written this way, not what the code does, what it used to do, or what its declaration already states."
    }
  ],
  "next": "fix the lines above or mark one with a tenet:ignore <id> directive, then commit again",
  "stats": {
    "baselined": 0,
    "files": 1,
    "windows": 1,
    "calls": 2,
    "cache_hits": 0,
    "input_tokens": 2169,
    "cost_usd": 0.00009109799999999999,
    "duration_ms": 760
  },
  "skipped": []
}
```

`next` is the empty string when `findings` is empty, so there is nothing to tell anyone to do; the three directive forms it names are the ones Directives lists. Every path tenetlint prints, including the `file` field of `--format json`, is relative to the directory you ran it from, whatever part of the repository that is.

Reading the report is one thing and knowing to run it is another, so `plugin/` is a Claude Code plugin that does both. It carries a `tenet` skill, which says when to run the lint and what to do with each finding, and a `PreToolUse` hook, which lints the staged changes before a `git commit` and hands the findings back instead of letting the commit through. This repository is its own marketplace:

```
/plugin marketplace add zoidsh/tenetlint
/plugin install tenetlint@tenetlint
```

Agents that read a repository rather than a plugin get the same instructions from `tenet init --agent`. `--agent cursor` writes them to `.cursor/rules/tenet.mdc`, `--agent agents` and `--agent claude` keep them as a `## tenet` section of `AGENTS.md` or `CLAUDE.md`, replacing the section an earlier run wrote rather than adding a second one, and the flag repeats. A run that names an agent writes those files and nothing else, leaving your `tenets.yml` as it is.

```
tenet init --agent cursor --agent agents
```

## Environment variables

- `TYPESAFE_API_KEY` is your key, and every command that asks the model needs it.
- `TYPESAFE_BASE_URL` sends the requests to another host, such as a proxy or a local stand-in.
- `TENETLINT_FORMAT`, `text` or `json`, settles the output format whatever the terminal says.
- `TENETLINT_SKIP=1` makes the installed hooks exit without linting.
- `TENETLINT_INSTALL_DIR` is where the installer script puts the binary, `~/.local/bin` by default.
- `TENETLINT_VERSION` is the release the installer script fetches, `latest` by default, with or without the leading `v`.
- `TENETLINT_BINARY` points the npm wrapper at a binary of your own instead of the one its platform package carries.
- `NO_COLOR` turns the colour off in the text report, whatever the terminal is.

## Development

`CLAUDE.md` in this repository has the toolchain, the test and lint commands, and what to run before a branch is done.

Pre-release: nothing here is stable yet, and the tenet format, the flags and the output may all change without notice.
