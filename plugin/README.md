# tenetlint for Claude Code

A Claude Code plugin that teaches the agent to lint its own changes against your `tenets.yml` and to fix what comes back, without a person in the loop.

## Install

This repository is its own marketplace, so add it and then install the plugin from it:

```
/plugin marketplace add zoidsh/tenetlint
/plugin install tenetlint@tenetlint
```

To try it from a checkout instead, start Claude Code with `claude --plugin-dir ./plugin`.

The plugin runs the `tenet` binary; install it first, and set `TYPESAFE_API_KEY` in the environment the agent runs in.

## What it does

The `tenet` skill tells the agent when to run the lint and what to do with a finding: read `--format json`, open the file at the line, and change the code until the tenet's sentence holds, or mark the line with a `tenet:ignore <id>` directive and write down why. It never lets the agent edit `tenets.yml` or a cutoff to make a finding go away.

The `PreToolUse` hook catches a `git commit` before it runs, lints the staged changes, and denies the tool call when there is a finding, handing the agent the findings and the `next` line so it can fix them and commit again. It exits without linting when `TENETLINT_SKIP` is set, and when `tenet` is not on `PATH` it says so and lets the commit through.

The same instructions go into a repository, for the agents that do not read Claude Code plugins, with `tenet init --agent cursor`, `--agent agents` or `--agent claude`.
