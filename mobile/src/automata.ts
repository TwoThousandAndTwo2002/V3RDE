// The cellular automaton that places the food doodles. Written new for V3RDE (NEW_WORK_UI.md, UI 7;
// mobile/docs/style_guide.md "Doodles"). Pure functions: the same input gives the same picture on every device.

/** A grid of cells: rows of 0 (dead) and 1 (live). */
export type Grid = number[][];

/** A pseudo-random sequence in [0, 1): a linear congruential generator (Numerical Recipes constants). */
export function lcg(seed: number): () => number {
  let s = seed >>> 0;
  return () => {
    s = (Math.imul(1664525, s) + 1013904223) >>> 0;
    return s / 4294967296;
  };
}

/**
 * FNV-1a over small integers, then the murmur3 finalizer (fmix32), so that every bit of the result depends on
 * every bit of the input: a stable 32-bit hash of a cell.
 */
export function hash(...xs: number[]): number {
  let h = 2166136261;
  for (const x of xs) h = Math.imul(h ^ (x >>> 0), 16777619) >>> 0;
  h ^= h >>> 16;
  h = Math.imul(h, 0x85ebca6b) >>> 0;
  h ^= h >>> 13;
  h = Math.imul(h, 0xc2b2ae35) >>> 0;
  return (h ^ (h >>> 16)) >>> 0;
}

/**
 * An elementary cellular automaton (Wolfram rule 0-255). Row 0 is random (from `seed`, `density` of the cells
 * live); each next row is the next generation. The left and right edges wrap. A row depends only on the rows
 * above it, so a taller grid keeps the same top rows.
 */
export function elementary(rule: number, cols: number, rows: number, seed: number, density: number): Grid {
  const next = lcg(seed);
  let row: number[] = Array.from({ length: cols }, () => (next() < density ? 1 : 0));
  const grid: Grid = [row];
  for (let y = 1; y < rows; y++) {
    const up = row;
    row = nextRow(rule, up);
    grid.push(row);
  }
  return grid;
}

/** The next generation of an elementary rule from one row (the edges wrap). */
export function nextRow(rule: number, up: number[]): number[] {
  const n = up.length;
  return up.map((_, i) => (rule >> ((up[(i + n - 1) % n] << 2) | (up[i] << 1) | up[(i + 1) % n])) & 1);
}

/** The next window of `grid`: the top row goes, and the next generation of the bottom row comes in. */
export function stepUp(rule: number, grid: Grid): Grid {
  return grid.length === 0 ? grid : [...grid.slice(1), nextRow(rule, grid[grid.length - 1])];
}

/** One doodle: the centre of its drawing (pt), which drawing, its turn (degrees), and live (strong) or dead (faint). */
export interface Doodle { x: number; y: number; drawing: number; turn: number; live: boolean }

/** How to make the wallpaper. */
export interface Wallpaper {
  cell: number;      // the cell size (pt)
  drawings: number;  // how many drawings there are (2 or more)
  jitter: number;    // the largest shift from the cell centre (pt)
  turn: number;      // the largest turn (degrees)
  steps: number;     // turn steps on each side of 0 (0: no turn)
  seed: number;      // the start of the automaton
  density: number;   // the share of live cells in its first row
}

/** The first rule-30 window of a `w` x `h` pt area (one row per cell row). */
export function startGrid(w: number, h: number, p: Wallpaper): Grid {
  const cols = Math.ceil(w / p.cell), rows = Math.ceil(h / p.cell);
  return cols > 0 && rows > 0 ? elementary(30, cols, rows, p.seed, p.density) : [];
}

// Salts of the hash, so that the shift, the turn and the pick of a cell do not depend on each other.
const SALT_SHIFT_X = 1, SALT_SHIFT_Y = 2, SALT_TURN = 3, SALT_PICK = 4;

/**
 * The doodles of a `w` x `h` pt area: a drawing in every cell; `grid` (a rule-30 window, one row per cell row)
 * makes each cell live or dead. The hash picks the drawing; when the pick is the same as the drawing to the left
 * or above, the next drawing is taken, so no two neighbours are the same. Fewer than 2 drawings: no doodles.
 */
export function wallpaper(w: number, h: number, p: Wallpaper, grid = startGrid(w, h, p)): Doodle[] {
  const cols = Math.ceil(w / p.cell), rows = Math.ceil(h / p.cell);
  if (cols <= 0 || rows <= 0 || p.drawings < 2) return [];
  const span = 2 * p.jitter + 1, turns = 2 * p.steps + 1;
  const picks: number[][] = [];
  const out: Doodle[] = [];
  grid.forEach((line, y) => {
    picks.push([]);
    line.forEach((alive, x) => {
      const left = x > 0 ? picks[y][x - 1] : -1, up = y > 0 ? picks[y - 1][x] : -1;
      let pick = hash(x, y, SALT_PICK) % p.drawings;
      while (pick === left || pick === up) pick = (pick + 1) % p.drawings;
      picks[y].push(pick);
      out.push({
        x: x * p.cell + p.cell / 2 + (hash(x, y, SALT_SHIFT_X) % span) - p.jitter,
        y: y * p.cell + p.cell / 2 + (hash(x, y, SALT_SHIFT_Y) % span) - p.jitter,
        drawing: pick,
        turn: p.steps > 0 ? ((hash(x, y, SALT_TURN) % turns) - p.steps) * (p.turn / p.steps) : 0,
        live: alive === 1,
      });
    });
  });
  return out;
}
