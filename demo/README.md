# demo

`index.html` plays a 21.7-second animation of one commit meeting the hook, for a screen recorder to capture. It runs once on load; space or a click replays it.

## What the page shows

The left pane is the staged diff of `sample/agent-change.patch`, a discount feature an agent wrote for the invoice service in `sample/`: a float for money, a `percent = 0` fallback on a parse error, two comments that narrate the arithmetic, and a test that stands a `stubRepository` in for the real store. The right pane is one bar per tenet the lint ran, the six rules of the `agent-hygiene` preset and the `money-in-cents` tenet the sample's `tenet.yml` writes out, each filling to the probability jev answered with against a single mark at the 0.80 cutoff. Four bars cross it, turn red and pick up the file and line of their finding, the diff lines they name light up, and the status reads `commit blocked · 4 findings`.

Then the diff crossfades to `sample/agent-fix.patch`, the bars re-animate to the second run's probabilities, all of them under the cutoff, and the status reads `commit passed · 0 findings`. The footer carries each run's own round trip, input tokens, calls, windows, cache hits and cost.

jev answers with the tenets it raises, so a tenet it was quiet about has no probability to draw: `no-placeholder-phrase` and `assertion-justified` sit at zero in both runs, labelled `no answer`, and `no-transcript-comment` has a number only in the second.

## The numbers

Every id, path, line number and figure in the page is one measured run against the live API, captured on 2026-09-18 and committed under `data/`. `data/README.md` says which command produced each file. The blocked run took 1.5 s over 2 windows and cost $0.00032; the passing run took 0.7 s and cost $0.00019, with no cache hit, because the fix rewrote both windows and a window's answer is cached against its content.

To measure it again, in a throwaway clone of `sample/` with the `*.patch` files left out, one commit of it staged, `XDG_CACHE_HOME` pointed at an empty directory, and `TYPESAFE_API_KEY` set:

```sh
git apply agent-change.patch && git add -A
tenet --format json --no-cache > blocked.json
git apply agent-fix.patch && git add -A
tenet --format json > passed.json
```

Read the probabilities under 0.80 back with `tenet --config below-cutoff.yml --format json`, which is the same config at a cutoff of 0.01; the lint prints a number only for a finding or a near miss within 0.2 of the cutoff.

## Opening and recording it

A file URL is enough: `open demo/index.html`, or `xdg-open`. The page needs the network only for JetBrains Mono from Google Fonts, and falls back to the system monospace face without it.

The stage is exactly 1280 by 720 and scales down to fit a smaller window, so record a window of at least that at 30 fps, capturing the stage rather than the whole screen. Reload or press space to start the take. The page is dark whatever the system theme is, and honours `prefers-reduced-motion` by keeping the beats and dropping the transitions, which is not the take to record.

The timings are constants in the `T` object at the top of the script, with the total logged to the console on load.
