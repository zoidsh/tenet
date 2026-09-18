# bench

`bench/results.md` holds every number the top-level README quotes about speed, cost, rule quality and languages. It is generated, never edited: `bench/run.sh` writes it from scratch, and a number that cannot be re-measured is a number nobody can correct.

## What is measured

- Speed and cost of five runs, all at the fixed commit named at the top of `run.sh`: a cold full sweep, the same sweep with the cache warm, one staged commit, a two-thousand-line diff against a fixed base ref, and one pull request title and description. Every duration and cost comes from the `stats` object of `--format json`, not from wall clock, and the binary is built before the first run so no timing carries a compile.

  `COMMIT` is chosen by hand and bumped at a release, because the staged row lints that commit's own change and a commit that touched only data files would measure nothing. The four runs inside the checkout pass this branch's `tenet.yml` with `--config`, so the measured commit needs no config of its own and one from before the rename is still measurable.
- Rule quality, from one `tenet check --builtin --no-cache --runs 3`: each rule's AUC, accuracy at its own cutoff, location score, verdict, how far its examples moved between the three passes, and how many crossed the cutoff. Preset rules come first, then the standalone ones.
- The same rule, comment-why, measured over a German and a Japanese translation of its corpus, beside the English row.
- What one agent call over the same diff costs and takes, as a lower bound, measured rather than estimated. Every model named in `prices.yml` is sent the identical prompt — one instruction line and the whole diff — through one `claude -p` call with no tools, no system prompt and no settings, and the run reads the token counts and the wall time out of the CLI's own JSON. Cost is those counts at the list prices in `prices.yml`, not the figure the CLI reports.

## Running it

From the repository root, with a key configured (`tenet auth`, or `TYPESAFE_API_KEY` in the environment) and the `claude` CLI installed and logged in, since the agent rows are real calls and they spend on whichever account that CLI is logged into:

    bench/run.sh

It refuses to run with uncommitted changes, because it measures a fixed commit, so commit the previous `bench/results.md` first. It checks that commit out into a throwaway git worktree, points `XDG_CACHE_HOME` at a fresh directory so the cold run is cold and the developer's cache is left alone, and removes both at exit. That isolation is Linux only, because `os.UserCacheDir` reads `XDG_CACHE_HOME` there and `~/Library/Caches` on macOS, where the warm sweep therefore reads and fills the developer's own cache; the cold sweep is still cold everywhere, because `--no-cache` and not the directory is what makes it so.

It costs real money and takes minutes: the full sweep judges the whole tree, `check --builtin --runs 3` judges every rule's corpus three times, and the agent comparison sends the whole diff to each model in `prices.yml`.

To change the script without paying for the tenet runs, point it at the stub, which answers from canned JSON in the shapes `internal/report` and `internal/check` write. The stub does not stand in for the agent rows: those are still four real `claude -p` calls on every run.

    TENET_BIN=bench/testdata/stub-tenet bench/run.sh

The numbers a stub run writes are invented. Throw that `results.md` away and regenerate it with the real binary before committing.

## The files

- `run.sh` generates `results.md`.
- `prices.yml` is the only file here that is edited by hand: the id, the display name and the input and output per-million prices of each model the comparison calls, read from the vendor's own pricing page, with its source and the date it was read. It is also the list of models the run calls and the order their rows appear in, so adding an entry adds a call and its cost.
- `pr.txt` and `pr-tenet.yml` are the pull request text the fifth row lints and the config that judges it.
- `lang/comment-why.de.yml` and `lang/comment-why.ja.yml` are the translated corpora. They must stay in step with `rules/comment-why`: an example added or relabelled there belongs in both, or the language table compares different corpora.
- `testdata/stub-tenet` is the stub for a dry run.
