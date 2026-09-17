# Adding a rule

A built-in rule is a directory under `rules/<id>/` holding a `rule.yml`, an `examples.yml` beside it and a `README.md` paragraph that `tenetlint rules <id>` prints. The order below is the order the work happens in, and each step exists because skipping it makes the next one meaningless.

## 1. Write the tenet from a real source

Quote a sentence someone already wrote for their own repository, from a public `CLAUDE.md`, `AGENTS.md` or contributing guide, and put where you found it in the rule's `source` field. A sentence invented for the corpus is a sentence nobody has had to live with, and it reads like one: it names a shape rather than a habit, and the examples you then write for it are the shape restated. Keep the quote as it stands, in one or two sentences; the place to add precision is the criteria, not the sentence.

```yaml
id: no-fallback
tenet: "Do not add fallbacks, default-to-something-that-works-ish behavior, or silent degradation paths. Either the operation succeeds as intended, or it raises an actionable error."
source: amd/gaia CLAUDE.md
include: ["**/*.go"]
tags: [code, errors]
```

## 2. Collect twelve examples or more

`examples.yml` holds at least twelve cases, roughly half of each label, with `lines` on every violation: one line, or a `[first, last]` pair when the violation spans several and naming any line of it is right. Where the evidence is scattered over a file, give the tightest span that covers the part the rule is really about, which is usually where the substitution or the check happens rather than where the test later asserts on it.

Choose cases that are hard. A violation nobody would argue about scores 0.97 whatever the wording says, and moves no number when you edit the criteria; the cases that teach `check` anything are the ones a careful reviewer would have to stop and think about. Put in the acceptable cases that look like violations, too: the default that is genuinely part of an interface, the fake clock that is a production constructor argument, the null check on a value that really can be absent. Those are what a `false` criterion is written against.

When a label is a decision the tenet's sentence does not settle, write it down in the example's `note`. `comment-why` carries a function whose only comment is `// TODO: handle nil`, labelled `ok` with a note saying that a TODO restates nothing: the tenet is about a comment that repeats the code, so on its own words that case passes. The note is for the next person, who would otherwise read the label as a slip.

## 3. Run check

```
tenetlint check no-fallback
```

Read the verdict, then the misjudged list. `sharp` means nothing landed on the wrong side of the rule's cutoff; anything else names the examples that did, with their probabilities and one line of advice. The AUC says whether the wording separates the two labels at all: an AUC of 1.00 with misses under the cutoff is a wording that ranks perfectly and scores timidly, which the criteria can fix, while a low AUC is a tenet that has not said what it is about.

## 4. Sharpen the criteria against the misjudged list

Change `criteria.true` and `criteria.false`, not the examples and not `fail`. Lowering the cutoff to cover a rule's own weak spot buys a number and costs every repository that runs the rule.

Name the concrete shape the misjudged examples share: "an error value assigned to the blank identifier", "a comment that is a section header", "a value the function just produced with a `New...` constructor". Add an exclusion to `criteria.false` for the shape the acceptable examples share, which is what stops a criterion that lifts the violations from dragging the innocent cases up with them. Do not paste examples into the criteria as text; the spike measured that and it changes nothing at a cost of about 130 tokens a call.

Rerun after each edit and keep the numbers. Four attempts is enough to find out whether the wording is the problem: if the misses are still there with the AUC at 1.00, the remaining gap is the model's, not the sentence's, and the honest thing is to ship the rule as `usable` and say so.

## 5. Add it to a preset

A rule that belongs in no preset ships in the binary and runs for nobody. Name it in a file under `presets/`, or give it the `standalone` tag when leaving it out is deliberate. `TestBuiltinRulesShip` fails on a rule that is neither.

## 6. Let the corpus test hold the line

`TestBuiltinRulesShip` requires twelve examples per rule and both labels present. It is the reason a rule cannot arrive with three cases and a verdict that means nothing.
