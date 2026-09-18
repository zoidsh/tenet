# tenet

**Your agents read AGENTS.md. Then they ignore it. tenet makes them fix it before you ever see the diff.**

[![CI](https://github.com/zoidsh/tenet/actions/workflows/ci.yml/badge.svg)](https://github.com/zoidsh/tenet/actions/workflows/ci.yml)

*Pre-release: the tenet format, the flags and the output are not stable, and any release can change them, so pin the version you install with `TENET_VERSION`.*

tenet is the review gate for code that agents write. Your AGENTS.md says what a comment is for and that a failure is raised rather than hidden; agents break those rules anyway, and nobody reads every line of a large diff. You write each tenet once in plain language in `tenet.yml` and every commit is judged against it, fast enough that the agent fixes its own findings before you see the diff.

TypeSafe's jev model answers each rule with a calibrated probability, so a tenet is a pass-or-fail cutoff rather than a review comment to skim. On this repository a staged change took 1.4 s and cost $0.0022; a 2,062-line diff took 1.5 s and $0.0036, about 8x faster than one Claude Haiku 4.5 call and about 12x faster than Sonnet 5, and cheaper than both. The tables are under Benchmarks.

## Install

Homebrew, on macOS and on Linux:

```sh
brew install zoidsh/tap/tenet
```

npm, which carries the binary for your platform as an optional dependency and runs no install script:

```sh
npm install -g @zoidsh/tenet
npx @zoidsh/tenet
```

As a devDependency, so everyone working on the repository gets the same tenet:

```sh
npm install --save-dev @zoidsh/tenet
```

The installer script, which puts the binary in `~/.local/bin` and never asks for sudo:

```sh
curl -fsSL https://raw.githubusercontent.com/zoidsh/tenet/main/install.sh | sh
```

From source, which needs a Go toolchain:

```sh
go install github.com/zoidsh/tenet/cmd/tenet@latest
```

Every release tarball, and the `checksums.txt` that covers them, is on [GitHub Releases](https://github.com/zoidsh/tenet/releases).

To have every commit or every pull request linted for you, see pre-commit and GitHub Actions under Commit messages and pull requests.

## Quick start

From the root of your repository:

1. Save the key your lints are judged with, which comes from a TypeSafe account at [typesafe.ai](https://typesafe.ai).

   ```sh
   tenet auth
   ```

   Type the key at the prompt; it is kept in `~/.config/tenet/credentials`.

   From here an agent can do the rest. Install the Claude Code plugin: `/plugin marketplace add zoidsh/tenet` then `/plugin install tenet@zoidsh`; for another agent, `tenet init --agent agents` (or `cursor`, `claude`) gives the same instructions. Then tell it to set tenet up: it runs `tenet init`, maps the drafted rules to built-in ones, installs the hooks, and calibrates the tenets that are yours alone, as For agents describes. Steps 2 to 5 are that same work done by hand.

2. Draft a `tenet.yml` from the instruction files your agents already read, or turn on a preset from Built-in rules below.

   ```sh
   tenet init
   ```

3. Read what it drafted, delete the rules you did not mean, and print what the file now resolves to.

   ```sh
   tenet config
   ```

4. Lint your staged changes.

   ```sh
   tenet
   ```

   `tenet --base main` lints the working tree against that git ref instead, and naming paths lints those files whether or not they are staged.

5. Install the hooks, so every commit is linted from here on; `tenet hook uninstall` takes them away again.

   ```sh
   tenet hook install
   ```

`tenet init` splits each instruction file into sentences and list items, then asks jev what kind of instruction each one is and whether a diff alone settles it. It keeps the rules a diff is enough to judge, and reports what describes your project rather than instructing anyone. A rule phrased as an instruction to the agent, such as "never print the key", is kept when the thing it forbids would be visible in the changed lines.

Without `--from` it reads every one of `AGENTS.md`, `CLAUDE.md`, `.cursorrules`, `.cursor/rules/*.mdc`, `.github/copilot-instructions.md`, `.github/instructions/*.instructions.md` and `BUGBOT.md` that your repository has; with `--from path` it reads exactly the files you name, and the flag is repeatable. `--dry-run` prints the table without writing anything, `--force` replaces a `tenet.yml` that is already there, `--config path` writes somewhere other than the repository root, and `--format json` gives you every candidate with its probabilities.

In the table, `kind` is what jev took the sentence for (a rule about code, a process step, or context about the project) and `checkable p` is its confidence that a diff alone can settle it; only a checkable rule becomes a tenet.

```text
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

## Built-in rules

Twenty-five rules ship inside the binary, each with the criteria that say what a violation looks like and a labelled corpus measured by `tenet check --builtin --no-cache --runs 3`. A preset is a named list of them, and one line of `tenet.yml` turns the list on.

```console
$ tenet presets
agent-hygiene  The habits a coding agent slips into when nobody reads the diff.
  comment-why, no-mocking, no-transcript-comment, no-placeholder-phrase, assertion-justified, no-fallback

pr  The title and description a pull request arrives with when only the diff was thought about.
  subject-says-what-changed, body-says-why, body-states-door, body-few-visuals

unslop-prose  The prose an LLM writes into a README when nobody rewrites the draft.
  project-specific, no-generic-conclusion, no-metaphor-noun, no-false-contrast
```

`agent-hygiene`, the habits a coding agent slips into when nobody reads the diff:

- `comment-why`, a comment says why rather than what the code does
- `no-mocking`, tests use the real dependency
- `no-transcript-comment`, no comment about the request that prompted the change
- `no-placeholder-phrase`, nothing ships marked temporary or simplified
- `assertion-justified`, a cast names the invariant that makes it safe
- `no-fallback`, an actionable error rather than a silent degradation path

`unslop-prose`, the prose an LLM writes into a README when nobody rewrites the draft:

- `project-specific`, a claim names the mechanism or a number
- `no-generic-conclusion`, no "the future looks bright" ending
- `no-metaphor-noun`, the plain word rather than substrate or flywheel
- `no-false-contrast`, no "not just X, but Y"

`pr`, the title and description a pull request arrives with when only the diff was thought about:

- `subject-says-what-changed`, the title says what is different, not which files were touched
- `body-says-why`, the description gives a reason the diff does not show
- `body-states-door`, it says how far the change can be walked back
- `body-few-visuals`, one or two visuals, each beside the text it supports

Eleven of the twenty-five are standalone. They ship in the binary, and `tenet rules` lists every rule with its tags and the preset that includes it. They are in no preset because their corpus does not read sharp in every run at the 0.8 `fail` cutoff, defined under Pass or fail. The comment at the top of each preset file says what each one is short of, example by example, so you can decide whether it is short of anything you care about. `rules: [no-defensive-nil]` turns one on.

`tenet init` names agent-hygiene when it finds no instruction file to read, and Configuration below says how presets, rules and your own tenets compose.

## Pass or fail

Every rule and tenet has one cutoff, `fail`, 0.8 unless it says otherwise. A window is a slice of one file small enough to ask the model about in a single call, at most 254 lines of it. The model answers each window with a probability, and at or above the cutoff it is a finding, under it nothing at all. Any finding exits 1; a clean run exits 0 and a broken one exits 2.

There is no severity, no warning tier and no flag that lets a finding through, because a rule that is not worth failing a commit over is a rule whose cutoff is in the wrong place.

`tenet check` is where you find that place: it measures a tenet against examples you have labelled and tells you what each cutoff would cost you, and `fail` in the tenet or in an `override` (see Configuration) is where you write the answer down. `--verbose` lists the near misses, every tenet that came within 0.2 under its cutoff on a window.

```text
internal/cache/cache.go:11: comment-why (p=0.94)
internal/judge/judge.go:15: no-fallback (p=0.96)

comment-why  A comment says why the code exists or why it is written this way, not what the code does, what it used to do, or what its declaration already states.
no-fallback  Do not add fallbacks, default-to-something-that-works-ish behavior, or silent degradation paths. Either the operation succeeds as intended, or it raises an actionable error.

2 findings · 2 windows, 2 calls, 5 cached · $0.0001 · 0.8s
fix the lines above, then commit again
```

## Directives

Three directives exempt code from a tenet. `tenet:ignore` exempts the line it is written on, `tenet:ignore-next-line` the line below it, and `tenet:ignore-file` the whole file, wherever in that file you put it; the first line is the usual place, but it is not a rule.

Each takes an optional comma-separated list of tenet ids and exempts only those; with no list it exempts every tenet. Ids are joined by commas; the first word after the list begins a reason, as in `tenet:ignore no-fallback the vendor API returns 200 on failure`. A directive with no ids has nowhere for a reason, so put it in a comment of its own.

A directive that names an id your `tenet.yml` does not define, or a `tenet:ignore-` keyword that is not one of the three, fails the run rather than silently exempting nothing, because a typo you cannot see is worse than a run you have to fix.

```go
x := fallback() // tenet:ignore no-fallback

// tenet:ignore-next-line comment-why
y := 1 // set y to one
```

A directive only counts inside a comment, so a string, a test fixture or a sentence about directives does not quietly exempt the file it sits in. In code that means after a line comment marker, or between a block comment's markers, in the language the file's extension names; in a language that tenet does not know it counts anywhere on the line. The check is textual rather than a parse, so a marker inside a string literal opens a comment as far as tenet is concerned and a directive after it counts.

A directive is yours to write, not your agent's. It records a false positive you have read and confirmed, so the instructions the plugin and `tenet init --agent` put in front of an agent tell it never to write one. A finding it believes is wrong comes back to you with the line, the tenet's sentence and why it thinks the rule misfired. You then sharpen the tenet's criteria, write the directive yourself with the reason after the id, or tell it the finding was right and the code is what changes.

In Markdown, YAML and other prose and data files a directive counts at the start of a line, after list markers, whitespace or the format's own comment marker, or inside an `<!-- -->` comment; a sentence that quotes one mid-line does not count. In a commit message, and in a pull request's title and description, it counts anywhere. The directive and its id list are cut out of the line before anything is sent to the model, and the line numbers you are shown are the ones in your file. A mention that does not count is left where it is.

## Configuration

A rule ships in the binary; a tenet is one you write in `tenet.yml`; both run the same way, and a tenet that carries a rule's id replaces it.

A `tenet.yml` composes what will run out of the rules that ship inside the binary, listed under Built-in rules above, and the ones you write yourself. `tenet rules comment-why` prints one built-in rule in full, criteria and all, and a rule may sit in several presets.

```yaml
version: 1
provider: typesafe              # who judges, and whose key is read; typesafe is the default
presets: [agent-hygiene]        # built-in presets, expanded in order
rules: [no-defensive-nil]       # individual built-in rules, added after presets
disable: [no-mocking]           # removed after expansion, by id
override:                       # per-id patches applied last
  no-defensive-nil:
    fail: 0.9
    include: ["**/*.go"]
    kind: [code]                # code, prose, data, commit or pr; the globs narrow further
tenets: [...]                   # your own tenets, written out in full
```

A tenet's `kind` is how it says what it is about. Every file is code, prose or data, read off its name:

- prose: `.md`, `.rst`, `.txt` and the like, everything under `locales/` and `i18n/` whatever it is serialised as, and a README, CHANGELOG, CONTRIBUTING or LICENSE.
- data: `.json`, `.yml`, `.toml`, `.csv` and lock files.
- anything else under `docs/` is prose, so a `docs/api.json` stays data.
- everything else is code.

A commit message is a kind of its own, `commit`, read off the literal name `COMMIT_EDITMSG` it is linted under, and a pull request's title and description are another, `pr`, under the name `PULL_REQUEST`. The model is told which it is looking at, so a document is judged as a document rather than as source code, and a tenet that names a kind is asked only about files of that kind, on top of its include and exclude globs. A tenet that names none is asked about everything its globs match.

The order is the order of that file: the presets in the order you list them, then the rules, then your own tenets, then the disables, then the overrides. A tenet of your own that carries a built-in id replaces that rule wholesale, where it stood, so moving a rule into your config to reword it does not reorder the report. An override patches only the fields it names and leaves the rest of the rule alone.

An id that arrives twice, an unknown preset, rule, disable or override id, and a config that resolves to no tenets at all are each an error that names what it found. `tenet config`, from Quick start step 3, prints what your file resolves to, with the origin, kinds, cutoff and include globs of every tenet that will run.

```console
$ tenet config --config tenet.yml
tenet.yml

id                     origin         kind  fail  include
comment-why            agent-hygiene  code  0.80  **/*.go, **/*.ts, **/*.tsx, **/*.py
no-mocking             agent-hygiene  code  0.80  **/*_test.go, **/*.test.ts, **/*.test.tsx, **/*.spec.ts, **/*.spec.tsx, **/test_*.py, **/*_test.py
no-transcript-comment  agent-hygiene  code  0.80  **/*.go, **/*.ts, **/*.tsx, **/*.py
no-placeholder-phrase  agent-hygiene  code  0.80  **/*.go, **/*.ts, **/*.tsx, **/*.py
assertion-justified    agent-hygiene  code  0.80  **/*.go, **/*.ts, **/*.tsx, **/*.py
no-fallback            agent-hygiene  code  0.80  **/*.go, **/*.ts, **/*.tsx, **/*.py
no-defensive-nil       rules          code  0.80  **/*.go, **/*.ts, **/*.tsx, **/*.py
```

`tenet init --preset agent-hygiene` writes a config that names that preset and nothing else, which is also what `init` writes when it finds no instruction file to read; add `--from`, from Quick start, to draft your own rules into the same file underneath it.

A tenet is judged by its sentence alone unless you give it criteria: a `true` description of what a violation looks like and a `false` description of what an innocent change looks like. `init` drafts no criteria, because they are the one part of a tenet the model cannot guess at, and the part that most changes what the model answers. Add them to any tenet the lint gets wrong, in the words you would use to explain the call to a new reviewer. A tenet need not be in English: the Languages table under Benchmarks measures comment-why in German and in Japanese beside its English corpus.

```yaml
tenets:
  - id: comment-why
    tenet: A comment says why the code exists or why it is written this way, not what the code does.
    criteria:
      true: A comment that restates what the code visibly does, narrates steps, or is a section label.
      false: The comment gives a reason, a constraint, a contract, or a warning about ordering that the code does not show.
```

## Checking a tenet

`tenet check` tells you whether a tenet is phrased well enough to lint with, by running it over examples you have labelled yourself. Give a tenet an `examples` list, each with a `label` of `violation` or `ok` and the `code` it is about. A violation also names the `lines` a finding should land on: one line, or a `[first, last]` pair when the violation spans several and naming any line of it is right.

Keep them in a sibling file with `examples_from: examples/comment-why.yml` when they crowd the config out. A built-in rule keeps its examples in the `examples.yml` beside its `rule.yml` under `rules/<id>/`, and `tenet check --builtin` measures every rule that ships in the binary, whatever your config turns on. Examples are never shown to the model and never enter a tenet's hash, so adding one costs you nothing in the lint cache.

```yaml
tenets:
  - id: comment-why
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

```text
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

One line of advice names what usually moves the numbers:

- a lower `fail`, and which value, when the violations cluster just under the cutoff;
- a higher one when an innocent example reaches it;
- a `false` criterion when the innocent examples score high;
- a `true` criterion when the violations score low;
- a rewrite of the sentence itself when both sit in the middle.

`check` reports and never fails: it exits 0 whatever the numbers say, and 2 only when the config or the API is broken. `--format json` gives the same numbers for a script, `--min-examples` moves the bar, and `--no-cache` asks again. `--no-cache` bypasses the cached answers it reads, never the ones it writes, so the run after it is warm rather than cold. Unlike the lint, `check` does not split an oversized request: an example longer than one request's token budget comes back as an API error rather than being judged in halves, so keep an example to the piece of code the tenet is about.

`--runs 3` judges every example three times, leaving the cache out of it so the passes are independent. It adds a `stability` line per tenet: the largest standard deviation it saw over any one example, and every example whose probability landed on both sides of the cutoff between passes. The numbers above that line are still the first pass's, so asking for several passes does not change what one of them says. It is what tells you whether a verdict sitting near `fail` is a verdict or a coin toss, and it is the evidence a cutoff of its own should rest on.

Where a label is a call the tenet's sentence does not obviously make, write the reason in the example's `note`. `comment-why` carries a function whose only comment is a `TODO`, labelled `ok` with a note saying that a TODO restates nothing, because the tenet is about a comment that repeats the code. `rules/README.md` is how the built-in rules were built, step by step, and is the recipe to follow for one of your own.

## Adopting on an existing codebase

A first full sweep of code nobody wrote against these tenets finds things nobody is going to fix today, which is no reason to leave the rules off. `tenet baseline .` judges the whole tree, writes what it found to `.tenet-baseline.json`, and says how many findings it accepted; commit that file. The hook and CI then pass over every finding it holds and block only the ones your branch adds.

`tenet --show-baselined` lists the accepted ones alongside, marked `[baselined]` and still passing, when you want to see what is waiting, which in JSON is a `baselined` array beside `findings`, of the same shape. `--baseline path` reads a file other than the default one, and `--no-baseline` accepts nothing from the baseline, which is the sweep to do before a release.

An entry is matched by the file, the tenet and a hash of the offending line with the lines around it, so it survives the code above it moving and is gone the moment the line itself is edited. As the old findings get fixed, `tenet baseline --prune .` rewrites the file with only the entries the run still produces and says how many it dropped; it never accepts anything new. The file records the scope it was written over, and a prune from a narrower one stops rather than drop the entries it never looked at, naming both scopes.

```console
$ tenet baseline .
wrote 2 findings to .tenet-baseline.json

$ tenet .
0 findings, 2 baselined · 5 windows, 0 calls, 10 cached · $0.0000 · 0.0s
```

## Commit messages and pull requests

`tenet --commit-msg <file>` lints a commit message rather than code. The message is of kind `commit`, so `kind: [commit]` is how a tenet says it is about the message and nothing else, and it is linted as a file named `COMMIT_EDITMSG`, which an include glob can name instead.

The comment lines git strips itself, and everything below a `>8` scissors line, are gone before the model sees any of it, and the lines that are left keep the numbers your editor showed them under. A finding says `reword the message, which git kept in .git/COMMIT_EDITMSG, then commit again`, and a config with no tenet for the message costs nothing, because there is nothing to ask.

The hooks from Quick start step 5 include this one, the `commit-msg` hook, which git runs after `pre-commit`, so the code is judged first and the message only once the code passes.

```yaml
tenets:
  - id: commit-subject
    tenet: The commit subject is in the imperative mood and says what changed for a reader, not which functions were touched.
    kind: [commit]
```

`tenet --pr-text <file>` lints a pull request's title and description, which the file holds one after the other, as a file named `PULL_REQUEST` of kind `pr`. Nothing is stripped, because a description is prose rather than a file with comments in it, and a finding says `edit the pull request title or description, then push again`. The action writes the title and the description of the pull request it is running on and lints them itself, so this flag is for running the same rule anywhere else. A rule about what a change is called is usually about both texts, and one tenet can cover them:

```yaml
tenets:
  - id: says-what-changed
    tenet: The subject or title says what changed for a reader, not which functions were touched, and the body says why.
    kind: [commit, pr]
```

### pre-commit

The [pre-commit](https://pre-commit.com) framework builds tenet from source itself, fetching a Go toolchain of its own if the machine has none:

```yaml
repos:
  - repo: https://github.com/zoidsh/tenet
    rev: main
    hooks:
      - id: tenet
      - id: tenet-commit-msg
```

### GitHub Actions

The action downloads the release binary for the runner and lints the pull request against its base:

```yaml
steps:
      - uses: actions/checkout@v5
        with:
          fetch-depth: 0

      - uses: zoidsh/tenet@v0.1.0
        with:
          api-key: ${{ secrets.TYPESAFE_API_KEY }}
```

The version is pinned to a release. The release workflow moves a `v1` tag only on a 1.x release, so `@v1` works once 1.0 is out. Each finding is annotated on the line of the diff it was raised on. `PULL_REQUEST` is no file in the diff, so a finding about the title or the description is in the check run's annotation list rather than against a line. The paths are relative to the repository root, which is where `actions/checkout` puts it unless you gave it a `path` of its own. `annotate: false` turns the annotations off, and the step then prints one JSON document per lint, two of them on a pull request.

## For agents

Without `--format`, output is text on a terminal and JSON anywhere else, because what reads a pipe is a script or an agent. That applies to the three commands that report on a run, `tenet` itself, `check` and `init`; `config`, `baseline`, `hook`, `rules` and `presets` print text wherever they are pointed, because what they print is a listing rather than a result. The JSON carries `findings`, the `next` line that says what to do about them, `stats`, `skipped`, and a `baselined` array when `--show-baselined` asked for one.

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
  "next": "fix the lines above, then commit again",
  "stats": {
    "baselined": 0,
    "files": 1,
    "windows": 1,
    "calls": 2,
    "cache_hits": 0,
    "input_tokens": 2169,
    "cost_usd": 0.000091,
    "duration_ms": 760
  },
  "skipped": []
}
```

`next` is the empty string when `findings` is empty, so there is nothing to tell anyone to do; it names no directive, for the reason under Directives. Every path tenet prints, including the `file` field of `--format json`, is relative to the directory you ran it from, whatever part of the repository that is. The exception is `--format github`, one `::error` workflow command per finding, whose paths are relative to the repository root because that is what GitHub resolves an annotation against.

A run with no key exits 2 saying `no TypeSafe API key: run tenet auth, or set TYPESAFE_API_KEY; set TENET_SKIP=1 to commit without linting`, which is a broken run rather than a clean one. The key is the person's to enter at that prompt, never something for an agent to read, write into a file or put in a commit.

Reading the report is one thing and knowing to run it is another, so `plugin/` is a Claude Code plugin that does both. It carries a `tenet` skill, which says when to run the lint and what to do with each finding, and a `PreToolUse` hook, which lints the staged changes before a `git commit` and hands the findings back instead of letting the commit through. This repository is its own marketplace:

```text
/plugin marketplace add zoidsh/tenet
/plugin install tenet@zoidsh
```

Agents that read a repository rather than a plugin get the same instructions from `tenet init --agent`. `--agent cursor` writes them to `.cursor/rules/tenet.mdc`, `--agent agents` and `--agent claude` keep them as a `## tenet` section of `AGENTS.md` or `CLAUDE.md`, replacing the section an earlier run wrote rather than adding a second one, and the flag repeats. A run that names an agent writes those files and nothing else, so it refuses `--from`, `--preset`, `--config` and `--force` rather than half-doing two jobs, and `--dry-run` names the files it would write.

```sh
tenet init --agent cursor --agent agents
```

Setting tenet up is one instruction to the agent, as Quick start says. Two steps stay with you: installing the binary, and `tenet auth`, because the key is yours to paste.

The agent checks `tenet auth --status`, runs `tenet init`, and replaces every drafted rule that a built-in rule already covers with that rule's id, keeping the source line in a comment. It runs `tenet hook install`, so the built-in rules gate the next commit, and everything up to there takes under two minutes. Each remaining custom tenet it then calibrates by the recipe named under Checking a tenet: twelve labelled examples in `examples/<id>.yml`, `tenet check <id> --runs 3`, and criteria edited while the tenet sentence stays as written. Adding a rule later runs the same flow for that rule alone.

## Comparison

This is about fit rather than speed; the timings are under Benchmarks.

The static linters are eslint, ruff and golangci-lint, the prose linter is Vale, and the review bots are CodeRabbit, Copilot code review and Cursor Bugbot.

| | Static linters | Vale | Review bots | An agent | tenet |
| --- | --- | --- | --- | --- | --- |
| Rules in plain language | no, rules are code | word lists, regex | yes | yes | yes |
| Every commit, locally | yes | yes | mostly no, PR bots | a call each time | yes, over the network |
| Judges a diff without running the code | type-aware rules need a build | yes | yes | yes | yes |
| Findings on a line | yes | yes | yes | if you wire it up | yes |
| Calibrated pass or fail | yes, exact match | yes, exact match | no, free text | no, free text | yes |
| Prose, commit and PR text | no | prose files only | PR text at most | yes | yes |
| Finds logic bugs | no | no | yes, uncalibrated | yes, uncalibrated | no |
| Sees the rest of the repo | package scope only | no | yes | yes | no |
| Posts review comments | if you wire it up | if you wire it up | yes | if you wire it up | no |

tenet's three no's are the same decision three times: it judges the changed windows against the rule you wrote, never the program's behaviour and never the rest of the tree. So a logic bug, and a contract broken between two files that each look fine, are still a reviewer's job. It has no way to post a comment either, and the GitHub Action's annotations are annotations rather than review comments.

### What to use for what

- A static linter settles what a parser can settle: syntax, types, unused code, the rules whose answer is in the grammar.
- Vale, or a `grep -nE` in the same hook, settles the mechanical prose checks the presets hand off on purpose, because a regex decides them and a model should not be paid to: em dashes, emoji, title case, commit prefixes and the like.
- A review bot or an agent takes the reading that needs the whole program: logic, contracts across files, and whether the design is the right one.
- tenet takes the rules you wrote in plain language that a diff is enough to judge, and runs them on every commit rather than once a pull request is open.

## Benchmarks

Every number in this README comes from [bench/results.md](bench/results.md), which `bench/run.sh` generates at a fixed commit, dated in the file itself, and which nobody edits by hand. [bench/README.md](bench/README.md) says what it measures and how to regenerate it.

| Run | Lines or scope | Calls | Cost | Duration |
| --- | --- | --- | --- | --- |
| Full sweep, cold cache | the whole tree | 122 | $0.0183 | 5.2 s |
| Full sweep, warm cache | the same tree, straight after | 0 | $0.0000 | 0.0 s |
| One staged change | 379 lines staged | 13 | $0.0022 | 1.4 s |
| A 2,062-line diff | 2,105 lines, 2,062 of them Go, 83,295 bytes of diff | 21 | $0.0036 | 1.5 s |
| One pull request text | 27 lines of title and description | 1 | <$0.0001 | 1.0 s |

Rule quality is measured the same way, by `tenet check --builtin --no-cache --runs 3` over every rule in the binary. In that run all fourteen rules in a preset read `sharp`, and of the eleven standalone rules ten read `usable` and one reads `sharp`. bench/results.md has it rule by rule.

The same rule, translated, with its examples judged three times each at the 0.80 cutoff. Location is the share of violations whose named line was hit, and Crossings counts examples that landed on both sides of the cutoff across the three passes.

| Language | Examples | AUC | Accuracy at 0.80 | Location | Largest sd | Crossings | Verdict |
| --- | --- | --- | --- | --- | --- | --- | --- |
| English | 14 | 1.00 | 1.00 | 1.00 over 7 | 0.015 | 0 | sharp |
| German | 14 | 1.00 | 0.93 | 1.00 over 7 | 0.015 | 0 | usable |
| Japanese | 14 | 1.00 | 0.79 | 1.00 over 7 | 0.023 | 0 | usable |

And the 2,062-line diff against what one agent call over the same diff would cost and take:

| Reviewer | Input tokens | Output tokens | Cost | Time |
| --- | --- | --- | --- | --- |
| tenet, measured | 86,343 | n/a | $0.0036 | 1.5 s |
| Claude Haiku 4.5 | 20,823 | 1,000 | $0.0258 | 12.7 s |
| Claude Sonnet 5 | 20,823 | 1,000 | $0.0516 | 17.5 s |
| GPT-5 nano | 20,823 | 1,000 | $0.0014 | n/a |

GPT-5 nano's estimate is the one that undercuts tenet, at $0.0014 against $0.0036. Every agent row is a lower bound: one call, the whole diff in the prompt, no tool use, no reading the rest of the repository and no second pass. bench/results.md states the prices, the rates and where each came from.

## Environment variables

Every one of these can be settled for a single run by a flag, which outranks the variable. The flags are on `tenet` itself and on every subcommand.

- `TYPESAFE_API_KEY` is your TypeSafe key, and every command that asks the model needs a key from somewhere. It is read first, then `.tenet/credentials` in the repository, then `~/.config/tenet/credentials`, so exporting it in CI overrides whatever is saved on the machine. `tenet auth --project` saves it to `.tenet/credentials` rather than your home directory and adds that file to `.gitignore`, `tenet auth --status` says which of the three a run is reading, and `tenet auth typesafe` names the provider outright.
- `--typesafe-api-key` outranks all three, though a saved key or the variable is better: a flag is in the process list for anyone on the machine to read.
- `TYPESAFE_BASE_URL`, or `--typesafe-base-url`, sends the requests to another host, such as a proxy or a local stand-in.
- `TENET_FORMAT`, `text` or `json`, settles the output format whatever the terminal says.
- `TENET_SKIP=1` makes the installed hooks exit without linting.
- `TENET_INSTALL_DIR` is where the installer script puts the binary, `~/.local/bin` by default.
- `TENET_VERSION` is the release the installer script fetches, `latest` by default, with or without the leading `v`.
- `TENET_BINARY` points the npm wrapper at a binary of your own instead of the one its platform package carries.
- `NO_COLOR` turns the colour off in the text report, whatever the terminal is. `--color` settles it outright: `auto`, the default, reads the terminal and `NO_COLOR`, while `always` and `never` say so.

## Development

`CLAUDE.md` in this repository has the toolchain, the test and lint commands, and what to run before a branch is done.

## Acknowledgements

The built-in rules quote sentences other people wrote for their own repositories, because a rule nobody has lived with reads like one. Each rule's `source` field says where its sentence came from.

- [cursor/plugins](https://github.com/cursor/plugins/blob/HEAD/pstack/skills/unslop/SKILL.md), the pstack unslop skill, for nine prose rules: no-false-contrast, no-false-range, no-connector-colon, inline-header-detail, no-hedging, no-generic-conclusion, no-metaphor-noun, project-specific and active-voice, and for the chatbot phrases in no-throat-clearing.
- [mattpocock/skills](https://github.com/mattpocock/skills/blob/HEAD/skills/in-progress/pr/SKILL.md), the pr skill, for five pull request rules: body-says-why, body-states-door, body-few-visuals, body-names-blast-radius and body-shows-evidence.
- [maxgoff/unslop](https://github.com/maxgoff/unslop/blob/HEAD/skills/unslop/SKILL.md) for no-throat-clearing, and for the commit rules subject-says-what-changed was drafted from.
- [hardikpandya/stop-slop](https://github.com/hardikpandya/stop-slop/blob/HEAD/SKILL.md) for concrete-subject.
- [dmmulroy/anti-slop](https://github.com/dmmulroy/anti-slop/blob/HEAD/src/rules/require-safety-comment-for-type-assertion.ts) for assertion-justified.
- The AGENTS.md of [vitest-dev/vitest](https://github.com/vitest-dev/vitest/blob/HEAD/AGENTS.md) for no-mocking.
- The CLAUDE.md of [oven-sh/bun](https://github.com/oven-sh/bun/blob/HEAD/src/CLAUDE.md) for no-transcript-comment.
- The CLAUDE.md of [amd/gaia](https://github.com/amd/gaia/blob/HEAD/CLAUDE.md) for no-fallback.
- The CLAUDE.md of [Kaikei-e/Alt](https://github.com/Kaikei-e/Alt/blob/HEAD/CLAUDE.md) for no-defensive-nil.
- The author's own CLAUDE.md for comment-why.

Every judgement a lint makes comes from [TypeSafe](https://typesafe.ai)'s jev model. The quoted skills and instruction files stay under their own licences: the four skills are MIT, as are the vitest-dev/vitest and amd/gaia files, and Kaikei-e/Alt is Apache-2.0.
