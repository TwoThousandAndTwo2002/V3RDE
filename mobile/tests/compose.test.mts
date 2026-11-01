// X54 (security F10): the Go services of the stack (deploy/docker-compose.yml) drop all Linux capabilities and
// cannot gain new privileges. Written new for V3RDE (NEW_WORK.md 100).
import test from "node:test";
import { strict as assert } from "node:assert";
import fs from "node:fs";

const compose = fs.readFileSync(new URL("../../deploy/docker-compose.yml", import.meta.url), "utf8");
const dockerfile = (p: string) => fs.readFileSync(new URL("../../" + p, import.meta.url), "utf8");

/** The text of each top-level block: an x- anchor ("x-post") or a service ("auth"). */
function blocks(): Map<string, string> {
  const out = new Map<string, string>();
  const services = compose.indexOf("\nservices:\n");
  for (const m of compose.matchAll(/^(x-[a-z-]+):[^\n]*\n((?:[ #][^\n]*\n|\n)*)/gm)) out.set(m[1], m[2]);
  const body = compose.slice(services + "\nservices:\n".length);
  for (const m of body.matchAll(/^  ([a-z-]+):\n((?:(?:    |\s*#|\s*$)[^\n]*\n)*)/gm)) out.set(m[1], m[2]);
  return out;
}

const HARD = ['security_opt: ["no-new-privileges:true"]', "cap_drop: [ALL]"];
const hardened = (text: string) => HARD.every((h) => text.includes(h));

test("every Go service drops all capabilities and gets no new privileges", () => {
  const b = blocks();
  assert.ok(hardened(b.get("x-hardened")!) && hardened(b.get("x-sidecar")!) && hardened(b.get("x-post")!));
  const own = ["auth", "listings", "barcode", "gate", "analyze"];
  for (const s of own) assert.ok(b.get(s)!.includes("<<: *hardened"), s);
  for (const s of ["touchup", "rescue", "fanout", "voice", "push"]) assert.ok(b.get(s)!.includes("<<: *post"), s);
  const sidecars = [...b.keys()].filter((k) => k.startsWith("sidecar-"));
  assert.equal(sidecars.length, 9);
  for (const s of [...sidecars, "ledger"]) assert.ok(b.get(s)!.includes("<<: *sidecar"), s);
  // the only services left as they were: caddy (root, port 80) and the lif job (root)
  const all = [...b.keys()].filter((k) => !k.startsWith("x-"));
  assert.deepEqual(all.filter((s) => !own.includes(s) && !/^(touchup|rescue|fanout|voice|push|ledger|sidecar-)/.test(s)).sort(), ["caddy", "lif"]);
});

test("the hardened images run as uid 65534 (no capability is needed)", () => {
  for (const p of ["auth/Dockerfile", "listings/Dockerfile", "barcode/Dockerfile", "gate/Dockerfile", "inference/Dockerfile",
    "post/Dockerfile", "telemetry/Dockerfile"]) {
    assert.match(dockerfile(p), /^USER 65534:65534$/m, p);
  }
});
