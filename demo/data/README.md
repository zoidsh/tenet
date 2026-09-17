# The numbers the demo page animates

Six runs against the live jev API, captured on 2026-09-18 over the sample project in `demo/sample` and its own `tenet.yml`, which turns on the `agent-hygiene` and `pr` presets and adds the `money-in-cents` tenet. Every number, id, path and line in `demo/index.html` comes from these files.

The two code runs judge a staged change: a fresh clone of `demo/sample` (the `*.patch` and `*.txt` files left out), `git init`, one commit of the sample as it stands, then `git apply` and `git add -A`. `XDG_CACHE_HOME` pointed at an empty directory, so every run below is cold.

```sh
git apply agent-change.patch && git add -A
tenet --format json --no-cache                             # blocked.json
git apply agent-fix.patch && git add -A
tenet --format json                                        # passed.json
tenet --commit-msg commit-msg-bad.txt  --format json --no-cache   # commit-blocked.json
tenet --commit-msg commit-msg-good.txt --format json --no-cache   # commit-passed.json
tenet --pr-text pr-bad.txt  --format json --no-cache              # pr-blocked.json
tenet --pr-text pr-good.txt --format json --no-cache              # pr-passed.json
```

What they found:

- `blocked.json`: `no-fallback` 0.94 on billing.go:28, `money-in-cents` 0.95 on :34, `comment-why` 0.88 on :36, `no-mocking` 0.90 on billing_test.go:36. 893 ms, $0.000318.
- `passed.json`: nothing. 795 ms, $0.000190. Its `cache_hits` is 0 because the fix rewrote both windows, and a window's answer is cached against its content.
- `commit-blocked.json`: `subject-says-what-changed` 0.96 on COMMIT_EDITMSG:1. 781 ms, $0.000037. `commit-passed.json`: nothing, 545 ms, $0.000024.
- `pr-blocked.json`: `subject-says-what-changed` 0.94 on PULL_REQUEST:1, `body-says-why` 0.94 on :5, `body-few-visuals` 0.97 on :10, `body-states-door` 0.88 on :13. 726 ms, $0.000119. `pr-passed.json`: nothing, 554 ms, $0.000073.

`cutoff-sweep.txt` is where the probabilities under 0.80 come from, the ones the bars fall to when a run passes. Every window is asked about every tenet whose globs and kind match it, but a run prints a probability only for a finding or for a near miss within 0.2 of the cutoff. Judging the same six runs again through `below-cutoff.yml`, a copy of the sample's config with every cutoff moved, puts each remaining answer inside that band and prints it:

```sh
sed 's/{fail: 0.01}/{fail: 0.2}/' below-cutoff.yml > sweep.yml   # then 0.4, 0.6, 0.8
tenet --config sweep.yml --verbose                               # near misses on stderr
```

It costs nothing and asks nothing again: the cache is keyed by the question rather than by the cutoff, so the sweep reads the answers the six runs above already paid for.

The sample's own code is clean before the patch is applied: `tenet .` over it reports 0 findings across 6 windows, and `go vet` and `go test` pass on it and on the fixed state.
