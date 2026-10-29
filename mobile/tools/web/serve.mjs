#!/usr/bin/env node
// The V3RDE web server, written new for V3RDE (NEW_WORK_UI.md, UI 18; mobile/docs/web.md). It serves the web
// build of the app (mobile/web-dist, made by `npm run web:build`) and sends /api and /v1 to the services, with the
// routes, body limits and cache rules of the edge of the stack (tools/web/routes.mjs), so a browser sees the same
// single origin as in production. Node's standard library only.
//
//   npm run web:serve                                  # http://127.0.0.1:8090, the services at $V3RDE_API_BASE
//   node tools/web/serve.mjs --api https://host --port 8090 --root web-dist
//
// On plain http (this server) a browser drops a Secure cookie, so the session cookie loses Secure and Domain on
// the way back (localCookie). Nothing is cached or logged except the method, the path and the status.
import http from "node:http";
import https from "node:https";
import fs from "node:fs";
import path from "node:path";
import { pipeline } from "node:stream";
import { fileURLToPath } from "node:url";
import { EDGE_ANSWERS, cacheFor, cleanPath, fileIn, forwardHeaders, localCookie, routeOf, typeOf } from "./routes.mjs";

const MOBILE = path.resolve(path.dirname(fileURLToPath(import.meta.url)), "..", "..");

/** Sends a short JSON answer. */
function answer(res, status, body, extra = {}) {
  res.writeHead(status, { "Content-Type": "application/json", "Content-Length": Buffer.byteLength(body), "Cache-Control": "no-store", ...extra });
  res.end(body);
}

/** A file of the web build, or index.html for a path that is not a file (the app routes it). */
function serveFile(root, req, res) {
  const want = fileIn(root, req.url.split("?")[0]);
  let file = want;
  let exists = false;
  try {
    exists = !!want && fs.statSync(want).isFile();
  } catch {
    exists = false;
  }
  if (!exists) file = path.join(root, "index.html");
  fs.stat(file, (err, st) => {
    if (err || !st.isFile()) return answer(res, 404, '{"error":"no web build: run npm run web:build","code":"no_web_build"}');
    res.writeHead(200, { "Content-Type": typeOf(file), "Content-Length": st.size, "Cache-Control": cacheFor(req.url.split("?")[0], exists) });
    if (req.method === "HEAD") return res.end();
    pipeline(fs.createReadStream(file), res, () => {}); // a read error or a browser that went away: both streams close
  });
}

/** Sends the request on to the services and streams the answer back. `target` = the clean path and query. */
function forward(api, route, target, req, res) {
  if (!api) return answer(res, 502, '{"error":"no services address: set V3RDE_API_BASE or --api","code":"no_upstream"}');
  const size = Number(req.headers["content-length"] || 0);
  if (size > route.maxBody) {
    req.resume();
    return answer(res, 413, '{"error":"request body too large","code":"too_large"}', { Connection: "close" });
  }
  const url = new URL(api + target);
  const lib = url.protocol === "https:" ? https : http;
  let timer = null;
  let answered = false;
  let sent = 0;
  const stopTimer = () => { if (timer) clearTimeout(timer); timer = null; };
  const out = lib.request(url, { method: req.method, headers: { ...forwardHeaders(req.headers, req.socket.remoteAddress ?? ""), host: url.host } }, (up) => {
    answered = true;
    stopTimer();
    const headers = { ...up.headers };
    delete headers["transfer-encoding"];
    delete headers.connection;
    const cookies = up.headers["set-cookie"];
    if (cookies) headers["set-cookie"] = cookies.map(localCookie);
    res.writeHead(up.statusCode ?? 502, headers);
    pipeline(up, res, () => {}); // an answer cut in the middle closes the browser connection too
  });
  // the edge waits this long for the first byte of the answer once the request is sent (analyze 70 s, voice 40 s)
  if (route.waitMs) out.on("finish", () => { if (!answered) timer = setTimeout(() => out.destroy(new Error("timeout")), route.waitMs); });
  out.on("error", (e) => {
    stopTimer();
    if (res.headersSent) return res.destroy();
    const gate = e.message === "timeout";
    answer(res, gate ? 504 : 502, gate ? '{"error":"the service did not answer in time","code":"edge_timeout"}'
      : '{"error":"the services cannot be reached","code":"upstream_unreachable"}');
  });
  // the browser went away (an upload stopped, a page closed): the services stop too, as behind the edge
  res.on("close", () => { stopTimer(); if (!res.writableFinished) out.destroy(); });
  req.on("data", (chunk) => {
    sent += chunk.length;
    if (sent > route.maxBody) { // a body with no Content-Length (chunked) that is too large
      out.destroy();
      if (!res.headersSent) answer(res, 413, '{"error":"request body too large","code":"too_large"}', { Connection: "close" });
      req.destroy();
    }
  });
  req.pipe(out);
}

/** The server: `root` = the web build, `api` = the services' base URL ("" = not set). `waitMs` (tests only)
 *  replaces the wait of every route that has one. */
export function createWebServer({ root, api, waitMs = 0 }) {
  const base = api ? api.replace(/\/+$/, "") : "";
  return http.createServer((req, res) => {
    const started = Date.now();
    res.on("finish", () => {
      if (process.env.V3RDE_WEB_QUIET !== "1") process.stdout.write(`   ${req.method} ${req.url.split("?")[0]} ${res.statusCode} ${Date.now() - started} ms\n`);
    });
    // the route is chosen from the path the services would get ("." and ".." removed), so the two never differ
    const target = cleanPath(req.url);
    const route = target ? routeOf(target.split("?")[0]) : { kind: "file" };
    if (route.kind === "service") return forward(base, waitMs && route.waitMs ? { ...route, waitMs } : route, target, req, res);
    if (route.kind === "internal" || route.kind === "removed" || route.kind === "health") {
      const a = EDGE_ANSWERS[route.kind];
      return answer(res, a.status, a.body);
    }
    if (req.method !== "GET" && req.method !== "HEAD") return answer(res, 405, '{"error":"method not allowed","code":"method_not_allowed"}');
    serveFile(root, req, res);
  });
}

function main(argv) {
  const opt = { root: path.join(MOBILE, "web-dist"), api: process.env.V3RDE_API_BASE ?? "", port: 8090, host: "127.0.0.1" };
  for (let i = 0; i < argv.length; i++) {
    const [k, v] = [argv[i], argv[i + 1]];
    if (k === "--root") { opt.root = path.resolve(v); i++; }
    else if (k === "--api") { opt.api = v; i++; }
    else if (k === "--port") { opt.port = Number(v); i++; }
    else if (k === "--host") { opt.host = v; i++; }
    else { process.stderr.write("usage: serve.mjs [--root web-dist] [--api https://host] [--port 8090] [--host 127.0.0.1]\n"); process.exit(2); }
  }
  if (!fs.existsSync(path.join(opt.root, "index.html"))) {
    process.stderr.write(`no ${opt.root}/index.html: run npm run web:build first\n`);
    process.exit(1);
  }
  if (opt.api && !/^https?:\/\/[^/]+/.test(opt.api)) {
    process.stderr.write("--api must be http(s)://host\n");
    process.exit(2);
  }
  createWebServer(opt).listen(opt.port, opt.host, () => {
    process.stdout.write(`V3RDE web  http://${opt.host}:${opt.port}   app: ${opt.root}   services: ${opt.api || "(not set: /api answers 502)"}\n`);
  });
}

if (process.argv[1] && path.resolve(process.argv[1]) === fileURLToPath(import.meta.url)) main(process.argv.slice(2));
