// X53 (security F4): the security headers of the edge (deploy/Caddyfile). Written new for V3RDE (NEW_WORK.md 99).
import test from "node:test";
import { strict as assert } from "node:assert";
import fs from "node:fs";
import crypto from "node:crypto";
import path from "node:path";
import { fileURLToPath } from "node:url";

const caddy = fs.readFileSync(new URL("../../deploy/Caddyfile", import.meta.url), "utf8");

/** The lines of the first `header {` block of the :80 site (the one for every answer). */
function siteHeaders(): string[] {
  const site = caddy.slice(caddy.indexOf("\n:80 {"));
  const m = site.match(/\n\theader \{\n([\s\S]*?)\n\t\}/);
  assert.ok(m, "the :80 site has a header block");
  return m[1].split("\n").map((l) => l.trim()).filter(Boolean);
}

test("every answer of the edge has the security headers", () => {
  const h = siteHeaders();
  for (const want of ["X-Content-Type-Options nosniff", "Referrer-Policy strict-origin-when-cross-origin",
    "X-Frame-Options SAMEORIGIN", 'Strict-Transport-Security "max-age=86400"']) {
    assert.ok(h.includes(want), want);
  }
  // X53 review: a "-" line defers the whole block, and a deferred block is not run on an error answer (a 502)
  assert.ok(!h.some((l) => l.startsWith("-")), "no delete in the block");
});

test("the edge does not name itself, also in its error answers", () => {
  const site = caddy.slice(caddy.indexOf("\n:80 {"));
  assert.match(site, /\n\theader -Server\n/);
  const errors = site.match(/\n\thandle_errors \{\n([\s\S]*?)\n\t\}/);
  assert.ok(errors, "a handle_errors block");
  assert.deepEqual(errors[1].split("\n").map((l) => l.trim()), ["header -Server", 'respond "" {err.status_code}']);
});

test("the CSP is Report-Only and closes the risky directives", () => {
  const line = siteHeaders().find((l) => l.startsWith("Content-Security-Policy"));
  assert.ok(line, "a CSP line");
  assert.ok(line.startsWith('Content-Security-Policy-Report-Only "'), "Report-Only until the reports are checked");
  const csp = line.slice(line.indexOf('"') + 1, -1);
  const dir = new Map(csp.split(";").map((d) => d.trim().split(/\s+/)).map(([k, ...v]) => [k, v]));
  assert.deepEqual(dir.get("object-src"), ["'none'"]);
  assert.deepEqual(dir.get("base-uri"), ["'self'"]);
  assert.deepEqual(dir.get("frame-ancestors"), ["'self'"]);
  assert.ok(!dir.get("script-src")!.includes("'unsafe-inline'"), "no inline script");
  assert.ok(dir.get("script-src")!.includes("https://*.googleapis.com"), "the map script (src/maps.ts)");
  assert.ok(dir.get("img-src")!.includes("blob:"), "the photo previews (URL.createObjectURL)");
});

/** Every .html file under a folder. */
function htmlFiles(dir: string): string[] {
  return fs.readdirSync(dir, { withFileTypes: true }).flatMap((e) => {
    const p = path.join(dir, e.name);
    return e.isDirectory() ? htmlFiles(p) : e.name.endsWith(".html") ? [p] : [];
  });
}

test("every inline script of the web pages is allowed by its hash (script-src has no 'unsafe-inline')", () => {
  const line = siteHeaders().find((l) => l.startsWith("Content-Security-Policy"))!;
  const scriptSrc = line.split(";").find((d) => d.includes("script-src"))!;
  const pages = htmlFiles(fileURLToPath(new URL("../web-dist/", import.meta.url)));
  assert.ok(pages.some((p) => p.endsWith(path.join("about", "index.html"))), "the about page is in the web build");
  let inline = 0;
  for (const p of pages) {
    for (const [, attrs, body] of fs.readFileSync(p, "utf8").matchAll(/<script\b([^>]*)>([\s\S]*?)<\/script>/g)) {
      if (/\bsrc=/.test(attrs) && body.trim() === "") continue;
      inline++;
      const hash = "'sha256-" + crypto.createHash("sha256").update(body, "utf8").digest("base64") + "'";
      assert.ok(scriptSrc.includes(hash), `${path.basename(path.dirname(p))}/${path.basename(p)}: an inline script whose hash ${hash} is not in script-src`);
    }
  }
  assert.ok(inline >= 1, "the about page doodles");
});
