# tenet for Claude Code

A Claude Code plugin that teaches the agent to lint its own changes against your `tenet.yml` and to fix what comes back, with no one in the loop.

## Install

This repository is its own marketplace, so add it and then install the plugin from it:

```
/plugin marketplace add zoidsh/tenet
/plugin install tenet@zoidsh
```

To try it from a checkout instead, start Claude Code with `claude --plugin-dir ./plugin`.

The plugin runs the `tenet` binary, which the [README](../README.md) has every way of installing, and which needs a TypeSafe key: run `tenet auth typesafe` once, or set `TYPESAFE_API_KEY` in the environment the agent runs in.

## What it does

The `tenet` skill says when to run the lint and what to do with a finding: read `--format json`, open the file at the line, and change the code until the tenet's sentence holds. It rules out the shortcuts, so the agent does not edit `tenet.yml`, lower a cutoff, write a baseline or add a `tenet:ignore` directive to make its own finding go away; a finding it thinks is wrong comes back to you with the line and the tenet's sentence instead.

The skill also carries the setup, so `set tenet up in this repository` is the whole instruction: the agent checks `tenet auth --status`, asks you to run `tenet auth` when there is no key, runs `tenet init` and `tenet hook install`, replaces each drafted rule that a built-in rule already covers, then calibrates every custom tenet against labelled examples before it gates a commit. Installing tenet and entering the key are the two steps that stay yours.

The `PreToolUse` hook catches a `git commit` before it runs, lints the staged changes, and denies the tool call when there is a finding, handing the agent the findings and the `next` line so it can fix them and commit again. Claude Code's `if` filter runs a hook anyway when it cannot tell what a Bash command expands to, so the hook reads the tool call on stdin and lints only when the command itself commits. It exits without linting when `TENET_SKIP` is set, and when `tenet` is not on `PATH`, or the repository has no `tenet.yml`, it says so and lets the commit through.

The same instructions go into a repository, for the agents that do not read Claude Code plugins, with `tenet init --agent cursor`, `--agent agents` or `--agent claude`.
