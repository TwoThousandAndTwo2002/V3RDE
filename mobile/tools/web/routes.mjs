// The routes of the V3RDE web server, written new for V3RDE (NEW_WORK_UI.md, UI 18). The same routes as the edge of
// the stack (deploy/Caddyfile on branch services, read Oct 29 at 17b4260): one origin for the app and the services,
// so the browser needs no CORS. Pure functions, so tests/web.test.mts runs them directly.

// The edge reads "7MB" and "64KB" in units of 1000 (Caddy request_body max_size, go-humanize), not 1024.
const KB = 1000;
const MB = 1000 * KB;

/**
 * The services behind /api and /v1, in the order the edge matches them: the body limit of each route and how long
 * the edge waits for the first byte of the answer (the edge's response_header_timeout; 0 = its default).
 */
export const SERVICES = [
  { name: "analyze", paths: ["/v1/analyze", "/api/analyze"], maxBody: 7 * MB, waitMs: 70_000 },
  { name: "touchup", paths: ["/api/touchup", "/v1/touchup"], maxBody: 64 * KB, waitMs: 0 },
  { name: "rescue", paths: ["/api/rescue/"], maxBody: 64 * KB, waitMs: 0 },
  { name: "fanout", paths: ["/api/fanout/"], maxBody: 64 * KB, waitMs: 0 },
  { name: "voice", exact: ["/api/voice", "/v1/voice"], maxBody: 13 * MB, waitMs: 40_000 },
  { name: "auth", paths: ["/api/auth/"], maxBody: 64 * KB, waitMs: 0 },
  { name: "listings", paths: ["/api/listings"], maxBody: 6 * MB, waitMs: 0 },
];

/** Internal routes of the post services: the edge answers 404 (their own secret, never public). */
const INTERNAL = ["/v1/fanout", "/v1/rescue", "/v1/voice/"];
/** Routes the one analysis route replaced: the edge answers 410 with the way forward. */
const REMOVED = ["/api/scan", "/api/inventory", "/v1/inventory"];

/** True when `path` is `base`, or under it (`base/...`); a base that ends in "/" matches only under it. */
function under(path, base) {
  if (base.endsWith("/")) return path.startsWith(base);
  return path === base || path.startsWith(base + "/");
}

/**
 * What the server does with a request path (the path only, no query):
 *   { kind: "service", service, maxBody, waitMs } | { kind: "internal" } | { kind: "removed" } | { kind: "health" } |
 *   { kind: "file" } (the app: a file of the web build, or index.html)
 */
export function routeOf(rawPath) {
  const path = rawPath.toLowerCase(); // the path matchers of the edge do not look at case
  if (path === "/healthz") return { kind: "health" };
  for (const s of SERVICES) {
    const hit = s.exact ? s.exact.includes(path) : s.paths.some((b) => under(path, b) || (b === "/api/listings" && path.startsWith(b)));
    if (hit) return { kind: "service", service: s.name, maxBody: s.maxBody, waitMs: s.waitMs };
  }
  if (INTERNAL.some((b) => (b.endsWith("/") ? path.startsWith(b) : under(path, b)))) return { kind: "internal" };
  if (REMOVED.some((b) => path === b)) return { kind: "removed" };
  return { kind: "file" }; // the edge also sends any other path (an unknown /api one too) to the app
}

/** The JSON answers the edge gives itself (the same text as deploy/Caddyfile). */
export const EDGE_ANSWERS = {
  internal: { status: 404, body: '{"error":"not found","code":"not_found"}' },
  removed: { status: 410, body: '{"error":"this route was removed; use POST /v1/analyze","code":"route_removed"}' },
  health: { status: 200, body: '{"ok":true,"service":"edge"}' },
};

/**
 * The Cache-Control of a file of the web build: the content-hashed files of Expo (under /_expo/static/ and
 * /assets/) are kept for a year, but only when the file is there; everything else (index.html, a missing file
 * that falls back to index.html) is never kept, so a browser never keeps the page at a file's address.
 */
export function cacheFor(path, exists) {
  const hashed = path.startsWith("/_expo/static/") || path.startsWith("/assets/");
  return hashed && exists ? "public, max-age=31536000, immutable" : "no-store";
}

/** The Content-Type of a file of the web build, by its extension. */
export function typeOf(file) {
  const ext = (file.match(/\.([a-z0-9]+)$/i)?.[1] ?? "").toLowerCase();
  return {
    html: "text/html; charset=utf-8", js: "text/javascript; charset=utf-8", mjs: "text/javascript; charset=utf-8",
    json: "application/json", css: "text/css; charset=utf-8", map: "application/json", png: "image/png", jpg: "image/jpeg",
    jpeg: "image/jpeg", ico: "image/x-icon", svg: "image/svg+xml", ttf: "font/ttf", otf: "font/otf", woff: "font/woff",
    woff2: "font/woff2", wasm: "application/wasm", txt: "text/plain; charset=utf-8", webp: "image/webp",
  }[ext] ?? "application/octet-stream";
}

/**
 * The file a request path names inside the web build, or "" when the path leaves it (.., a NUL byte, a bad
 * escape). The caller then serves index.html (the app's own router handles the path).
 */
export function fileIn(root, path) {
  let p;
  try {
    p = decodeURIComponent(path);
  } catch {
    return "";
  }
  if (p.includes("\0")) return "";
  const parts = [];
  for (const seg of p.split("/")) {
    if (seg === "" || seg === ".") continue;
    if (seg === "..") return "";
    parts.push(seg);
  }
  return parts.length ? root.replace(/\/+$/, "") + "/" + parts.join("/") : "";
}

/**
 * A Set-Cookie of the services for a plain-http local server: without `Secure` (a browser drops a Secure cookie
 * on http) and without `Domain` (the cookie then belongs to this host). Everything else stays.
 */
export function localCookie(setCookie) {
  return setCookie.split(";").map((p) => p.trim()).filter((p) => !/^secure$/i.test(p) && !/^domain=/i.test(p)).join("; ");
}

/**
 * The request headers that go to the services: no hop-by-hop headers, no Host (the services' own host is set).
 * The client address headers are set to the real address, as the edge does, so a caller cannot choose its own.
 */
const HOP = new Set(["connection", "keep-alive", "proxy-authenticate", "proxy-authorization", "te", "trailer", "transfer-encoding",
  "upgrade", "host", "proxy-connection"]);
const CLIENT = new Set(["x-forwarded-for", "cf-connecting-ip"]);
export function forwardHeaders(headers, clientIp = "") {
  const out = {};
  for (const [k, v] of Object.entries(headers)) {
    if (!HOP.has(k.toLowerCase()) && !CLIENT.has(k.toLowerCase()) && v !== undefined) out[k] = v;
  }
  if (clientIp) { out["x-forwarded-for"] = clientIp; out["cf-connecting-ip"] = clientIp; }
  return out;
}

/** The path a request names after the URL rules remove "." and ".." (what the services get), or "" for a bad URL. */
export function cleanPath(url) {
  try {
    const u = new URL(url, "http://local");
    return u.pathname + u.search;
  } catch {
    return "";
  }
}
