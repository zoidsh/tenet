// Checks index.html where there is no browser: its timeline, under a DOM stub
// and a virtual clock, and its stylesheet. Run `node demo/check.js`.
const fs = require("fs");
const path = require("path");

const HTML = fs.readFileSync(process.argv[2] || path.join(__dirname, "index.html"), "utf8");
const src = HTML.slice(HTML.indexOf("<script>") + 8, HTML.lastIndexOf("</script>"));

class El {
	constructor(tag) {
		this.tag = tag;
		this.children = [];
		this.parent = null;
		this._text = "";
		this.dataset = {};
		this.classes = new Set();
		this.style = {
			_props: {},
			setProperty: (k, v) => { this.style._props[k] = v; }
		};
	}
	get className() { return [...this.classes].join(" "); }
	set className(v) { this.classes = new Set(String(v).split(/\s+/).filter(Boolean)); }
	get classList() {
		const cs = this.classes;
		return {
			add: (...n) => n.forEach((c) => cs.add(c)),
			remove: (...n) => n.forEach((c) => cs.delete(c)),
			contains: (c) => cs.has(c),
			toggle: (c, on) => { if (on === undefined) { cs.has(c) ? cs.delete(c) : cs.add(c); } else if (on) { cs.add(c); } else { cs.delete(c); } }
		};
	}
	get textContent() {
		if (this.children.length) return this.children.map((c) => c.textContent).join("");
		return this._text;
	}
	set textContent(v) {
		this.children.forEach((c) => { c.parent = null; });
		this.children = [];
		this._text = String(v);
	}
	append(...nodes) {
		for (const n of nodes) {
			if (n.parent) n.parent.children.splice(n.parent.children.indexOf(n), 1);
			n.parent = this;
			this.children.push(n);
		}
	}
	get previousElementSibling() {
		if (!this.parent) return null;
		const i = this.parent.children.indexOf(this);
		return i > 0 ? this.parent.children[i - 1] : null;
	}
	get nextElementSibling() {
		if (!this.parent) return null;
		const i = this.parent.children.indexOf(this);
		return i + 1 < this.parent.children.length ? this.parent.children[i + 1] : null;
	}
	matches(sel) {
		let m = /^\.([\w-]+)$/.exec(sel);
		if (m) return this.classes.has(m[1]);
		m = /^\[data-([\w-]+)="(.*)"\]$/.exec(sel);
		if (m) {
			const key = m[1].replace(/-([a-z])/g, (_, c) => c.toUpperCase());
			return this.dataset[key] === m[2];
		}
		throw new Error("stub selector not supported: " + sel);
	}
	querySelector(sel) {
		for (const c of this.children) {
			if (c.matches(sel)) return c;
			const deep = c.querySelector(sel);
			if (deep) return deep;
		}
		return null;
	}
	querySelectorAll(sel, out) {
		out = out || [];
		for (const c of this.children) {
			if (c.matches(sel)) out.push(c);
			c.querySelectorAll(sel, out);
		}
		return out;
	}
}

const byId = new Map();
function el(id) {
	if (!byId.has(id)) {
		const e = new El("div");
		e.id = id;
		byId.set(id, e);
	}
	return byId.get(id);
}
// The markup's own starting state, for the ids whose initial values the script reads.
el("layer-passed").dataset.state = "hidden";
el("blackout").dataset.shown = "1";
el("counter").textContent = "0.00 s";
el("cost-caption").textContent = "cost";

const document = {
	createElement: (t) => new El(t),
	getElementById: (id) => el(id)
};

let now = 0;
let seq = 0;
let nextId = 0;
const queue = new Map();
function setTimeoutStub(fn, delay) {
	const id = ++nextId;
	queue.set(id, { id, at: now + Math.max(0, delay | 0), seq: ++seq, fn });
	return id;
}
function clearTimeoutStub(id) { queue.delete(id); }
const DateStub = { now: () => now };

const checkpoints = [];
function checkAt(ms, label, fn) { checkpoints.push({ at: ms, label, fn }); }

const window = {
	innerWidth: 1280,
	innerHeight: 720,
	matchMedia: () => ({ matches: false }),
	addEventListener: () => {}
};

const logs = [];
const consoleStub = { log: (...a) => logs.push(a.join(" ")) };

const fn = new Function("document", "window", "console", "setTimeout", "clearTimeout", "Date", src);

const failures = [];
function ok(cond, msg) { if (!cond) failures.push(msg); }
function eq(got, want, msg) { if (JSON.stringify(got) !== JSON.stringify(want)) failures.push(msg + "\n  got:  " + JSON.stringify(got) + "\n  want: " + JSON.stringify(want)); }

// ---- the invariants and the checkpoints ----

const PRE = 0;
const AT = [0, 16000, 28600];
const MAIN_FADE = 500;
const starts = AT.map((a) => PRE + a);

const rowsOf = (id) => el(id).children.filter((c) => c.classes.has("row"));
const allRows = () => rowsOf("layer-blocked").concat(rowsOf("layer-passed"));
const sessionLines = () => el("session-lines").children.map((c) => c.textContent);

// Between the acts the layers are blanked; from there until main is visible
// again no row may carry `in`, or it is on screen while the pane is empty.
// Every layer that exists is held to the rows the pane has.
let blanked = true;
function invariant() {
	const rows = allRows();
	if (rows.length === 0) blanked = true;
	if (el("main").dataset.vis === "1") blanked = false;
	if (blanked) {
		const early = rows.filter((r) => r.classes.has("in"));
		ok(early.length === 0, "a row carried `in` at " + now + " ms, before main's data-vis was 1");
	}
	budget("layer-blocked");
	budget("layer-passed");
}

starts.slice(1).forEach((s, i) => {
	checkAt(s - 1, "act " + (i + 2) + " opens on empty layers", () => {
		eq(rowsOf("layer-blocked").length, 0, "layer-blocked was not empty at " + (s - 1) + " ms");
		eq(rowsOf("layer-passed").length, 0, "layer-passed was not empty at " + (s - 1) + " ms");
	});
});

// A finding marks the one line it was measured on, and no other.
checkAt(starts[0] + 8000, "code act markers", () => {
	const rows = rowsOf("layer-blocked");
	const lit = rows.filter((r) => r.classes.has("hit"));
	eq(lit.map((r) => r.dataset.hit), ["no-fallback", "money-in-cents", "comment-why", "no-mocking"],
		"the lines lit in the code act are not exactly the measured ones");
	eq(lit.map((r) => r.querySelector(".marker").textContent), ["1", "2", "3", "4"],
		"a marker is not on the line its finding was measured on");
	for (const row of lit) {
		eq(row.querySelector(".marker").dataset.shown, "1", "a marker on a lit line is not shown");
	}
	for (const row of rows) {
		if (row.classes.has("hit")) continue;
		eq(row.querySelector(".marker").dataset.shown, "0", "a marker is shown on a line with no finding");
	}
});

// item 2: one flow, pre-commit then commit-msg on the same commit.
checkAt(starts[1] - 1, "act 1 session", () => {
	eq(sessionLines(), [
		"> Add discount codes to invoice totals",
		"● Bash(git commit -m \"Update billing.go and billing_test.go\")",
		"  ⎿  tenet: 4 findings · commit blocked",
		"● fixing billing.go:26, :31, :32 and billing_test.go:36",
		"● Bash(git commit -m \"Update billing.go and billing_test.go\")",
		"  ⎿  tenet: 0 findings · code passed"
	], "act 1's session strip");
});

checkAt(starts[2] - 1, "act 2 session", () => {
	eq(sessionLines().slice(6), [
		"  ⎿  tenet: 1 finding · reword the message",
		"● rewording the subject",
		"● Bash(git commit -m \"Add discount codes, rounding half cents to the house\")",
		"  ⎿  tenet: 0 findings · committed"
	], "act 2's session strip");
});

checkAt(starts[2] + 12000, "act 3 session", () => {
	eq(sessionLines().slice(10), [
		"> Open the pull request",
		"● Bash(gh pr create --fill)",
		"  ⎿  tenet: 4 findings · pull request blocked",
		"● rewriting the description",
		"● Bash(gh pr create --fill)",
		"  ⎿  tenet: 0 findings · pull request opened"
	], "act 3's session strip");
});

// the sweep: the headline tail shows the number the counter stopped on.
checkAt(starts[0] + 5000, "blocked numbers agree", () => {
	eq(el("counter").textContent, "1.07 s", "the counter at the verdict");
	ok(/Blocked in 1\.07 s\./.test(el("headline").textContent), "the headline tail disagrees with the counter: " + el("headline").textContent);
	eq(el("status").textContent, "4 findings · blocked", "the status line");
	ok(/^4 findings from /.test(el("subline").textContent), "the subline disagrees on the count: " + el("subline").textContent);
});

checkAt(starts[0] + 15000, "passed numbers agree", () => {
	eq(el("counter").textContent, "0.68 s", "the counter at the pass");
	ok(/Passed in 0\.68 s\./.test(el("headline").textContent), "the headline tail disagrees with the counter: " + el("headline").textContent);
	eq(el("status").textContent, "0 findings · passed", "the status line");
});

// item 4: the cost is a stat of its own, six decimals, no "per" anything.
checkAt(starts[0] + 5000, "cost stat", () => {
	eq(el("cost").textContent, "$0.000309", "the blocked cost");
	eq(el("cost-caption").textContent, "cost", "the cost caption");
	eq(el("clock-caption").textContent, "this commit", "the seconds caption");
	ok(!/per/.test(el("cost").textContent), "the cost line still says \"per\"");
});

checkAt(starts[0] + 15000, "passed cost stat", () => {
	eq(el("cost").textContent, "$0.000189", "the passed cost");
});

// ---- the stylesheet and the widths it has to hold ----

const css = HTML.slice(HTML.indexOf("<style>") + 7, HTML.indexOf("</style>"));
const below = css.slice(css.indexOf("}") + 1);

for (const m of below.matchAll(/font-size:\s*([^;]+);/g)) {
	ok(/^var\(--t-/.test(m[1].trim()), "a font-size below :root is not a token: " + m[1].trim());
}
for (const m of below.matchAll(/#[0-9a-f]{3,8}\b/g)) {
	failures.push("a literal colour below :root: " + m[0]);
}
for (const m of below.matchAll(/transition:\s*([^;]+);/g)) {
	// var(--bar-dur, 800ms) carries a comma of its own; the fallback is not a
	// second transition.
	for (const part of m[1].replace(/, *\d+ms/g, "").split(",")) {
		ok(/var\(--(fast|base|slow|fade|bar-dur)/.test(part), "a transition duration that is not a token: " + part.trim());
	}
}
for (const m of below.matchAll(/(padding|margin|gap|padding-left|margin-top|margin-left|line-height|height|width)[^:]*:\s*([^;]+);/g)) {
	for (const px of m[2].matchAll(/(\d+)px/g)) {
		const n = Number(px[1]);
		// 1px rules and the 2px the hit line's own border takes out of its inset
		// are not spacing; 720 and 1280 are the frame.
		if (n <= 2 || n === 720 || n === 1280) continue;
		ok(n % 4 === 0, "a size off the 4px rhythm: " + m[0].trim());
	}
}

// A JetBrains Mono glyph at 16px advances 9.6px, which Chrome on Linux rounds
// to 10; a tab stops every second column; a sans glyph at 16px averages about
// 0.52em and at 18px 0.6em.
const MONO = 10;
const LINE = 22;
const LEFT_PANE = Number(/grid-template-columns:\s*(\d+)px/.exec(css)[1]);
const RIGHT_PANE = 1280 - LEFT_PANE;
const PAD = 24 * 2;
// The row's 8px inset, the marker column, the number column and the two
// column gaps between them and the text.
const GUTTER = 8 + 24 + 12 + 24 + 12;
const COLUMNS = Math.floor((LEFT_PANE - PAD - GUTTER) / MONO);
// The main area's height less the pane's padding, in rows.
const MAIN = 720 - 52 - 88 - 112 - 40;
const ROWS = Math.floor((MAIN - 24) / LINE);

function columns(t) {
	let c = 0;
	for (const ch of t) c += ch === "\t" ? 2 - (c % 2) : 1;
	return c;
}

function rowsNeeded(row) {
	if (row.classes.has("file")) return 1;
	return Math.max(1, Math.ceil(columns(row.querySelector(".text").textContent) / COLUMNS));
}

// Every act, in both states, fits the pane: a line longer than the text
// column wraps and takes two rows, and the rows of a layer never exceed what
// the pane holds. A line a finding lands on is never one that wraps, so its
// marker sits on one row.
const budgeted = new Set();
function budget(layerId) {
	const rows = rowsOf(layerId);
	if (rows.length === 0) return;
	const key = layerId + ":" + rows.map((r) => r.textContent).join("\n");
	if (budgeted.has(key)) return;
	budgeted.add(key);
	const total = rows.reduce((n, r) => n + rowsNeeded(r), 0);
	ok(total <= ROWS, layerId + " holds " + total + " rows of the " + ROWS + " the pane has, starting " + JSON.stringify(rows[0].textContent));
	for (const row of rows) {
		if (row.dataset.hit && rowsNeeded(row) > 1) failures.push("a line with a finding wraps: " + JSON.stringify(row.querySelector(".text").textContent));
		if (row.dataset.line !== undefined && Number(row.dataset.line) > 99) failures.push("a line number wider than its column: " + row.dataset.line);
	}
}

// ---- the sample: every numbered line is the file's own, at that number ----

const SAMPLE = path.join(__dirname, "sample");
const read = (name) => fs.readFileSync(path.join(SAMPLE, name), "utf8");

// Applies a unified diff to the files it names, the way `git apply` does for
// the two patches, so the numbered lines can be read back from the result.
function applyPatch(files, patch) {
	const out = Object.assign({}, files);
	let name = null;
	let old = null;
	let fresh = null;
	let at = 0;
	const flush = () => {
		if (name === null) return;
		fresh.push(...old.slice(at));
		out[name] = fresh.join("\n");
	};
	for (const line of patch.split("\n")) {
		let m;
		if ((m = /^diff --git a\/(\S+) b\/(\S+)$/.exec(line))) {
			flush();
			name = m[2];
			old = out[name].split("\n");
			fresh = [];
			at = 0;
		} else if ((m = /^@@ -(\d+)(?:,\d+)? \+(\d+)(?:,\d+)? @@/.exec(line))) {
			const from = Number(m[1]) - 1;
			fresh.push(...old.slice(at, from));
			at = from;
		} else if (name !== null && (line.startsWith(" ") || line === "")) {
			if (line === "" && !/^(---|\+\+\+|index )/.test(line) && at >= old.length) continue;
			fresh.push(old[at]);
			at += 1;
		} else if (line.startsWith("-") && !line.startsWith("---")) {
			if (old[at] !== line.slice(1)) failures.push("the patch does not apply at " + name + ":" + (at + 1) + ": " + JSON.stringify(line));
			at += 1;
		} else if (line.startsWith("+") && !line.startsWith("+++")) {
			fresh.push(line.slice(1));
		}
	}
	flush();
	return out;
}

const base = { "billing.go": read("billing.go"), "billing_test.go": read("billing_test.go") };
const changed = applyPatch(base, read("agent-change.patch"));
const fixed = applyPatch(changed, read("agent-fix.patch"));
// The lines a patch adds, by file, because the same text can be context in
// one file and new in the other.
function added(patch) {
	const byFile = {};
	let name = null;
	for (const line of patch.split("\n")) {
		const m = /^diff --git a\/(\S+) b\/(\S+)$/.exec(line);
		if (m) {
			name = m[2];
			byFile[name] = new Set();
		} else if (name !== null && line.startsWith("+") && !line.startsWith("+++")) {
			byFile[name].add(line.slice(1));
		}
	}
	return byFile;
}

const TEXTS = {
	COMMIT_EDITMSG: [read("commit-msg-bad.txt"), read("commit-msg-good.txt")],
	PULL_REQUEST: [read("pr-bad.txt"), read("pr-good.txt")]
};

// Reads a layer back against the files it claims to show: each numbered row
// is that line of that file, and a row is tinted as added exactly when the
// patch adds that text.
function verbatim(layerId, files, adds, which) {
	let file = null;
	for (const row of rowsOf(layerId)) {
		if (row.dataset.file !== undefined) {
			file = row.dataset.file;
			continue;
		}
		const text = row.querySelector(".text").textContent;
		const n = Number(row.dataset.line);
		const source = files[file] !== undefined ? files[file] : (TEXTS[file] ? TEXTS[file][which] : null);
		if (source === null || source === undefined) {
			failures.push(layerId + " shows " + file + ", which is not a sample file");
			continue;
		}
		const want = source.split("\n")[n - 1];
		if (text !== want) failures.push(layerId + " line " + file + ":" + n + " is " + JSON.stringify(text) + ", the file has " + JSON.stringify(want));
		if (adds) {
			const tinted = row.classes.has("add");
			if (tinted !== adds[file].has(text) && text !== "") failures.push(layerId + " line " + file + ":" + n + (tinted ? " is tinted but the patch does not add it" : " is added by the patch but not tinted"));
		}
	}
}

checkAt(starts[0] + 8000, "act 1 verbatim", () => {
	verbatim("layer-blocked", changed, added(read("agent-change.patch")), 0);
	verbatim("layer-passed", fixed, added(read("agent-fix.patch")), 1);
});
checkAt(starts[1] + 4000, "act 2 verbatim", () => {
	verbatim("layer-blocked", {}, null, 0);
	verbatim("layer-passed", {}, null, 1);
});
checkAt(starts[2] + 4000, "act 3 verbatim", () => {
	verbatim("layer-blocked", {}, null, 0);
	verbatim("layer-passed", {}, null, 1);
});

// Every tenet fits the right pane: the marker, a gap and the id on one line,
// the gloss on the next.
const glosses = [...src.matchAll(/"([\w-]+)":\s*"([^"]+)"/g)];
for (const [, id, gloss] of glosses) {
	const room = RIGHT_PANE - PAD;
	const label = 22 + 8 + id.length * 11;
	ok(label <= room, "the label for " + id + " needs " + label.toFixed(1) + "px of " + room);
	const words = 22 + 8 + gloss.length * 16 * 0.52;
	ok(words <= room, "the gloss for " + id + " needs " + words.toFixed(1) + "px of " + room);
}

// The status line never wraps: every line it shows, at 24px semibold sans,
// about 0.58em a glyph, fits the right pane.
for (const m of src.matchAll(/(?:caption: |setTextAt\(statusEl, )"([^"]+)"/g)) {
	const width = m[1].length * 24 * 0.58;
	ok(width <= RIGHT_PANE - PAD, "the status line " + JSON.stringify(m[1]) + " needs " + width.toFixed(0) + "px of " + (RIGHT_PANE - PAD));
}

const SESSION = 1280 - PAD;
for (const m of src.matchAll(/arg: "((?:[^"\\]|\\.)*)"/g)) {
	const arg = JSON.parse('"' + m[1] + '"');
	const width = ("● Bash(" + arg + ")").length * MONO;
	ok(width <= SESSION, "the session line for " + arg + " needs " + Math.round(width) + "px of " + SESSION);
}

// ---- run ----

fn(document, window, consoleStub, setTimeoutStub, clearTimeoutStub, DateStub);

checkpoints.sort((a, b) => a.at - b.at);
let ci = 0;
const END_AT = 60000;
for (;;) {
	let next = null;
	for (const t of queue.values()) {
		if (!next || t.at < next.at || (t.at === next.at && t.seq < next.seq)) next = t;
	}
	const cpAt = ci < checkpoints.length ? checkpoints[ci].at : Infinity;
	if (!next && cpAt === Infinity) break;
	if (next && next.at <= cpAt) {
		now = next.at;
		queue.delete(next.id);
		next.fn();
		invariant();
		continue;
	}
	now = cpAt;
	checkpoints[ci].fn();
	ci += 1;
	if (now > END_AT) break;
}

console.log(logs.join("\n"));
if (failures.length) {
	console.log("\nFAIL (" + failures.length + ")");
	for (const f of failures) console.log("- " + f);
	process.exit(1);
}
console.log("\ndemo/check.js: ok (" + checkpoints.length + " checkpoints)");
