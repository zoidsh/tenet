# tenetlint

tenetlint is a command-line linter for the rules you wrote in English. It reads tenets such as "a comment says why, not what" from a `tenets.yml`, sends your staged changes to TypeSafe's jev model for judgement, and reports each violation with a file, a line and a probability.

Install it with `npm install -g tenetlint`, or run it with `npx tenetlint`; the binary for your platform comes from an optional dependency, so there is no install script. Set `TYPESAFE_API_KEY` to your key, then run `tenet init` and `tenet`.

The command is `tenet`, with `tenetlint` beside it as the same program under its old name.

The documentation lives at https://github.com/zoidsh/tenetlint.
