# demo

`index.html` plays a 44.7-second animation of three commits meeting the hook, for a screen recorder to capture. It runs once on load; space or a click replays it.

## What the clip shows

Three acts on a stage of exactly 1280 by 720, each one blocked and then fixed, with a session strip along the bottom where the agent works:

- **0.4 s, the code.** The turn `> Add discount codes to invoice totals` is typed into the session, `● Bash(git commit -m "Update billing.go and billing_test.go")` follows with the subject from `sample/commit-msg-bad.txt`, and the diff of `sample/agent-change.patch` opens line by line. The counter runs while jev is asked, the bars fill against the 0.80 cutoff, four of them cross it, and each one's numbered marker appears both on its row and in the gutter of the line it names; where the line the run measured sits under a comment that narrates it, the highlight covers both lines and the marker sits on the comment. The headline becomes "Add discount codes. Blocked in 0.89 s." and the session answers `⎿ tenet: 4 findings · commit blocked`. Then the agent fixes its own findings, the diff crossfades to `sample/agent-fix.patch`, the bars fall under the cutoff, the headline reads "Fixed by the agent. Passed in 0.80 s." and the same command, run again, gets `⎿ tenet: 0 findings · code passed`.
- **16.4 s, the commit message.** One flow, so the strip opens on no new prompt: the commit that just cleared the code is stopped by its own message, `⎿ tenet: 1 finding · reword the message`. The same shape over `sample/commit-msg-bad.txt`, a subject that names the files that were open: `subject-says-what-changed` at 0.96, then the reworded message at 0.14, committed as `● Bash(git commit -m "Add discount codes, rounding half cents to the house")`.
- **29.0 s, the pull request.** `sample/pr-bad.txt`, a title naming files, a body that lists what was touched and four screenshots: all four rules of the `pr` preset fire, between 0.88 and 0.97. The rewritten description passes, and the clip ends on that pass at 44.3 s.

Black covers the stage for 400 ms at each end, so the cut points are clean, and a hairline along the bottom edge fills over the whole clip, which is how you see on the recording where it starts and ends.

Each tenet's row carries its id, a few plain words for what the rule asks, the bar, and the file and line of its finding. The code act shows the four rules that fire; `no-transcript-comment`, `no-placeholder-phrase` and `assertion-justified` also ran and stayed at or under 0.13, and their numbers are in `data/cutoff-sweep.txt`.

## The numbers

Every id, path, line number and figure on screen is one measured run, captured on 2026-09-18 and committed under `data/`, six runs in all. `data/README.md` says which command produced each file and how the probabilities under the cutoff were read back. The clip costs what the six runs cost: $0.000318 for the blocked code commit, $0.000037 for the commit message, $0.000119 for the pull request, and less again for each of the three that pass.

## Opening and recording it

A file URL is enough: `open demo/index.html`, or `xdg-open`. The page needs the network only for JetBrains Mono from Google Fonts, and falls back to the system monospace face without it.

Record a window of at least 1280 by 720 at 30 fps, capturing the stage rather than the whole screen; it scales down to fit a smaller window, so give it room. Reload or press space to start the take. The page is dark whatever the system theme is, and honours `prefers-reduced-motion` by keeping the beats and dropping the transitions, which is not the take to record.

The acts start at the milliseconds in `T.AT` at the top of the script, and each act's own beats are in its `off`. The console logs the act, verdict and pass times on load.
