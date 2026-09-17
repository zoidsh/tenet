# tenet

tenet is a command-line linter for the rules you wrote in English. It reads tenets such as "a comment says why, not what" from a `tenet.yml`, sends your staged changes to TypeSafe's jev model for judgement, and reports each violation with a file, a line and a probability.

Install it with `npm install -g @zoidsh/tenet`, or run it with `npx @zoidsh/tenet`; the binary for your platform comes from an optional dependency, so there is no install script. Run `tenet auth typesafe` to save your key, then `tenet init` and `tenet`.

The documentation lives at https://github.com/zoidsh/tenet.
