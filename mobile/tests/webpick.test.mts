// X50c (T9 Nov 1: upload of an older photo on an Android phone): the web photo picker (src/webpick.ts).
import test from "node:test";
import { strict as assert } from "node:assert";
import { readFileSync } from "node:fs";
import { canDecode, isImageFile, pickImageWeb } from "../src/webpick.ts";

function fakeDoc() {
  const log: string[] = [];
  const fields: any[] = [];
  const doc = {
    body: { appendChild: (el: any) => { log.push("append"); fields.push(el); } },
    createElement: (tag: string) => {
      const ls: Record<string, () => void> = {};
      const el: any = { tag, style: {}, files: null as any, attrs: {} as Record<string, string>,
        setAttribute: (k: string, v: string) => { el.attrs[k] = v; },
        addEventListener: (t: string, f: () => void) => { ls[t] = f; },
        click: () => log.push("click"), remove: () => log.push("remove"), fire: (t: string) => ls[t]?.() };
      return el;
    },
  };
  return { doc: doc as unknown as Document, log, fields };
}

test("a real field gets a real click in the tap; the chosen file comes back; cancel gives null", async () => {
  const { doc, log, fields } = fakeDoc();
  const p = pickImageWeb(doc);
  assert.deepEqual(log, ["append", "click"]); // in the same tap: no await before the click
  const f = fields[0];
  assert.equal(f.type, "file");
  assert.equal(f.accept, "image/*");
  assert.doesNotMatch(f.style.cssText, /display:\s*none/); // some Android web views do not open a hidden field
  assert.match(f.style.cssText, /opacity:0/);
  f.files = [{ name: "a.jpg", type: "image/jpeg" }];
  f.fire("change");
  assert.equal((await p)?.name, "a.jpg");
  assert.deepEqual(log.slice(-1), ["remove"]);
  const q = pickImageWeb(doc);
  fields[1].fire("cancel");
  assert.equal(await q, null);
});

test("a field that never answered is closed by the next tap", async () => {
  const { doc, fields } = fakeDoc();
  const first = pickImageWeb(doc);
  const second = pickImageWeb(doc);
  assert.equal(await first, null);
  assert.equal(fields.length, 2); // the old field was ended (null) when the new one opened
  fields[1].files = [{ name: "b.jpg", type: "" }];
  fields[1].fire("change");
  assert.equal((await second)?.name, "b.jpg");
});

test("the file types the upload tries", () => {
  assert.equal(isImageFile({ type: "image/jpeg" }), true);
  assert.equal(isImageFile({ type: "image/heic" }), true);
  assert.equal(isImageFile({ type: "" }), true); // some galleries give no type
  assert.equal(isImageFile({ type: "video/mp4" }), false);
  assert.equal(isImageFile(null), false);
});

test("X50c review: a photo the browser cannot read is refused before the upload; no decoder: the upload decides", async () => {
  let closed = 0;
  assert.equal(await canDecode(new Blob(["x"]), async () => ({ close: () => { closed++; } })), true);
  assert.equal(closed, 1); // the decoded bitmap is closed
  assert.equal(await canDecode(new Blob(["x"]), async () => { throw new Error("HEIC"); }), false);
  assert.equal(await canDecode(new Blob(["x"]), undefined), true);
});

test("the camera screen uses it on the web, and frees the file's address later", () => {
  const cap = readFileSync(new URL("../src/screens/Capture.tsx", import.meta.url), "utf8");
  assert.match(cap, /if \(RN\.Platform\.OS === "web"\) \{ \/\/ X50c[^\n]*\n\s*const f = await pickImageWeb\(\);/);
  assert.match(cap, /if \(!isImageFile\(f\) \|\| !\(await canDecode\(f\)\)\) return setProblem\(CAPTURE\.shotFailed\);/);
  assert.match(cap, /await take\(keep\(URL\.createObjectURL\(f\)\)\);/);
  assert.match(cap, /canDecode\(file\)\.then\(\(ok\) => \(ok \? take\(keep\(URL\.createObjectURL\(file\)\)\) : setProblem\(CAPTURE\.shotFailed\)\)\);/); // the drop path too
});
