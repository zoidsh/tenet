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

const PRE = 400;
const AT = [0, 16000, 28600];
const MAIN_FADE = 500;
const starts = AT.map((a) => PRE + a);

const rowsOf = (id) => el(id).children.filter((c) => c.classes.has("row"));
const allRows = () => rowsOf("layer-blocked").concat(rowsOf("layer-passed"));
const sessionLines = () => el("session").children.map((c) => c.textContent);

// item 3: nothing from either layer is on screen between the acts, and no row
// is faded in before the main area is.
// Between the acts the layers are blanked; from there until main is visible
// again no row may carry `in`, or it is on screen while the pane is empty.
let blanked = true;
function invariant() {
	if (allRows().length === 0) blanked = true;
	if (el("main").dataset.vis === "1") blanked = false;
	if (blanked) {
		const early = allRows().filter((r) => r.classes.has("in"));
		ok(early.length === 0, "a row carried `in` at " + now + " ms, before main's data-vis was 1");
	}
}

starts.slice(1).forEach((s, i) => {
	checkAt(s - 1, "act " + (i + 2) + " opens on empty layers", () => {
		eq(rowsOf("layer-blocked").length, 0, "layer-blocked was not empty at " + (s - 1) + " ms");
		eq(rowsOf("layer-passed").length, 0, "layer-passed was not empty at " + (s - 1) + " ms");
	});
});

// item 1: the two comment-led findings highlight a two-line span, marker on top.
checkAt(starts[0] + 8000, "code act spans", () => {
	const rows = rowsOf("layer-blocked");
	const hit = (id) => rows.findIndex((r) => r.dataset.hit === id);
	const lit = rows.map((r, i) => (r.classes.has("hit") ? i : -1)).filter((i) => i >= 0);
	eq(lit, [2, 3, 5, 6, 7, 11], "the wrong rows are highlighted in the code act");

	const fb = hit("no-fallback");
	ok(rows[fb - 1].classes.has("hit"), "no-fallback's comment line is not highlighted");
	eq(rows[fb - 1].querySelector(".marker").textContent, "1", "no-fallback's marker is not on its comment line");
	eq(rows[fb - 1].querySelector(".marker").dataset.shown, "1", "no-fallback's marker is not shown");
	eq(rows[fb].querySelector(".marker").textContent, "", "no-fallback's own line still carries a marker");
	eq(rows[fb].querySelector(".marker").dataset.shown, "0", "no-fallback's own line shows an empty marker");

	const cw = hit("comment-why");
	ok(rows[cw - 1].classes.has("hit"), "comment-why's comment line is not highlighted");
	eq(rows[cw - 1].querySelector(".marker").textContent, "3", "comment-why's marker is not on its comment line");
	eq(rows[cw].querySelector(".marker").textContent, "", "comment-why's own line still carries a marker");

	const mc = hit("money-in-cents");
	ok(!rows[mc - 1].classes.has("hit"), "money-in-cents highlighted a line above it");
	eq(rows[mc].querySelector(".marker").textContent, "2", "money-in-cents' marker is not on its own line");

	const nm = hit("no-mocking");
	ok(!rows[nm - 1].classes.has("hit"), "no-mocking highlighted the file row above it");
	eq(rows[nm].querySelector(".marker").textContent, "4", "no-mocking's marker is not on its own line");

	eq(el("filemeta").textContent, "northwind/invoicing · 2 files changed, +52", "act 1 file meta");
});

// item 2: one flow, pre-commit then commit-msg on the same commit.
checkAt(starts[1] - 1, "act 1 session", () => {
	eq(sessionLines(), [
		"> Add discount codes to invoice totals",
		"● Bash(git commit -m \"Update billing.go and billing_test.go\")",
		"  ⎿  tenet: 4 findings · commit blocked",
		"● fixing billing.go:28, :34, :36 and billing_test.go:36",
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
	eq(el("counter").textContent, "0.89 s", "the counter at the verdict");
	ok(/Blocked in 0\.89 s\./.test(el("headline").textContent), "the headline tail disagrees with the counter: " + el("headline").textContent);
	eq(el("status").textContent, "4 findings · blocked", "the status line");
	ok(/^4 findings from /.test(el("subline").textContent), "the subline disagrees on the count: " + el("subline").textContent);
});

checkAt(starts[0] + 15000, "passed numbers agree", () => {
	eq(el("counter").textContent, "0.80 s", "the counter at the pass");
	ok(/Passed in 0\.80 s\./.test(el("headline").textContent), "the headline tail disagrees with the counter: " + el("headline").textContent);
	eq(el("status").textContent, "0 findings · passed", "the status line");
});

// item 4: the cost is a stat of its own, six decimals, no "per" anything.
checkAt(starts[0] + 5000, "cost stat", () => {
	eq(el("cost").textContent, "$0.000318", "the blocked cost");
	eq(el("cost-caption").textContent, "cost", "the cost caption");
	eq(el("clock-caption").textContent, "seconds, this commit", "the seconds caption");
	ok(!/per/.test(el("cost").textContent), "the cost line still says \"per\"");
});

checkAt(starts[0] + 15000, "passed cost stat", () => {
	eq(el("cost").textContent, "$0.000190", "the passed cost");
});

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

// Nothing is set under 16px, and the two labels the data makes longest fit
// their boxes: a mono glyph is 0.6em, a sans one averages about 0.52em.
const RIGHT_PANE = 1280 - 640 - 24 * 2;
const glosses = [...src.matchAll(/"([\w-]+)":\s*"([^"]+)"/g)];
for (const [, id, gloss] of glosses) {
	const width = 22 + 8 + id.length * 18 * 0.6 + 8 + 4 + gloss.length * 16 * 0.52;
	ok(width < RIGHT_PANE, "the label for " + id + " needs " + Math.round(width) + "px of " + RIGHT_PANE);
}
const SESSION = 1280 - 24 * 2;
for (const m of src.matchAll(/arg: "([^"]*)"/g)) {
	const width = ("● Bash(" + m[1] + ")").length * 16 * 0.6;
	ok(width < SESSION, "the session line for " + m[1] + " needs " + Math.round(width) + "px of " + SESSION);
}

console.log(logs.join("\n"));
if (failures.length) {
	console.log("\nFAIL (" + failures.length + ")");
	for (const f of failures) console.log("- " + f);
	process.exit(1);
}
console.log("\ndemo/check.js: ok (" + checkpoints.length + " checkpoints)");
