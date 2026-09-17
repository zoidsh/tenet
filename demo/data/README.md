# The numbers the demo page animates

One measured run, captured on 2026-09-18 against the live jev API, over the sample project in `demo/sample`. Every number and line in `demo/index.html` comes from these files.

Each run is a fresh clone of `demo/sample` (the `*.patch` files left out), `git init`, one commit of the sample as it stands, then `git apply` of the patches and `git add -A`, so the lint is judging a staged change. `XDG_CACHE_HOME` pointed at an empty directory, so the blocked run is cold.

- `blocked.json`: `agent-change.patch` applied and staged, then `tenet --format json --no-cache`. Four findings.
- `passed.json`: `agent-fix.patch` applied on top, staged, then `tenet --format json`. No findings. Its `cache_hits` is 0 because the fix rewrote both windows, and a window's answer is cached against its content.
- `below-cutoff-blocked.json` and `below-cutoff-passed.json`: the same two states judged again through `below-cutoff.yml`, a copy of the sample's `tenet.yml` whose every cutoff is 0.01, as `tenet --config below-cutoff.yml --format json`. The lint prints a probability only for a finding or for a near miss within 0.2 of the cutoff, and this is how the numbers under 0.8 were read back. It costs nothing, because the cache is keyed by the question rather than by the cutoff.

jev answers with the tenets it raises, so a tenet it is quiet about has no probability in either file at any cutoff. `no-placeholder-phrase` and `assertion-justified` have no number in either run, and `no-transcript-comment` only in the passing one; the page draws those rows at zero and says so.
