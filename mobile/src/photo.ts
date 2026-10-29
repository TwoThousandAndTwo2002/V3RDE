// A photo for the analyze route, written new for V3RDE (NEW_WORK_UI.md, UI 14). A taken or picked photo becomes a
// JPEG with the long edge at most UPLOAD.maxEdge (logic.ts, X32 D28). The size is read from the decoded image,
// and only the width is given to the resize, so the photo keeps its own aspect (an Android camera can report the
// sensor's unturned size). The web shrinks a still once with a canvas (webcam.shrink); it also needs a Blob.
import * as RN from "react-native";
import { ImageManipulator as Editor, SaveFormat as Format } from "expo-image-manipulator";
import type { Upload as Part } from "./api";
import { UPLOAD, targetSize } from "./logic";
import { shrink } from "./webcam";

const JPEG = { name: "photo.jpg", type: "image/jpeg" } as const;

/** Web: a still from the camera or a dropped file, shrunk to the upload size. */
export async function fromBlob(original: Blob): Promise<Part> {
  const small = await shrink(original, UPLOAD.maxEdge, UPLOAD.quality);
  return { ...JPEG, uri: URL.createObjectURL(small.blob), blob: small.blob };
}

/** A photo URI (camera or library): decoded, made smaller when it is too big, saved as JPEG. */
export async function fromUri(uri: string): Promise<Part> {
  let saved = uri;
  try {
    const decoded = await Editor.manipulate(uri).renderAsync();
    const goal = targetSize(decoded.width, decoded.height, UPLOAD.maxEdge);
    const job = Editor.manipulate(decoded);
    if (goal.width > 0 && goal.width < decoded.width) job.resize({ width: goal.width });
    saved = (await (await job.renderAsync()).saveAsync({ compress: UPLOAD.quality, format: Format.JPEG })).uri;
  } catch {
    // a photo the library cannot decode goes as it is; the services check it
  }
  const out: Part = { ...JPEG, uri: saved };
  if (RN.Platform.OS === "web") out.blob = await (await fetch(saved)).blob();
  return out;
}

/** expo-camera on the web can give bare base64 in `uri`: make it a data URI. */
export function asUri(u: string): string {
  return /^(data|blob|file|https?|content|ph):/.test(u) ? u : "data:image/jpeg;base64," + u;
}
