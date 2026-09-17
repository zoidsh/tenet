# tenetlint

tenetlint is a command-line linter for the rules you wrote in English. It reads tenets such as "a comment says why, not what" from a `tenets.yml`, sends your staged changes to TypeSafe's jev model for judgement, and reports each violation with a file, a line and a probability, so the conventions in your CLAUDE.md become a gate you can run on every commit instead of a document nobody rereads.

Set `TYPESAFE_API_KEY` to your key. `TYPESAFE_BASE_URL` sends the requests to another host, such as a proxy or a local stand-in, and `TENETLINT_SKIP=1` makes the installed pre-commit hook exit without linting.

Pre-release: nothing here is stable yet, and the tenet format, the flags and the output may all change without notice.
