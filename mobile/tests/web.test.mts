// Tests of the web server (tools/web/routes.mjs, tools/web/serve.mjs), written new for V3RDE (NEW_WORK_UI.md, UI 18).
// The routes are those of the edge of the stack (deploy/Caddyfile on branch services); the server runs here with a
// stand-in for the services on a free port.
import test from "node:test";
import { strict as assert } from "node:assert";
import http from "node:http";
import fs from "node:fs";
import os from "node:os";
import path from "node:path";
import type { AddressInfo } from "node:net";
// @ts-ignore -- a plain ES module of the web server (no types)
import { cacheFor, cleanPath, fileIn, forwardHeaders, localCookie, routeOf, typeOf } from "../tools/web/routes.mjs";
// @ts-ignore
import { createWebServer } from "../tools/web/serve.mjs";

test("the routes are the routes of the edge", () => {
  const svc = (p: string) => { const r = routeOf(p); return r.kind === "service" ? r.service : r.kind; };
  assert.equal(svc("/api/analyze"), "analyze");
  assert.equal(svc("/api/analyze/scan/abc"), "analyze");
  assert.equal(svc("/v1/analyze"), "analyze");
  assert.equal(svc("/api/analyzer"), "file"); // not under /api/analyze
  assert.equal(svc("/api/touchup"), "touchup");
  assert.equal(svc("/api/touchup/batch"), "touchup");
  assert.equal(svc("/api/rescue/l1/send"), "rescue");
  assert.equal(svc("/api/rescue"), "file"); // the edge matches only /api/rescue/*
  assert.equal(svc("/api/fanout/l1"), "fanout");
  assert.equal(svc("/api/voice"), "voice");
  assert.equal(svc("/v1/voice"), "voice");
  assert.equal(svc("/v1/voice/x"), "internal");
  assert.equal(svc("/v1/fanout/status"), "internal");
  assert.equal(svc("/v1/rescue/l1"), "internal");
  assert.equal(svc("/api/auth/me"), "auth");
  assert.equal(svc("/api/listings"), "listings");
  assert.equal(svc("/api/listings/mine"), "listings");
  assert.equal(svc("/api/scan"), "removed");
  assert.equal(svc("/healthz"), "health");
  assert.equal(svc("/reset"), "file");
  assert.equal(svc("/API/Auth/me"), "auth"); // the edge does not look at case
  assert.equal(routeOf("/api/analyze").maxBody, 7_000_000); // "7MB" at the edge = 7 000 000 bytes
  assert.equal(routeOf("/api/auth/login").maxBody, 64_000);
  assert.equal(routeOf("/api/voice").waitMs, 40_000);
});

test("cache rules, content types and safe paths", () => {
  assert.equal(cacheFor("/_expo/static/js/web/index-1.js", true), "public, max-age=31536000, immutable");
  assert.equal(cacheFor("/_expo/static/js/web/gone.js", false), "no-store");
  assert.equal(cacheFor("/index.html", true), "no-store");
  assert.equal(typeOf("a/b.js"), "text/javascript; charset=utf-8");
  assert.equal(typeOf("font.TTF"), "font/ttf");
  assert.equal(typeOf("x.unknown"), "application/octet-stream");
  assert.equal(fileIn("/r", "/a/b.js"), "/r/a/b.js");
  assert.equal(fileIn("/r/", "/a/./b.js"), "/r/a/b.js");
  assert.equal(fileIn("/r", "/a/../../etc/passwd"), "");
  assert.equal(fileIn("/r", "/%2e%2e/etc/passwd"), "");
  assert.equal(fileIn("/r", "/%E0%A4%A"), "");
  assert.equal(fileIn("/r", "/"), "");
});

test("cookies and headers for a plain-http local server", () => {
  assert.equal(localCookie("s=abc; Path=/; HttpOnly; Secure; SameSite=Lax; Domain=example.org"), "s=abc; Path=/; HttpOnly; SameSite=Lax");
  const h = forwardHeaders({ host: "x", connection: "keep-alive", cookie: "s=1", "content-type": "a/b" });
  assert.deepEqual(h, { cookie: "s=1", "content-type": "a/b" });
  const ip = forwardHeaders({ "x-forwarded-for": "6.6.6.6", "CF-Connecting-IP": "6.6.6.6", cookie: "s=1" }, "127.0.0.1");
  assert.deepEqual(ip, { cookie: "s=1", "x-forwarded-for": "127.0.0.1", "cf-connecting-ip": "127.0.0.1" });
  assert.equal(cleanPath("/api/analyze/../../v1/fanout/status?x=1"), "/v1/fanout/status?x=1");
  assert.equal(cleanPath("/api/analyze/%2e%2e/%2e%2e/v1/rescue/l1"), "/v1/rescue/l1");
  assert.equal(cleanPath("//other.host/api/auth/me"), "/api/auth/me"); // never another host
});

type Seen = { method: string; url: string; host: string; body: number; cookie: string; xff: string };

async function stack(waitMs = 0) {
  const root = fs.mkdtempSync(path.join(os.tmpdir(), "v3rde-web-"));
  fs.mkdirSync(path.join(root, "_expo/static/js/web"), { recursive: true });
  fs.writeFileSync(path.join(root, "index.html"), "<html>app</html>");
  fs.writeFileSync(path.join(root, "_expo/static/js/web/index-1.js"), "console.log(1)");
  const seen: Seen[] = [];
  const services = http.createServer((req, res) => {
    let n = 0;
    req.on("data", (c) => { n += c.length; });
    req.on("end", () => {
      seen.push({ method: req.method ?? "", url: req.url ?? "", host: req.headers.host ?? "", body: n, cookie: String(req.headers.cookie ?? ""),
        xff: String(req.headers["x-forwarded-for"] ?? "") });
      if (req.url === "/api/analyze/slow") { // the first byte at once, the end after 600 ms
        res.writeHead(200, { "Content-Type": "application/json" });
        res.write('{"a":');
        return setTimeout(() => res.end("1}"), 600);
      }
      if (req.url === "/api/analyze/late") return setTimeout(() => { res.writeHead(200); res.end("{}"); }, 600);
      if (req.url === "/api/listings/cut") { // says 100 bytes, sends 7, then the connection breaks
        res.writeHead(200, { "Content-Length": "100", "Content-Type": "image/jpeg" });
        res.write("partial");
        return setTimeout(() => res.socket?.destroy(), 50);
      }
      if (req.url === "/api/auth/login") {
        res.writeHead(200, { "Content-Type": "application/json", "Set-Cookie": "sess=abc; Path=/; HttpOnly; Secure; SameSite=Lax" });
        return res.end('{"id":"u1","email":"a@b.co"}');
      }
      res.writeHead(201, { "Content-Type": "application/json" });
      res.end('{"ok":true}');
    });
  });
  await new Promise<void>((go) => services.listen(0, "127.0.0.1", () => go()));
  const api = "http://127.0.0.1:" + (services.address() as AddressInfo).port;
  const web = createWebServer({ root, api, waitMs });
  await new Promise<void>((go) => web.listen(0, "127.0.0.1", () => go()));
  const base = "http://127.0.0.1:" + (web.address() as AddressInfo).port;
  const close = () => { web.close(); services.close(); fs.rmSync(root, { recursive: true, force: true }); };
  return { base, seen, close };
}

test("the server: the app, its fallback, the services and the edge answers", async () => {
  process.env.V3RDE_WEB_QUIET = "1";
  const s = await stack();
  try {
    let r = await fetch(s.base + "/");
    assert.equal(r.status, 200);
    assert.equal(await r.text(), "<html>app</html>");
    assert.equal(r.headers.get("cache-control"), "no-store");
    r = await fetch(s.base + "/reset");
    assert.equal(await r.text(), "<html>app</html>"); // a path that is not a file: the app
    r = await fetch(s.base + "/_expo/static/js/web/index-1.js");
    assert.equal(r.headers.get("cache-control"), "public, max-age=31536000, immutable");
    assert.equal(r.headers.get("content-type"), "text/javascript; charset=utf-8");
    r = await fetch(s.base + "/_expo/static/js/web/old.js");
    assert.equal(r.headers.get("cache-control"), "no-store"); // a missing hashed file is never kept a year
    r = await fetch(s.base + "/..%2f..%2fetc/passwd");
    assert.equal(await r.text(), "<html>app</html>");

    r = await fetch(s.base + "/api/auth/login", { method: "POST", body: '{"email":"a@b.co"}', headers: { "Content-Type": "application/json" } });
    assert.equal(r.status, 200);
    assert.equal(r.headers.get("set-cookie"), "sess=abc; Path=/; HttpOnly; SameSite=Lax");
    r = await fetch(s.base + "/api/listings?after=x", { method: "POST", body: "12345", headers: { cookie: "sess=abc" } });
    assert.equal(r.status, 201);
    const last = s.seen[s.seen.length - 1];
    assert.deepEqual([last.method, last.url, last.body, last.cookie], ["POST", "/api/listings?after=x", 5, "sess=abc"]);
    assert.match(last.host, /^127\.0\.0\.1:\d+$/);
    assert.equal(last.xff, "127.0.0.1");
    const raw = await new Promise<number>((go, fail) => { // fetch would clean the path itself; send it as it is
      const q = http.request(s.base + "/", { method: "POST", path: "/api/analyze/../../v1/fanout/status" }, (a) => { a.resume(); go(a.statusCode ?? 0); });
      q.on("error", fail); q.end("{}");
    });
    assert.equal(raw, 404); // ".." cannot reach an internal route
    assert.equal(s.seen.filter((x) => x.url.includes("fanout")).length, 0);

    r = await fetch(s.base + "/api/auth/login", { method: "POST", body: "x".repeat(70 * 1024) });
    assert.equal(r.status, 413); // the edge's limit for auth is 64 KB
    r = await fetch(s.base + "/v1/fanout/status", { method: "POST", body: "{}" });
    assert.equal(r.status, 404);
    assert.equal((await r.json()).code, "not_found");
    r = await fetch(s.base + "/api/scan", { method: "POST" });
    assert.equal(r.status, 410);
    r = await fetch(s.base + "/healthz");
    assert.equal((await r.json()).service, "edge");
    r = await fetch(s.base + "/", { method: "DELETE" });
    assert.equal(r.status, 405);
  } finally {
    s.close();
  }
});

test("the wait: only for the first byte; an answer cut in the middle ends the browser's answer too", async () => {
  process.env.V3RDE_WEB_QUIET = "1";
  const s = await stack(300);
  try {
    let r = await fetch(s.base + "/api/analyze/slow");
    assert.equal(await r.text(), '{"a":1}'); // 600 ms in all, longer than the 300 ms wait: not cut
    r = await fetch(s.base + "/api/analyze/late");
    assert.equal(r.status, 504);
    assert.equal((await r.json()).code, "edge_timeout");
    r = await fetch(s.base + "/api/listings/cut");
    await assert.rejects(r.arrayBuffer()); // an error, not a wait with no end
  } finally {
    s.close();
  }
});

test("without a services address /api answers 502, the app still loads", async () => {
  process.env.V3RDE_WEB_QUIET = "1";
  const root = fs.mkdtempSync(path.join(os.tmpdir(), "v3rde-web-"));
  fs.writeFileSync(path.join(root, "index.html"), "<html>app</html>");
  const web = createWebServer({ root, api: "" });
  await new Promise<void>((go) => web.listen(0, "127.0.0.1", () => go()));
  const base = "http://127.0.0.1:" + (web.address() as AddressInfo).port;
  try {
    assert.equal((await fetch(base + "/")).status, 200);
    const r = await fetch(base + "/api/auth/me");
    assert.equal(r.status, 502);
    assert.equal((await r.json()).code, "no_upstream");
  } finally {
    web.close();
    fs.rmSync(root, { recursive: true, force: true });
  }
});
