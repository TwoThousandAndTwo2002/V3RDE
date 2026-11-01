// X50c (T9 Nov 1: "on mobile user is not able to upload a previously taken photo", Android, DuckDuckGo): the web photo
// picker. expo-image-picker (web) opens a hidden (display: none) file field with a made-up click event and then waits
// for the photo to load into an Image to read its size, with no error path: a photo the browser cannot show leaves
// the app waiting with no word. Here the field is a real, tiny, off-screen one that gets a real click() in the tap;
// the app then checks that the browser can read the photo (canDecode) and gives it to the upload step.
// No react-native import: the tests load this file.

let pending: (() => void) | null = null; // the field of an earlier tap that never answered (no cancel event)

/** Opens the photo picker of the browser; the chosen file, or null (cancelled). Call it in the tap, before any await. */
export function pickImageWeb(doc: Document = document): Promise<File | null> {
  pending?.();
  return new Promise((resolve) => {
    const input = doc.createElement("input");
    input.type = "file";
    input.accept = "image/*";
    input.setAttribute("aria-hidden", "true");
    input.tabIndex = -1;
    // off screen but not display: none (some Android web views do not open a field that is not shown)
    input.style.cssText = "position:fixed;left:-100px;top:0;width:1px;height:1px;opacity:0";
    let done = false;
    const end = (f: File | null) => {
      if (done) return;
      done = true;
      pending = null;
      input.remove();
      resolve(f);
    };
    pending = () => end(null);
    input.addEventListener("change", () => end(input.files?.[0] ?? null));
    input.addEventListener("cancel", () => end(null));
    doc.body.appendChild(input);
    input.click();
  });
}

/** A file the upload can try: an image type, or no type (some Android galleries give none). */
export function isImageFile(f: { type?: string } | null): boolean {
  return !!f && (!f.type || f.type.startsWith("image/"));
}

/** X50c review: the browser can read the photo (a file it cannot show, for example HEIC on Android, would else go to
 *  the scan as it is). True when the browser has no createImageBitmap (the upload step then decides). */
export async function canDecode(f: Blob, decode: ((b: Blob) => Promise<{ close?: () => void }>) | undefined =
  (globalThis as { createImageBitmap?: (b: Blob) => Promise<{ close?: () => void }> }).createImageBitmap): Promise<boolean> {
  if (!decode) return true;
  try {
    const b = await decode(f);
    b.close?.();
    return true;
  } catch {
    return false;
  }
}
