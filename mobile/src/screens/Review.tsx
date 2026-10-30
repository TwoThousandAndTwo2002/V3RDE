// The review of the found items, written new for V3RDE (NEW_WORK_UI.md, UI 15; the approved X32 mockup).
// One item: the form "Your listing" (title, servings, pickup). Several: the review list, a card for each item with
// the item photo the server cut, and one pickup for all. Post sends one listing for each item, one after the
// other; a posted item leaves the list, a failed one stays with its error. What the user did with the answer goes
// to /api/analyze/labels one time (posted, retake or closed). An item with a server photo can be touched up
// (UI 16, TouchUp.tsx); the photo the user chose is the one that posts. The chat bar (microphone or text) changes
// the list at once (UI 17, X48s, Voice.tsx).
import { useEffect, useRef, useState } from "react";
import { KeyboardAvoidingView, TextInput, View, useWindowDimensions } from "react-native";
import * as RN from "react-native";
import { ApiError, analyzeLabels, listings, touchup, type Upload } from "../api";
import {
  deviceLocale, foundTitle, itemPhotoPath, labelReport, notPostedTitle, postTitle, postedSummary, postingText,
  type ItemForm, type LabelOutcome,
} from "../logic";
import { LIMITS, listingProblem, servingsInput, sourceNote, stepServings } from "../capture";
import { layoutFor, space, useColors } from "../theme";
import { ErrorText, Field, IconBtn, Muted, Photo, Pill, ReviewCard, Rise, Row, Sheet, SheetBody, SheetPills, Title, usePhoto } from "../ui";
import { useSession } from "../session";
import { REVIEW } from "../words";
import type { Answer } from "./Capture";
import { TouchUp } from "./TouchUp";
import { useChat } from "./Voice";
import { POLL_MS, afterPoll, photoToPost, touchError, type TouchState } from "../touchup";
import { TOUCH, VOICE } from "../words";

/** One card of the review list, with the item photo of the server (or the touched-up photo the user chose). */
function ItemCard({ it, n, touch, mark, editId, onChange, onRemove }: {
  it: ItemForm; n: number; touch?: React.ReactNode; mark?: string; editId?: string; onChange: (f: ItemForm) => void; onRemove: () => void;
}) {
  const uri = usePhoto(editId ? touchup.photoPath(editId) : itemPhotoPath(it.photo_id) || undefined);
  const col = useColors();
  const extra = mark ? <>{touch}<Muted style={{ color: col.link }}>{mark}</Muted></> : touch; // X48s: changed in the chat
  return (
    <ReviewCard uri={uri || undefined} n={n} title={it.title} servings={it.servings} error={it.err} extra={extra}
      onTitle={(t) => onChange({ ...it, title: t })} onServings={(t) => onChange({ ...it, servings: servingsInput(t) })}
      onRemove={onRemove} onLess={() => onChange({ ...it, servings: stepServings(it.servings, -1) })}
      onMore={() => onChange({ ...it, servings: stepServings(it.servings, 1) })} />
  );
}

export default function Review({ answer, shot, barcode, onStartOver, onPosted, onBusy }: {
  answer: Answer; shot: Upload; barcode: string; onStartOver: () => void; onPosted: (n: number) => void;
  /** Tells the flow that listings are posting (Back and Esc wait). */
  onBusy: (busy: boolean) => void;
}) {
  const { ended } = useSession();
  const col = useColors();
  const wide = layoutFor(useWindowDimensions().width).dialogs;
  const [items, setItems] = useState<ItemForm[]>(answer.items);
  const [pickup, setPickup] = useState("");
  const [problem, setProblem] = useState("");
  const [sending, setSending] = useState(false);
  const [progress, setProgress] = useState("");
  const [partial, setPartial] = useState(false);
  const busy = useRef(false);
  const [touch, setTouch] = useState<Record<number, TouchState>>({}); // by item number
  const live = useRef(true);
  useEffect(() => () => { live.current = false; }, []);
  const removed = useRef(new Set<number>()); // items the user removed: their jobs stop
  const talking = useRef(false); // a chat line is out (X48s: a change goes in at once, so nothing else waits)
  const setOne = (item: number, t: TouchState) => live.current && setTouch((all) => ({ ...all, [item]: t }));
  const startTouch = async (item: number, photoId: string) => {
    const since = Date.now();
    setOne(item, { kind: "working", id: "", since });
    try {
      const job = await touchup.start(photoId);
      let now: TouchState = { kind: "working", id: job.touchup_id, since };
      while (live.current && now.kind === "working") {
        await new Promise((go) => setTimeout(go, POLL_MS));
        if (!live.current || removed.current.has(item)) return;
        now = afterPoll(await touchup.get(job.touchup_id), job.touchup_id, since, Date.now());
      }
      setOne(item, now);
    } catch (err) {
      if (err instanceof ApiError && err.status === 401) return ended();
      const code = err instanceof ApiError && typeof err.body?.code === "string" ? err.body.code : "";
      setOne(item, { kind: "error", text: touchError(err instanceof ApiError ? err.status : 0, code, TOUCH) });
    }
  };
  const touchFor = (it: ItemForm, n: number) => (it.photo_id ? (
    <TouchUp n={n} photoId={it.photo_id} state={touch[it.item] ?? { kind: "idle" }} locked={sending}
      onStart={() => startTouch(it.item, it.photo_id)}
      onChoose={(c) => { const t = touch[it.item]; if (t?.kind === "offer") setOne(it.item, { ...t, chosen: c }); }} />
  ) : null);
  const working = items.some((it) => touch[it.item]?.kind === "working"); // only items still in the list
  const editOf = (it: ItemForm) => { const t = touch[it.item]; return t?.kind === "offer" && t.chosen === "edit" ? t.id : undefined; };
  const postedSoFar = useRef(0); // the listings of this answer that are up (a second Post after a partial one adds to it)
  const told = useRef(false); // the label report goes one time for this answer
  const tell = (final: ItemForm[], outcome: LabelOutcome) => {
    if (told.current || !answer.requestId) return;
    told.current = true;
    analyzeLabels(labelReport(answer.requestId, answer.items, final, outcome, deviceLocale()));
  };
  useEffect(() => () => tell([], "closed"), []); // eslint-disable-line react-hooks/exhaustive-deps -- left with no post
  const startOver = () => {
    if (busy.current) return; // not while the listings post
    tell([], "retake");
    onStartOver();
  };
  const pickupBox = useRef<React.ComponentRef<typeof TextInput>>(null);
  const chat = useChat({ items, pickup, onOpen: (o) => { talking.current = o; },
    onApply: (next, place) => { setItems(next); setPickup(place); setProblem(""); } });
  const one = answer.items.length === 1;

  const post = async () => {
    if (busy.current || items.length === 0) return;
    if (working) return setProblem(TOUCH.wait); // a touch-up that still runs decides the photo
    if (talking.current) return setProblem(VOICE.first); // a chat line is still being read
    const pickupWrong = listingProblem("-", 1, pickup); // the pickup is for all items: no item name with it
    if (pickupWrong) return setProblem(pickupWrong);
    for (const it of items) {
      const wrong = listingProblem(it.title, it.servings, pickup);
      if (wrong) return setProblem(one || !it.title.trim() ? wrong : it.title.trim() + ": " + wrong);
    }
    setProblem("");
    busy.current = true;
    onBusy(true);
    setSending(true);
    const sent = items; // the list as the user left it, for the label report
    const left: ItemForm[] = [];
    let done = 0;
    for (let k = 0; k < items.length; k++) {
      const it = items[k];
      if (!one) setProgress(postingText(k + 1, items.length));
      const base = { title: it.title.trim(), number_of_serving: Number(it.servings), pickup_text: pickup.trim(), request_id: answer.requestId,
        details: it.details };
      try {
        const photoId = photoToPost(it.photo_id, touch[it.item] ?? { kind: "idle" });
        await listings.create(photoId ? { ...base, photo_id: photoId } : { ...base, photo: shot });
        done++;
      } catch (err) {
        if (err instanceof ApiError && err.status === 401) { busy.current = false; onBusy(false); return ended(); }
        if (one) setProblem(err instanceof Error ? err.message : String(err));
        left.push({ ...it, err: REVIEW.notPosted });
      }
    }
    busy.current = false;
    onBusy(false);
    setSending(false);
    setProgress("");
    if (done > 0) tell(sent, "posted"); // the report goes after a listing is up, not before
    postedSoFar.current += done;
    if (done > 0) chat.clear(); // X48s: the chat is cleared after a post
    if (left.length === 0) return onPosted(postedSoFar.current);
    if (!one) {
      setItems(left);
      setPartial(true);
      setProblem(postedSoFar.current > 0 ? postedSummary(postedSoFar.current, left.length) : REVIEW.nonePosted);
    }
  };

  const pickupField = (
    <Field ref={pickupBox} testID="pickup" label={one ? REVIEW.pickup : REVIEW.pickupAll} value={pickup} multiline maxLength={LIMITS.pickup}
      placeholder={REVIEW.pickupHint} returnKeyType="done" submitBehavior="blurAndSubmit"
      onChangeText={(t) => setPickup(t.replace(/\n/g, " "))} />
  );
  const pickupMark = chat.pickupMarked ? (
    <Muted style={{ color: col.link, marginTop: space.hair, marginLeft: space.inset }}>{VOICE.markPickup}</Muted>
  ) : null;
  let fields: React.ReactNode;
  if (one) {
    const it = items[0];
    const note = sourceNote(answer.source === "barcode" ? barcode : "", it.description);
    const set = (f: Partial<ItemForm>) => setItems([{ ...it, ...f }]);
    fields = (
      <View style={{ flex: 1, minWidth: 0 }}>
        <Rise i={2}><Field testID="title" label={REVIEW.title} value={it.title} maxLength={LIMITS.title} returnKeyType="next"
          onSubmitEditing={() => pickupBox.current?.focus()}
          onChangeText={(t) => set({ title: t })} /></Rise>
        {note ? <Rise i={3}><Muted style={{ marginTop: space.inset, marginLeft: space.inset }}>{note}</Muted></Rise> : null}
        {it.photo_id ? <Rise i={3}><View style={{ marginLeft: space.inset }}>{touchFor(it, 1)}</View></Rise> : null}
        <Rise i={4}>
          <Muted style={{ marginTop: space.group, marginLeft: space.inset, marginBottom: space.hair }}>{REVIEW.servings}</Muted>
          <Row>
            <IconBtn big glyph="minus" label={REVIEW.fewer} onPress={() => set({ servings: stepServings(it.servings, -1) })} />
            <View style={{ flex: 1, minWidth: 0 }}>
              <Field testID="servings" accessibilityLabel={REVIEW.servings} value={it.servings} center keyboardType="number-pad"
                inputMode="numeric" onChangeText={(t) => set({ servings: servingsInput(t) })} /></View>
            <IconBtn big glyph="plus" label={REVIEW.more} onPress={() => set({ servings: stepServings(it.servings, 1) })} />
          </Row>
        </Rise>
        <Rise i={5}>{pickupField}{pickupMark}</Rise>
        {problem ? <ErrorText>{problem}</ErrorText> : null}
      </View>
    );
  } else {
    fields = (
      <View style={{ flex: 1, minWidth: 0 }}>
        {items.map((it, k) => (
          <Rise key={it.item} i={2 + k}>
            <ItemCard it={it} n={k + 1} touch={touchFor(it, k + 1)} mark={chat.marks[it.item]} editId={editOf(it)}
              onChange={(f) => setItems(items.map((x) => (x.item === it.item ? f : x)))}
              onRemove={() => { removed.current.add(it.item); setItems(items.filter((x) => x.item !== it.item)); }} /></Rise>
        ))}
        <Rise i={2 + items.length}>{pickupField}{pickupMark}</Rise>
        {problem ? <ErrorText>{problem}</ErrorText> : null}
      </View>
    );
  }
  const title = one ? REVIEW.one : partial ? notPostedTitle(items.length) : foundTitle(items.length);
  const last = one ? 6 : 3 + items.length;
  return (
    <KeyboardAvoidingView style={{ flex: 1 }} behavior={RN.Platform.OS === "ios" ? "padding" : undefined}>
      <Sheet testID={one ? "your-listing" : "review-list"} kind="large" wide photo={shot.uri}>
        <View style={{ flexShrink: 1, flexGrow: 1 }}>
          <Rise i={0}><Title style={{ marginBottom: space.hair }}>{title}</Title></Rise>
          {one ? null : <Rise i={1}><Muted>{REVIEW.check}</Muted></Rise>}
          {wide ? (
            // X48s: the photo stays in place; only the list column scrolls, with the chat bar under it
            <View style={{ flexDirection: "row", gap: space.foot, alignItems: "flex-start", marginTop: space.inset, flexShrink: 1, minHeight: 0 }}>
              <View style={{ flex: 1, minWidth: 0 }}><Rise i={1}><Photo uri={shot.uri} round="card" /></Rise></View>
              <View style={{ flex: 1, minWidth: 0, alignSelf: "stretch" }}>
                <SheetBody>{fields}{items.length > 0 ? chat.thread : null}</SheetBody>
                {items.length > 0 && !sending ? chat.bar : null}
              </View>
            </View>
          ) : (
            <>
              <SheetBody>{fields}{items.length > 0 ? chat.thread : null}</SheetBody>
              {items.length > 0 && !sending ? chat.bar : null}
            </>
          )}
          <View accessibilityLiveRegion="polite" aria-live="polite">
            {progress ? <Muted center style={{ marginTop: space.gap }}>{progress}</Muted> : null}</View>
          <Rise i={last}>
            <SheetPills>
              <Pill testID="start-over" kind="secondary" title={REVIEW.startOver} onPress={sending ? undefined : startOver} grow />
              {items.length > 0 ? <Pill testID="post" title={one ? REVIEW.post : postTitle(items.length)} busy={sending} busyName={REVIEW.posting}
                onPress={post} grow /> : null}
            </SheetPills>
          </Rise>
        </View>
      </Sheet></KeyboardAvoidingView>
  );
}
