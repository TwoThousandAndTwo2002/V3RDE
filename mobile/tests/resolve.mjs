// X49x: the app's modules import each other with no extension (the bundler adds it). Under node --test this hook
// tries ".ts" for such an import (the same hook as tests/webvoice.test.mts), so a word table can live in one file
// (src/words.ts) and the other modules can import it.
import { register } from "node:module";
register("data:text/javascript," + encodeURIComponent(
  "export async function resolve(s, c, next) { if (/^\\.\\.?\\//.test(s) && !/\\.[a-z]+$/.test(s)) { try { return await next(s + '.ts', c); } catch {} } return next(s, c); }"));
