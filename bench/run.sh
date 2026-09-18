#!/usr/bin/env bash
# Regenerates bench/results.md from scratch. Nothing in that file is written by
# hand: the top-level README quotes its numbers, and a number nobody can
# re-measure is a number nobody can correct.
set -euo pipefail

# COMMIT is the commit every measurement is taken at, checked out into a
# throwaway worktree. A benchmark that followed HEAD would report a different
# corpus every week and the README would drift without anybody editing it. It
# is chosen by hand for a staged change worth showing: the staged row lints
# COMMIT's own change, so a commit that touched only data files would measure
# nothing.
COMMIT=f0fdf08

# BASE is chosen so that the diff to COMMIT is about two thousand added plus
# deleted lines of Go, which is the size of pull request the agent-reviewer
# comparison is about. Walk `git log main` if the history is rewritten and put
# the measured count back in the table.
BASE=7911bea

# Every model is asked this, with the same diff appended, so that the rows of
# the agent table differ only by the model that answered.
AGENT_PROMPT="Review this pull request diff as a senior engineer. Report the bugs, risks and changes it needs, most important first, in about a page. Do not restate the diff."

if [ ! -f bench/run.sh ]; then
	echo "run this from the repository root, as bench/run.sh" >&2
	exit 2
fi

for tool in jq git claude; do
	if ! command -v "$tool" >/dev/null 2>&1; then
		echo "bench/run.sh needs $tool" >&2
		exit 2
	fi
done
if [ -z "${TENET_BIN:-}" ] && ! command -v mise >/dev/null 2>&1; then
	echo "bench/run.sh needs mise to build the binary, or TENET_BIN pointing at one" >&2
	exit 2
fi

# bench/results.md is this script's own output, so a modified or untracked one
# is not the kind of change that would make the measurement mean something
# else. Anything else still refuses.
if [ -n "$(git status --porcelain -- ':!bench/results.md')" ]; then
	echo "bench/run.sh measures a fixed commit, so it refuses to run with uncommitted changes" >&2
	echo "commit or stash them; bench/results.md itself is the one exception" >&2
	exit 2
fi

tmp=$(mktemp -d)
partial=bench/.results.md.partial
cleanup() {
	if [ -d "$tmp/repo" ]; then
		git worktree remove --force "$tmp/repo" >/dev/null 2>&1 || true
	fi
	rm -rf "$tmp"
	rm -f "$partial"
}
trap cleanup EXIT

if [ -n "${TENET_BIN:-}" ]; then
	bin=$(cd "$(dirname "$TENET_BIN")" && pwd)/$(basename "$TENET_BIN")
else
	# Built once, outside the measured runs, so that no timing carries a
	# compile with it.
	mise exec -- go build -o "$tmp/tenet" ./cmd/tenet
	bin="$tmp/tenet"
fi

# A cache of its own, so that the developer's is not filled with a benchmark's
# answers.
export TENET_CACHE_DIR="$tmp/cache"
mkdir -p "$TENET_CACHE_DIR"

if ! "$bin" auth --status >"$tmp/auth.txt" 2>&1; then
	cat "$tmp/auth.txt" >&2
	exit 2
fi

git worktree add --detach "$tmp/repo" "$COMMIT" >/dev/null

repo=$PWD

# A finding is exit 1 and is what the run is for; anything above that is a
# broken run and stops the script.
lint() {
	out=$tmp/$1
	dir=$2
	shift 2
	code=0
	(cd "$dir" && "$bin" "$@" --format json) >"$out" 2>"$tmp/stderr.txt" || code=$?
	if [ "$code" -gt 1 ]; then
		cat "$tmp/stderr.txt" >&2
		cat "$out" >&2
		echo "bench/run.sh: tenet exited $code" >&2
		exit "$code"
	fi
}

lint_stats() {
	jq -r '[.stats.files, .stats.windows, .stats.calls, .stats.input_tokens,
	        .stats.cost_usd, .stats.duration_ms, (.findings | length)] | @tsv' "$tmp/$1"
}

group() {
	printf '%s' "$1" | sed -E ':a; s/([0-9]+)([0-9]{3})/\1,\2/; ta'
}

secs() {
	awk -v ms="$1" 'BEGIN { printf "%.1f s", ms / 1000 }'
}

cost() {
	awk -v c="$1" 'BEGIN {
		if (c == 0) { printf "$0.0000" }
		else if (c < 0.0001) { printf "<$0.0001" }
		else { printf "$%.4f", c }
	}'
}

ratio() {
	awk -v v="$1" 'BEGIN { printf "%.2f", v }'
}

numstat_lines() {
	git -C "$tmp/repo" diff --numstat "$@" | awk '{ a += $1; d += $2 } END { print a + d + 0 }'
}

# Every run inside the checkout is judged against this branch's config rather
# than COMMIT's own. The rules live in the binary and the config only names a
# preset, so the measured commit needs no config of its own, and a commit older
# than the rename carries the file under its old name. A tenet's include globs are
# matched against paths relative to the run's own root rather than to the
# config's directory, so a config from outside the checkout judges it the same.
config="$repo/.tenet/config.yml"

lint cold.json "$tmp/repo" --no-cache --config "$config" .
lint warm.json "$tmp/repo" --config "$config" .

git -C "$tmp/repo" reset --soft HEAD~1 >/dev/null
staged_lines=$(numstat_lines --cached)
lint staged.json "$tmp/repo" --no-cache --config "$config"

base_all=$(numstat_lines "$BASE")
base_go=$(numstat_lines "$BASE" -- '*.go')
base_bytes=$(git -C "$tmp/repo" diff "$BASE" | wc -c | tr -d ' ')
lint diff.json "$tmp/repo" --no-cache --config "$config" --base "$BASE"

# The pull request text and the config that judges it are files of this
# branch, not of COMMIT, and neither reads the tree, so this one runs here.
pr_lines=$(wc -l <bench/pr.txt | tr -d ' ')
lint pr.json "$repo" --no-cache --pr-text bench/pr.txt --config bench/pr-tenet.yml

"$bin" check --builtin --no-cache --runs 3 --format json >"$tmp/builtin.json" 2>"$tmp/stderr.txt" || {
	cat "$tmp/stderr.txt" >&2
	exit 2
}
for lang_code in de ja; do
	"$bin" check --no-cache --runs 3 --format json \
		--config "bench/lang/comment-why.$lang_code.yml" >"$tmp/$lang_code.json" 2>"$tmp/stderr.txt" || {
		cat "$tmp/stderr.txt" >&2
		exit 2
	}
done

# Which preset each rule belongs to. A rule in none is standalone, which is a
# decision the corpus records rather than an oversight.
awk '
	FNR == 1 { inrules = 0 }
	/^name:/ { name = $2 }
	/^rules:/ { inrules = 1; next }
	inrules && /^[[:space:]]*-[[:space:]]/ {
		id = $2
		print id "\t" name
		next
	}
	inrules && !/^[[:space:]]/ { inrules = 0 }
' presets/*.yml | sort >"$tmp/presets.tsv"

preset_of() {
	awk -F'\t' -v id="$1" '$1 == id { out = out == "" ? $2 : out ", " $2 } END { print out == "" ? "standalone" : out }' "$tmp/presets.tsv"
}

rule_rows() {
	jq -r '.tenets[] | [.tenet, .examples, .verdict, .auc, .accuracy, .fail,
	                    .located_examples, .location_rate, .separated,
	                    (.stability.max_std_dev // -1), (.stability.crossed | length)] | @tsv' "$1"
}

prices() {
	awk '
		function value(line) {
			v = substr(line, index(line, ":") + 1)
			gsub(/^[[:space:]]+|[[:space:]]+$/, "", v)
			return v
		}
		function flush() {
			if (id != "") printf "%s\t%s\t%s\t%s\t%s\t%s\t%s\n", id, name, inp, outp, src, day, note
			id = ""; name = ""; inp = ""; outp = ""; src = ""; day = ""; note = ""
		}
		/^[[:space:]]*#/ { next }
		/^[[:space:]]*-[[:space:]]*id:/ { flush(); id = value($0); next }
		/^[[:space:]]*name:/ { name = value($0); next }
		/^[[:space:]]*input_usd_per_million:/ { inp = value($0); next }
		/^[[:space:]]*output_usd_per_million:/ { outp = value($0); next }
		/^[[:space:]]*source:/ { src = value($0); next }
		/^[[:space:]]*date:/ { day = value($0); next }
		/^[[:space:]]*note:/ { note = value($0); next }
		END { flush() }
	' bench/prices.yml
}

# One call per model, all made before the table is written so that a failure
# stops the run rather than leaving a half-measured table behind. The working
# directory is the throwaway one rather than the repository, so that no project
# settings or CLAUDE.md can reach the model and make one row unlike the others.
agent_runs() {
	{
		printf '%s\n\n' "$AGENT_PROMPT"
		git -C "$tmp/repo" diff "$BASE"
	} >"$tmp/prompt.txt"
	prices >"$tmp/prices.tsv"
	# Every entry is checked before the first call, so that a half-written one
	# costs nothing rather than being found out four calls in.
	while IFS=$'\t' read -r id _ inp outp _ _ _; do
		for field in "$id" "$inp" "$outp"; do
			if [ -z "$field" ] || [ "$field" = null ]; then
				echo "bench/run.sh: the bench/prices.yml entry for '${id:-an entry with no id}' is missing an id, an input price or an output price" >&2
				exit 2
			fi
		done
	done <"$tmp/prices.tsv"
	while IFS=$'\t' read -r id _ _ _ _ _ _; do
		if ! (cd "$tmp" && claude -p --model "$id" --tools "" --system-prompt "" \
			--strict-mcp-config --setting-sources "" --output-format json \
			<"$tmp/prompt.txt" >"$tmp/agent-$id.json"); then
			echo "bench/run.sh: the claude CLI exited non-zero for $id" >&2
			exit 2
		fi
		if ! jq -e '.usage.input_tokens and .usage.output_tokens' "$tmp/agent-$id.json" >/dev/null 2>&1; then
			echo "bench/run.sh: the claude call for $id returned no usable JSON, so it cannot be priced" >&2
			exit 2
		fi
		if [ "$(jq -r '.is_error' "$tmp/agent-$id.json")" != false ]; then
			echo "bench/run.sh: the claude call for $id reported an error:" >&2
			jq -r '.result // "no result field"' "$tmp/agent-$id.json" >&2
			exit 2
		fi
	done <"$tmp/prices.tsv"
}

agent_cells() {
	jq -r '[(.usage.input_tokens + .usage.cache_read_input_tokens + .usage.cache_creation_input_tokens),
	        .usage.output_tokens, (.usage.output_tokens_details.thinking_tokens // 0),
	        .duration_ms, .duration_api_ms] | @tsv' "$tmp/agent-$1.json"
}

agent_row() {
	name=$2
	IFS=$'\t' read -r in_tokens out_tokens _ ms _ <<<"$(agent_cells "$1")"
	[ -n "$name" ] && [ "$name" != null ] || name=$1
	usd=$(awk -v i="$in_tokens" -v o="$out_tokens" -v pi="$3" -v po="$4" \
		'BEGIN { printf "%.6f", (i * pi + o * po) / 1000000 }')
	printf '| %s | %s | %s | %s | %s |\n' \
		"$name" "$(group "$in_tokens")" "$(group "$out_tokens")" "$(cost "$usd")" "$(secs "$ms")"
}

agent_footnote() {
	src=$2
	day=$3
	note=$4
	IFS=$'\t' read -r _ _ thinking _ api_ms <<<"$(agent_cells "$1")"
	line="$(group "$thinking") of its output tokens were thinking tokens, and the CLI put $(secs "$api_ms") of the call down to the API"
	if [ "$src" != null ] && [ -n "$src" ]; then
		line="$line; prices from $src, read $day"
	fi
	if [ "$note" != null ] && [ -n "$note" ]; then
		line="$line; $note"
	fi
	echo
	echo "$1: $line."
}

speed_row() {
	label=$1
	scope=$2
	stats=$3
	IFS=$'\t' read -r files windows calls tokens usd ms findings <<<"$stats"
	printf '| %s | %s | %s | %s | %s | %s | %s | %s | %s |\n' \
		"$label" "$scope" "$(group "$files")" "$(group "$windows")" "$(group "$calls")" \
		"$(group "$tokens")" "$(cost "$usd")" "$(secs "$ms")" "$(group "$findings")"
}

rule_table_rows() {
	want=$1
	while IFS=$'\t' read -r id examples verdict auc accuracy fail located location separated sd crossed; do
		preset=$(preset_of "$id")
		case "$want" in
		preset) [ "$preset" != standalone ] || continue ;;
		standalone) [ "$preset" = standalone ] || continue ;;
		esac
		auc_cell=$(ratio "$auc")
		[ "$separated" = true ] || auc_cell="n/a"
		location_cell="none labelled"
		if [ "$located" -gt 0 ]; then
			location_cell="$(ratio "$location") over $located"
		fi
		sd_cell="n/a"
		if awk -v sd="$sd" 'BEGIN { exit !(sd >= 0) }'; then
			sd_cell=$(awk -v sd="$sd" 'BEGIN { printf "%.3f", sd }')
		fi
		printf '| %s | %s | %s | %s | %s | %s | %s | %s | %s |\n' \
			"$id" "$preset" "$examples" "$auc_cell" \
			"$(ratio "$accuracy") at $(ratio "$fail")" "$location_cell" "$verdict" "$sd_cell" "$crossed"
	done
}

lang_row() {
	printf '| %s | %s | %s | %s | %s | %s | %s | %s |\n' "$1" "$2" "$3" "$4" "$5" "$6" "$7" "$8"
}

lang_cells() {
	jq -r --arg id "$1" '.tenets[] | select(.tenet == $id) |
		[.examples, .auc, ([.accuracy_at[] | select(.cutoff > 0.75 and .cutoff < 0.85) | .accuracy] | first),
		 .located_examples, .location_rate, (.stability.max_std_dev // -1),
		 (.stability.crossed | length), .verdict] | @tsv' "$2"
}

lang_from() {
	IFS=$'\t' read -r examples auc at80 located location sd crossed verdict <<<"$(lang_cells "$2" "$3")"
	location_cell="none labelled"
	if [ "$located" -gt 0 ]; then
		location_cell="$(ratio "$location") over $located"
	fi
	lang_row "$1" "$examples" "$(ratio "$auc")" "$(ratio "$at80")" "$location_cell" \
		"$(awk -v sd="$sd" 'BEGIN { printf "%.3f", sd }')" "$crossed" "$verdict"
}

IFS=$'\t' read -r _ _ _ diff_tokens diff_usd diff_ms _ <<<"$(lint_stats diff.json)"

agent_runs

{
	echo "# tenet benchmarks"
	echo
	echo "Generated by bench/run.sh on $(date -u +%Y-%m-%d) at commit $COMMIT; do not edit by hand."
	echo
	echo "## Speed and cost"
	echo
	echo "| Run | Scope | Files | Windows | Calls | Input tokens | Cost | Duration | Findings |"
	echo "| --- | --- | --- | --- | --- | --- | --- | --- | --- |"
	speed_row "Full sweep, cold cache" "the whole tree" "$(lint_stats cold.json)"
	speed_row "Full sweep, warm cache" "the same tree, straight after" "$(lint_stats warm.json)"
	speed_row "One staged change" "$(group "$staged_lines") lines staged" "$(lint_stats staged.json)"
	speed_row "A $(group "$base_go")-line diff" "$(group "$base_all") lines, $(group "$base_go") of them Go, $(group "$base_bytes") bytes of diff" "$(lint_stats diff.json)"
	speed_row "One pull request text" "$(group "$pr_lines") lines of title and description" "$(lint_stats pr.json)"
	echo
	echo "Every duration is the \`stats.duration_ms\` the run reported, not wall clock, so the numbers exclude starting the process and compiling the binary. The cold sweep runs with \`--no-cache\` against an empty cache directory; the warm sweep is the same command immediately after, reading what the cold one wrote. The staged row runs in a worktree whose HEAD was moved back one commit, so what it lints is the fixed commit's own change."
	echo
	echo "## Rule quality"
	echo
	echo "\`tenet check --builtin --no-cache --runs 3\`: every rule in the binary, its corpus judged three times. Accuracy is the share of labelled examples on the right side of the rule's own cutoff in the first pass, the deviation is the largest any one example moved across the three passes, and a crossing is an example whose passes fell on both sides of the cutoff, so that its verdict is the run it was asked in."
	echo
	echo "| Rule | Preset | Examples | AUC | Accuracy at fail | Location | Verdict | Largest sd | Crossings |"
	echo "| --- | --- | --- | --- | --- | --- | --- | --- | --- |"
	rule_rows "$tmp/builtin.json" | rule_table_rows preset
	rule_rows "$tmp/builtin.json" | rule_table_rows standalone
	echo
	echo "## Languages"
	echo
	echo "The comment-why rule, its sentence, its criteria and all of its examples translated, each corpus judged three times at the 0.80 cutoff. The English row is the comment-why row of the run above; the other two come from bench/lang/comment-why.de.yml and bench/lang/comment-why.ja.yml, which hold the same fourteen examples with the same labels and only their prose translated."
	echo
	echo "| Language | Examples | AUC | Accuracy at 0.80 | Location | Largest sd | Crossings | Verdict |"
	echo "| --- | --- | --- | --- | --- | --- | --- | --- |"
	lang_from English comment-why "$tmp/builtin.json"
	lang_from German comment-why "$tmp/de.json"
	lang_from Japanese comment-why "$tmp/ja.json"
	echo
	echo "## Against an agent reviewer"
	echo
	echo "| Reviewer | Input tokens | Output tokens | Cost | Time |"
	echo "| --- | --- | --- | --- | --- |"
	printf '| tenet, measured | %s | n/a | %s | %s |\n' \
		"$(group "$diff_tokens")" "$(cost "$diff_usd")" "$(secs "$diff_ms")"
	while IFS=$'\t' read -r id name inp outp _ _ _; do
		agent_row "$id" "$name" "$inp" "$outp"
	done <"$tmp/prices.tsv"
	echo
	echo "Every row here is measured. Each agent row is one \`claude -p\` call through the Claude Code CLI, with an empty system prompt, no tools, no MCP servers and no settings of any kind, so that nothing but the model differs between them. All four were sent the identical prompt: one instruction line, then the $(group "$base_bytes") bytes of \`git diff $BASE\`. Input tokens are what the API counted for that call, including any cache reads; output tokens include the model's thinking tokens, which the per-model lines below give on their own. Cost is those token counts at the list prices in bench/prices.yml, all input tokens at the input price, rather than the figure the CLI reports for the call. Time is duration_ms as the CLI reports it, from the start of the turn to the result; the per-model lines give the CLI's own duration_api_ms beside it, which it measures from a different point and can exceed the wall figure. tenet's row is measured the same way it is elsewhere in this file, and its input tokens are what the run actually sent, which is the changed windows rather than the whole diff."
	echo
	echo "Every agent row is a lower bound: one call, the whole diff in the prompt, no tool use, no reading the rest of the repository and no second pass. A reviewer that opens the files around the diff, or that is asked again about what it missed, costs more than this and takes longer."
	while IFS=$'\t' read -r id _ _ _ src day note; do
		agent_footnote "$id" "$src" "$day" "$note"
	done <"$tmp/prices.tsv"
} >"$partial"

mv "$partial" bench/results.md
echo "wrote bench/results.md"
