// The new-listing flow, written new for V3RDE (NEW_WORK_UI.md, UI 14; mobile/docs/style_guide.md "Capture"; the
// approved X25 and X32 mockups). The camera: full screen on a phone; a card dialog with "Upload a photo" and
// drag-and-drop on a wide screen. It reads retail barcodes; a read takes the photo. After the shutter every step is
// a sheet over the photo: "Use this photo?", "Analyzing… n.n s", and a sheet for each answer code. The photo is
// always kept until Retake. The review of the found items comes in UI 15.
import { useEffect, useRef, useState } from "react";
import { Modal, StyleSheet, View, useWindowDimensions } from "react-native";
import * as RN from "react-native";
import { CameraView, useCameraPermissions as useCameraAccess, type BarcodeScanningResult, type BarcodeType } from "expo-camera";
import * as Picker from "expo-image-picker";
import { ApiError, analyze, type Upload } from "../api";
import { analyzeKind, answerToItems, foundTitle, retryText, type AnalyzeKind, type ItemForm } from "../logic";
import { SHOT_QUALITY, analyzeShare, barcodeOf, elapsedText } from "../capture";
import { asUri, fromBlob, fromUri } from "../photo";
import { freeze, raise, still, unfreeze, videoIn, videoState } from "../webcam";
import { camera, layoutFor, radius, size, space, useColors } from "../theme";
import { CameraBtn, CardDialog, ErrorText, Hint, Muted, Pill, Progress, Rise, Row, Sheet, Shutter, TextLink, Title } from "../ui";
import { useSession } from "../session";
import { CAPTURE } from "../words";

/** The retail barcodes the catalog knows (EAN and UPC). */
const CODES: BarcodeType[] = ["ean13", "ean8", "upc_a", "upc_e"];

/** A photo on its way: `raw` = a web still as taken (it is shrunk inside the Analyzing time). */
type Shot = Upload & { raw?: boolean };
/** The answer of the analyze route that the review uses (UI 15). */
export type Answer = { requestId: string; items: ItemForm[]; source: string };
type Step =
  | { kind: "ask" } | { kind: "busy"; since: number } | { kind: "found"; answer: Answer }
  | { kind: Exclude<AnalyzeKind, "retry" | "unavailable"> };

function Analyzing({ since }: { since: number }) {
  const [now, setNow] = useState(() => Date.now());
  useEffect(function tenths() {
    const timer = setInterval(() => setNow(Date.now()), 100);
    return () => clearInterval(timer);
  }, []);
  return (
    <>
      <Rise i={0}><Title center>{CAPTURE.analyzing + " " + elapsedText(now - since)}</Title></Rise>
      <Rise i={1}><View style={{ marginTop: space.bar }}><Progress share={analyzeShare(now - since)} /></View></Rise>
    </>
  );
}

/** A small sheet with a title, a line and two pills (the answer codes). */
function Choice({ photo, title, line, left, right, testID }: {
  photo: string; title: string; line: string; left: [string, () => void]; right: [string, () => void]; testID: string;
}) {
  return (
    <Sheet testID={testID} kind="small" photo={photo}>
      <Rise i={0}><Title center>{title}</Title></Rise>
      <Rise i={1}><Muted center lines={2} style={{ marginTop: space.hair }}>{line}</Muted></Rise>
      <Rise i={2}><Row style={{ marginTop: space.gutter }}>
        <Pill testID={testID + "-left"} kind="secondary" title={left[0]} onPress={left[1]} grow />
        <Pill testID={testID + "-right"} title={right[0]} onPress={right[1]} grow />
      </Row></Rise>
    </Sheet>
  );
}

export default function Capture({ onClose, review }: {
  onClose: () => void;
  /** UI 15: the review of the found items; without it a placeholder sheet shows the count. */
  review?: (answer: Answer, shot: Upload, startOver: () => void) => React.ReactNode;
}) {
  const col = useColors();
  const { ended } = useSession();
  const page = layoutFor(useWindowDimensions().width);
  const wide = page.dialogs;
  const [perm, askPerm] = useCameraAccess();
  const lens = useRef<CameraView>(null);
  const lensBox = useRef<React.ComponentRef<typeof View>>(null); // web: the box with expo-camera's <video>
  const [ready, setReady] = useState(false); // the camera can take a photo
  const [shot, setShot] = useState<Shot | null>(null);
  const [code, setCode] = useState("");
  const [step, setStep] = useState<Step>({ kind: "ask" });
  const [problem, setProblem] = useState("");
  const [dropping, setDropping] = useState(false);
  const shooting = useRef(false); // a photo is on its way
  const how = useRef<Record<string, unknown>>({}); // how this photo was taken (sent with it; the server logs it)
  const count = useRef(0);

  const asked = useRef(false); // the camera access is asked one time (the web says "can ask again" after a no)
  useEffect(function askOnce() {
    if (!perm || perm.granted || !perm.canAskAgain || asked.current) return;
    asked.current = true;
    askPerm();
  }, [perm, askPerm]);
  // each new photo or Retake makes the answers of the trips before it stale
  const trip = useRef(0);
  // the object URLs of the photos of this flow; they are given back at Retake and at the end
  const urls = useRef<string[]>([]);
  const keep = (u: string) => { if (u.startsWith("blob:")) urls.current.push(u); return u; };
  const freeUrls = () => { for (const u of urls.current) URL.revokeObjectURL?.(u); urls.current = []; };
  useEffect(() => () => { if (RN.Platform.OS === "web") freeUrls(); }, []); // eslint-disable-line react-hooks/exhaustive-deps -- at the end
  useEffect(function cameraGone() {
    if (shot) setReady(false); // the camera closes under a photo; it is ready again after onCameraReady
  }, [shot]);

  const take = async (uri: string, barcode = "", meta: Record<string, unknown> = { source: "upload" }) => {
    setProblem(""); setStep({ kind: "ask" }); setCode(barcode);
    const t0 = Date.now();
    const mine = ++trip.current;
    const up = await fromUri(uri);
    if (mine !== trip.current) return;
    how.current = { ...meta, prep_ms: Date.now() - t0 };
    if (up.uri.startsWith("blob:")) keep(up.uri);
    setShot(up);
  };

  const shoot = async (barcode = "") => {
    if (!lens.current || !ready || shooting.current) return; // one photo at a time
    shooting.current = true; // set before the first await
    const t0 = Date.now();
    const meta: Record<string, unknown> = { trigger: barcode ? "barcode" : "shutter", n: ++count.current };
    try {
      const v = RN.Platform.OS === "web" ? videoIn(lensBox.current) : null;
      if (v) { // web: the camera's full size (webcam.ts); the preview stops at the press
        Object.assign(meta, videoState(v));
        freeze(v);
        try {
          const s = await still(v);
          how.current = { ...meta, source: s.source, ms: Date.now() - t0, w: s.width, h: s.height };
          setProblem(""); setCode(barcode); setStep({ kind: "ask" });
          ++trip.current;
          setShot({ uri: keep(s.uri), blob: s.blob, name: "photo.jpg", type: "image/jpeg", raw: true });
        } catch (err) { unfreeze(v); throw err; }
        return;
      }
      const p = await lens.current.takePictureAsync({ quality: SHOT_QUALITY }); // the camera turns the photo (Android)
      if (p?.uri) await take(asUri(p.uri), barcode, { ...meta, source: "native", ms: Date.now() - t0 });
    } catch {
      setProblem(CAPTURE.shotFailed);
    } finally {
      shooting.current = false;
    }
  };

  const pick = async () => {
    if (shooting.current) return; // not while a photo is on its way
    try {
      const r = await Picker.launchImageLibraryAsync({ mediaTypes: ["images"], quality: 1 });
      if (!r.canceled && r.assets?.[0]) await take(r.assets[0].uri);
    } catch {
      setProblem(CAPTURE.shotFailed);
    }
  };

  const onCode = (r: BarcodeScanningResult) => {
    const c = barcodeOf(r?.data);
    if (c) shoot(c);
  };

  const startOver = () => {
    ++trip.current;
    if (RN.Platform.OS === "web") freeUrls();
    setShot(null); setCode(""); setProblem(""); setStep({ kind: "ask" });
  };

  const send = async () => {
    if (!shot) return;
    const mine = ++trip.current;
    const stale = () => mine !== trip.current; // Retake or Back came while this trip was out
    setProblem(""); setStep({ kind: "busy", since: Date.now() });
    try {
      let up: Upload = shot;
      if (shot.raw && shot.blob) { // the shrink is inside the Analyzing time
        const t0 = Date.now();
        up = await fromBlob(shot.blob);
        if (stale()) return;
        how.current = { ...how.current, prep_ms: Date.now() - t0 };
        setShot({ ...up, uri: keep(up.uri) });
      }
      const body = await analyze(up, code, how.current);
      if (stale()) return;
      const items = answerToItems(body);
      if (items.length === 0) { setStep({ kind: "ask" }); setProblem(retryText(502, null)); return; }
      setStep({ kind: "found", answer: { requestId: typeof body?.request_id === "string" ? body.request_id : "", items,
        source: typeof body?.source === "string" ? body.source : "" } });
    } catch (err) {
      if (stale()) return;
      if (err instanceof ApiError && err.status === 401) return ended();
      const status = err instanceof ApiError ? err.status : 0;
      const body = err instanceof ApiError ? err.body : null;
      const k = analyzeKind(body);
      if (k === "retry" || k === "unavailable") { setStep({ kind: "ask" }); setProblem(retryText(status, body)); return; }
      setStep({ kind: k });
    }
  };

  // Back (phone) and Esc (web: the Modal gives it) go one step back; not while the photo is analyzed
  const back = () => {
    if (!shot) onClose();
    else if (step.kind !== "busy") startOver();
  };
  // web: an image dropped on the window is used while the camera is up
  useEffect(function webDrop() {
    if (RN.Platform.OS !== "web") return;
    const over = (e: DragEvent) => { e.preventDefault(); if (!shot) setDropping(true); };
    const leave = () => setDropping(false);
    const dropped = (e: DragEvent) => {
      e.preventDefault(); setDropping(false);
      const file = e.dataTransfer?.files?.[0];
      if (!shot && file && file.type.startsWith("image/")) take(URL.createObjectURL(file));
    };
    for (const [name, fn] of [["dragover", over], ["dragleave", leave], ["drop", dropped]] as const) window.addEventListener(name, fn as EventListener);
    return () => {
      for (const [name, fn] of [["dragover", over], ["dragleave", leave], ["drop", dropped]] as const) window.removeEventListener(name, fn as EventListener);
    };
  }, [shot]); // eslint-disable-line react-hooks/exhaustive-deps -- take only sets state

  let body: React.ReactNode;
  if (shot) {
    const photo = shot.uri;
    if (step.kind === "found") {
      body = review ? review(step.answer, shot, startOver) : (
        <Choice testID="found" photo={photo} title={foundTitle(step.answer.items.length)} line=""
          left={[CAPTURE.retake, startOver]} right={[CAPTURE.closeFlow, onClose]} />
      );
    } else if (step.kind === "busy") {
      body = <Sheet testID="analyzing" kind="small" photo={photo}><Analyzing since={step.since} /></Sheet>;
    } else if (step.kind === "not_food") {
      body = <Choice testID="not-food" photo={photo} title={CAPTURE.notFood} line={CAPTURE.notFoodLine}
        left={[CAPTURE.upload, pick]} right={[CAPTURE.retake, startOver]} />;
    } else if (step.kind === "nofood_single") {
      body = <Choice testID="no-name-one" photo={photo} title={CAPTURE.noNameOne} line={CAPTURE.noNameOneLine}
        left={[CAPTURE.upload, pick]} right={[CAPTURE.retake, startOver]} />;
    } else if (step.kind === "nofood_multi") {
      body = <Choice testID="no-name-many" photo={photo} title={CAPTURE.noNameMany} line={CAPTURE.noNameManyLine}
        left={[CAPTURE.upload, pick]} right={[CAPTURE.retake, startOver]} />;
    } else if (step.kind === "already_listed") {
      body = <Choice testID="already-listed" photo={photo} title={CAPTURE.listed} line={CAPTURE.listedLine}
        left={[CAPTURE.retake, startOver]} right={[CAPTURE.seeMine, onClose]} />;
    } else if (step.kind === "daily_cap") {
      body = <Choice testID="daily-cap" photo={photo} title={CAPTURE.paused} line={CAPTURE.pausedLine}
        left={[CAPTURE.closeFlow, onClose]} right={[CAPTURE.retake, startOver]} />;
    } else {
      const showCode = !!code && !problem;
      body = (
        <Sheet testID="use-photo" kind="small" photo={photo}>
          <Rise i={0}><Title center>{problem ? CAPTURE.again : CAPTURE.use}</Title></Rise>
          {showCode ? <Rise i={1}><Muted center style={{ marginTop: space.hair }}>{CAPTURE.barcode(code)}</Muted></Rise> : null}
          <Rise i={showCode ? 2 : 1}><Row style={{ marginTop: showCode ? space.gutter : space.foot }}>
            <Pill testID="retake" kind="secondary" title={CAPTURE.retake} onPress={startOver} grow />
            <Pill testID="use" title={problem ? CAPTURE.tryAgain : CAPTURE.usePhoto} onPress={send} grow />
          </Row></Rise>
          {problem ? <Rise i={3}><ErrorText center lines={2}>{problem}</ErrorText></Rise> : null}
        </Sheet>
      );
    }
  } else {
    const granted = perm?.granted === true;
    const onReady = () => {
      setReady(true);
      const v = RN.Platform.OS === "web" ? videoIn(lensBox.current) : null;
      if (v) raise(v); // web: ask the camera for its largest size
    };
    const live = granted ? (
      <View ref={lensBox} style={StyleSheet.absoluteFill}>
        <CameraView ref={lens} style={StyleSheet.absoluteFill} facing={page.topBar ? "front" : "back"}
          onCameraReady={onReady} barcodeScannerSettings={{ barcodeTypes: CODES }} onBarcodeScanned={ready ? onCode : undefined} />
      </View>
    ) : (
      <View style={[StyleSheet.absoluteFill, { alignItems: "center", justifyContent: "center", padding: space.pad, gap: space.gap }]}>
        <Muted center style={{ color: col.cameraFg }}>{perm && !perm.canAskAgain ? CAPTURE.blocked : CAPTURE.allow}</Muted>
        {perm && perm.canAskAgain ? <TextLink testID="allow" onPress={askPerm}>{CAPTURE.allowLink}</TextLink> : null}
      </View>
    );
    const shutter = granted ? () => shoot() : pick;
    if (wide) {
      const hint = RN.Platform.OS !== "web" ? CAPTURE.hint : dropping ? CAPTURE.dropNow : CAPTURE.hint + " · " + CAPTURE.dropHint;
      body = (
        <CardDialog testID="camera">
          <View style={{ width: "100%", aspectRatio: camera.ratio, borderRadius: radius.card, overflow: "hidden", backgroundColor: col.cameraBg,
            borderWidth: dropping ? size.focusBorder : 0, borderColor: col.accent }}>
            {live}
            <Shutter onPress={shutter} label={granted ? CAPTURE.shoot : CAPTURE.upload} bottom={camera.cardShutterBottom} />
          </View>
          <Row style={{ marginTop: space.gutter }}>
            <Pill testID="upload" kind="secondary" title={CAPTURE.upload} onPress={pick} grow />
            <Pill testID="cancel" kind="secondary" title={CAPTURE.cancel} onPress={onClose} grow />
          </Row>
          <Muted center style={{ marginTop: space.gap }}>{hint}</Muted>
          {problem ? <ErrorText center>{problem}</ErrorText> : null}
        </CardDialog>
      );
    } else {
      body = (
        <View testID="camera" style={{ flex: 1, backgroundColor: col.cameraBg }}>
          {live}
          <CameraBtn glyph="close" label={CAPTURE.close} onPress={onClose} style={{ left: camera.edge, top: camera.edge }} />
          <Hint alert={!!problem}>{problem || CAPTURE.hint}</Hint>
          <CameraBtn glyph="upload" label={CAPTURE.upload} onPress={pick} style={{ left: camera.side, bottom: camera.uploadBottom }} />
          <Shutter onPress={shutter} label={granted ? CAPTURE.shoot : CAPTURE.upload} />
        </View>
      );
    }
  }
  return (
    <Modal visible transparent animationType="none" statusBarTranslucent onRequestClose={back}>
      {body}
    </Modal>
  );
}
