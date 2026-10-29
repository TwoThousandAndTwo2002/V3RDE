// Tests of the doodle automaton (src/automata.ts), written new for V3RDE (NEW_WORK_UI.md, UI 7).
import test from "node:test";
import { strict as assert } from "node:assert";
import { elementary, hash, lcg, nextRow, startGrid, stepUp, wallpaper } from "../src/automata.ts";

// Fixed values (computed once; a second, independent Python version of lcg and hash gives the same numbers).
const FIRST_LCG = [633697, 340630];
const FIRST_HASH = [91794203, 2293520258, 2072247796];
const FIRST_RULE30 = ["01101000", "11001100", "10111011"];

const P = { cell: 38, drawings: 20, jitter: 3, turn: 20, steps: 2, seed: 1026, density: 0.5 };

test("the sequence and the hash have fixed values", () => {
  const a = lcg(1026);
  assert.deepEqual([a(), a()].map((x) => Math.round(x * 1e6)), FIRST_LCG);
  assert.deepEqual([hash(0), hash(1, 2), hash(3, 4, 1)], FIRST_HASH);
});

test("rule 30 gives the known rows (from a fixed row), and the rows wrap at the edges", () => {
  const g = elementary(30, 8, 3, 1026, 0.5);
  assert.deepEqual(g.map((r) => r.join("")), FIRST_RULE30);
  const r30 = (l: number, c: number, r: number) => (30 >> ((l << 2) | (c << 1) | r)) & 1;
  for (let y = 1; y < 3; y++) g[y].forEach((v, i) => assert.equal(v, r30(g[y - 1][(i + 7) % 8], g[y - 1][i], g[y - 1][(i + 1) % 8])));
});

test("a taller grid keeps the same top rows (a keyboard does not move the wallpaper)", () => {
  assert.deepEqual(elementary(30, 11, 13, 1026, 0.5), elementary(30, 11, 22, 1026, 0.5).slice(0, 13));
});

test("the wallpaper has a drawing in every cell, near its centre, the same on each run", () => {
  const a = wallpaper(390, 844, P), b = wallpaper(390, 844, P);
  assert.deepEqual(a, b);
  assert.equal(a.length, Math.ceil(390 / 38) * Math.ceil(844 / 38));
  for (const d of a) {
    assert.ok(d.drawing >= 0 && d.drawing < 20 && [-20, -10, 0, 10, 20].includes(d.turn));
    const cx = Math.floor(d.x / 38) * 38 + 19, cy = Math.floor(d.y / 38) * 38 + 19;
    assert.ok(Math.abs(d.x - cx) <= 3 && Math.abs(d.y - cy) <= 3);
  }
  assert.deepEqual(wallpaper(0, 844, P), []);
  assert.ok(wallpaper(100, 100, { ...P, steps: 0 }).every((d) => d.turn === 0));
});

test("the live cells follow rule 30, and about half the cells are live", () => {
  const cols = Math.ceil(1440 / 38), rows = Math.ceil(900 / 38);
  const grid = elementary(30, cols, rows, 1026, 0.5);
  const d = wallpaper(1440, 900, P);
  d.forEach((m, i) => assert.equal(m.live, grid[Math.floor(i / cols)][i % cols] === 1));
  const share = d.filter((m) => m.live).length / d.length;
  assert.ok(share > 0.4 && share < 0.6, String(share));
});

test("one step moves the pattern up one row: the next window is the taller grid one row lower", () => {
  const g = elementary(30, 11, 23, 1026, 0.5), tall = elementary(30, 11, 26, 1026, 0.5);
  let w = g;
  for (let n = 1; n <= 3; n++) { w = stepUp(30, w); assert.deepEqual(w, tall.slice(n, n + 23)); }
  assert.deepEqual(nextRow(30, [0, 0, 1, 0, 0]), [0, 1, 1, 1, 0]);
  assert.deepEqual(stepUp(30, []), []);
});

test("a step changes only live and dead, not the drawings, their turns or their places", () => {
  const g0 = startGrid(390, 844, P), g1 = stepUp(30, g0);
  const a = wallpaper(390, 844, P, g0), b = wallpaper(390, 844, P, g1);
  assert.deepEqual(a.map(({ live, ...rest }) => rest), b.map(({ live, ...rest }) => rest));
  assert.ok(a.some((m, i) => m.live !== b[i].live));
  assert.deepEqual(wallpaper(390, 844, { ...P, drawings: 1 }), []);
});

test("no drawing is the same as the one to its left or above it", () => {
  const cols = Math.ceil(1440 / 38);
  const d = wallpaper(1440, 900, P);
  d.forEach((m, i) => {
    if (i % cols > 0) assert.notEqual(m.drawing, d[i - 1].drawing);
    if (i >= cols) assert.notEqual(m.drawing, d[i - cols].drawing);
  });
});

test("every drawing has a fair share on a desktop screen (chi-square, 19 degrees of freedom, 1 % level)", () => {
  const d = wallpaper(1440, 900, P);
  const counts = new Array(20).fill(0);
  for (const m of d) counts[m.drawing]++;
  const e = d.length / 20;
  const chi = counts.reduce((s, n) => s + (n - e) ** 2 / e, 0);
  assert.ok(chi < 36.19, "chi-square " + chi.toFixed(1) + " counts " + counts);
});
