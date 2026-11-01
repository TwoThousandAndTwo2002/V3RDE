// The review of the found items, written new for V3RDE (NEW_WORK_UI.md, UI 15; the approved X32 mockup).
// One item: the form "Your listing" (title, servings, pickup). Several: the review list, a card for each item with
// the item photo the server cut, and one pickup for all. Post sends one listing for each item, one after the
// other; a posted item leaves the list, a failed one stays with its error. What the user did with the answer goes
// to /api/analyze/labels one time (posted, retake or closed). An item with a server photo can be touched up
// (UI 16, TouchUp.tsx); the photo the user chose is the one that posts. The chat bar (microphone or text) changes
// the list at once (UI 17, X48s, Voice.tsx). X49: a Details pill next to Touch up shows what the scan found (phone: a
// small Popup; expanded width: a third column); Change edits the details, and the edit is recorded (Details.tsx).
// X49i (T9 2026-10-31, the approved "V3RDE Give And Review Mockup" v2): Touch up and the details are under one wide
// "More" button on each item (no Details button: the details show at once); a dialog opens it in the card, a phone
// in the bottom sheet. The third column of the widest dialog holds the chat, frosted.
import { useEffect, useRef, useState } from "react";
import { Image, KeyboardAvoidingView, ScrollView, TextInput, View, useWindowDimensions } from "react-native";
import * as RN from "react-native";
import { ApiError, analyzeLabels, listings, touchup, type Upload } from "../api";
import {
  addEdits, deviceLocale, diffEdits, notPostedItems, stillOpen, fitReport, foundTitle, itemPhotoPath, labelReport, notPostedTitle, postTitle, postedSummary, postingText,
  type ChatLine, type Edit, type EditHow, type ItemForm, type LabelOutcome, type PhotoLabel,
} from "../logic";
import { LIMITS, listingProblem, servingsInput, sourceNote, stepServings } from "../capture";
import { layoutFor, radius, size, space, useColors } from "../theme";
import { Body, ChipBtn, ErrorText, Field, Frost, Grow, IconBtn, Muted, Pill, Popup, ReviewCard, Rise, Row, Sheet, SheetBody, SheetPills, TextLink, Title,
  usePhoto } from "../ui";
import { useSession } from "../session";
import { REVIEW } from "../words";
import type { Answer } from "./Capture";
import { TouchUp } from "./TouchUp";
import { DetailsView, hasDetails } from "./Details";
import { useChat } from "./Voice";
import { POLL_MS, PUSH_POLL_MS, afterPoll, photoToPost, touchError, type TouchState } from "../touchup";
import { pollMs, pushNow, pushWait } from "../push";
import { MORE, PICKUP, PLACE, TOUCH, VOICE } from "../words";
import { currentPosition, type Place } from "../place";
import type { PickedAddress } from "../maps";
import { PickupAddress } from "./Pickup";

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
  const width = useWindowDimensions().width;
  const wide = layoutFor(width).dialogs;
  // X49i: the chat column, only when the dialog is its full width (narrower: the chat stays under the list)
  const column = wide && width >= size.dialogExtraWide + 2 * space.gutter;
  const [items, setItems] = useState<ItemForm[]>(answer.items);
  const [pickup, setPickup] = useState("");
  const [problem, setProblem] = useState("");
  // X49e: the pickup place when there is no address search (a phone build, or Maps did not load): the location now,
  // asked at Post. X49q (T9: "lets drop zip code"): no ZIP code; location off = the giver turns it on and posts again.
  // X49j: the pickup address (Places autocomplete, then the map) when the server gives a Maps key (web); else the X49e row
  const [mapsKey, setMapsKey] = useState("");
  const [picked, setPicked] = useState<PickedAddress | null>(null);
  const [needAddress, setNeedAddress] = useState(false); // X49q: the error shows under the address field
  const [needSeq, setNeedSeq] = useState(0); // X49w: each Post with no address scrolls the page to the address field
  const [naming, setNaming] = useState(false); // X49q review: the address of a moved pin is being found: Post waits
  useEffect(function mapsConfig() {
    if (RN.Platform.OS !== "web") return; // the Maps JavaScript API is a browser script
    let on = true;
    listings.config().then((c) => { if (on) setMapsKey(c.maps_key ?? ""); }).catch(() => undefined); // no key: the X49e row
    return () => { on = false; };
  }, []);
  const byAddress = !!mapsKey;
  const [sending, setSending] = useState(false);
  const [progress, setProgress] = useState("");
  const [partial, setPartial] = useState(false);
  const busy = useRef(false);
  const [touch, setTouch] = useState<Record<number, TouchState>>({}); // by item number
  const [moreOpen, setMoreOpen] = useState<number | null>(null); // X49i: the item whose More is open
  const [editItem, setEditItem] = useState<number | null>(null); // X49: the item whose details are being changed
  // X48u: every change in order and how it was made, for the label report (refs: the report also goes at unmount)
  const edits = useRef<Edit[]>([]);
  const now = useRef({ items, pickup });
  now.current = { items, pickup };
  const touchNow = useRef(touch);
  touchNow.current = touch;
  const pickupStart = useRef(""); // X48u review: the pickup before a run of typing (typed back to it: no edit)
  const change = (next: ItemForm[], place: string, how: EditHow) => {
    const last = edits.current[edits.current.length - 1];
    if (how === "hand" && place !== now.current.pickup && !(last?.how === "hand" && last.field === "pickup")) pickupStart.current = now.current.pickup;
    edits.current = addEdits(edits.current, diffEdits(now.current.items, next, now.current.pickup, place, how));
    const end = edits.current[edits.current.length - 1];
    if (how === "hand" && place === pickupStart.current && end?.how === "hand" && end.field === "pickup") edits.current = edits.current.slice(0, -1);
    now.current = { items: next, pickup: place };
    setMoreOpen((o) => stillOpen(o, next)); // X49 review: a removed item (by hand or in the chat) closes its More
    setEditItem((o) => stillOpen(o, next));
    setItems(next);
    setPickup(place);
  };
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
      let asked = pushNow(); // WS1: a message that came since the last read of the job also ends the wait
      while (live.current && now.kind === "working") {
        await pushWait(pollMs(POLL_MS, PUSH_POLL_MS), (m) => m.kind === "touchup" && m.id === job.touchup_id, asked); // WS1
        asked = pushNow();
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
  const scanned = (it: ItemForm) => answer.items.find((a) => a.item === it.item); // X49 review: the scanner's details
  useEffect(function closeGone() { // X49 review: the opened item is not in the list (posted, removed): no More
    setMoreOpen((o) => stillOpen(o, items));
    setEditItem((o) => stillOpen(o, items));
  }, [items]);
  const closeMore = () => { setMoreOpen(null); setEditItem(null); };
  const setDetails = (item: number, d: ItemForm["details"]) => {
    if (busy.current) return; // X49 review: not while the listings post (the post would bring the old copy back)
    change(now.current.items.map((x) => (x.item === item ? { ...x, details: d } : x)), now.current.pickup, "hand"); // X49: recorded
    setEditItem(null);
  };
  const touchFor = (it: ItemForm, n: number) => (!it.photo_id ? null : (
    <TouchUp n={n} photoId={it.photo_id} state={touch[it.item] ?? { kind: "idle" }} locked={sending}
      onStart={() => startTouch(it.item, it.photo_id)}
      onChoose={(c) => {
        const t = touch[it.item];
        if (t?.kind !== "offer") return;
        const name = (x: "original" | "edit") => (x === "edit" ? "touched_up" : "original");
        if (t.chosen !== c) edits.current = addEdits(edits.current, [{ item: it.item, field: "photo", from: name(t.chosen), to: name(c), how: "hand" }]);
        setOne(it.item, { ...t, chosen: c });
      }} />
  ));
  // X49i: what More holds: Touch up, then the details at once (Change edits them in place)
  // X49i review: while the listings post, an item with no details has nothing in More (Touch up is locked)
  const hasMore = (it: ItemForm) => (!!it.photo_id && !sending) || hasDetails(it, scanned(it));
  const moreInner = (it: ItemForm, n: number) => (
    <View testID={"more-menu-" + n} style={{ gap: space.gap }}>
      {touchFor(it, n)}
      {hasDetails(it, scanned(it)) ? (
        <DetailsView it={it} noTitle editing={editItem === it.item} locked={sending} onEdit={() => setEditItem(it.item)}
          onCancel={() => setEditItem(null)} onDone={(d) => setDetails(it.item, d)} />
      ) : null}
    </View>
  );
  const moreFor = (it: ItemForm, n: number) => {
    if (!hasMore(it)) return null;
    const open = moreOpen === it.item;
    return (
      <View>
        <ChipBtn testID={"more-" + n} wide glyph="menu" title={MORE.title} name={MORE.name(n)} on={open}
          onPress={() => { setEditItem(null); setMoreOpen(open ? null : it.item); }} />
        {open && wide ? <Grow key={it.item}><View style={{ marginTop: space.gap }}>{moreInner(it, n)}</View></Grow> : null}
      </View>
    );
  };
  const working = items.some((it) => touch[it.item]?.kind === "working"); // only items still in the list
  const editOf = (it: ItemForm) => { const t = touch[it.item]; return t?.kind === "offer" && t.chosen === "edit" ? t.id : undefined; };
  const postedSoFar = useRef(0); // the listings of this answer that are up (a second Post after a partial one adds to it)
  const told = useRef(false); // the label report goes one time for this answer
  const tell = (final: ItemForm[], outcome: LabelOutcome) => {
    if (told.current || !answer.requestId) return;
    told.current = true;
    const photos: Record<number, PhotoLabel> = {};
    for (const a of answer.items) {
      const t = touchNow.current[a.item];
      photos[a.item] = {
        photo: t?.kind === "offer" ? (t.chosen === "edit" ? "touched_up" : "original") : a.photo_id ? "original" : "",
        touchup: t?.kind === "offer" ? "done" : t?.kind === "kept" ? "kept" : t?.kind === "failed" || t?.kind === "error" ? "failed" : "",
      };
    }
    analyzeLabels(fitReport(labelReport(answer.requestId, answer.items, final, outcome, deviceLocale(),
      { edits: edits.current, chat: chatLog.current, photos })));
  };
  useEffect(() => () => tell([], "closed"), []); // eslint-disable-line react-hooks/exhaustive-deps -- left with no post
  const startOver = () => {
    if (busy.current) return; // not while the listings post
    tell([], "retake");
    onStartOver();
  };
  const pickupBox = useRef<React.ComponentRef<typeof TextInput>>(null);
  const chat = useChat({ items, pickup, onOpen: (o) => { talking.current = o; },
    onApply: (next, place, how) => { change(next, place, how); setProblem(""); } });
  const chatLog = useRef<ChatLine[]>([]);
  chatLog.current = chat.log.length ? chat.log : chatLog.current; // the chat clears after a post; its lines stay for the report
  const one = answer.items.length === 1;

  const post = async () => {
    if (busy.current || items.length === 0) return;
    if (working) return setProblem(TOUCH.wait); // a touch-up that still runs decides the photo
    if (talking.current) return setProblem(VOICE.first); // a chat line is still being read
    if (byAddress && !picked) { setProblem(""); setNeedSeq((n) => n + 1); return setNeedAddress(true); } // X49j: the address comes first (X49q: the error under the field)
    if (byAddress && naming) return setProblem(PICKUP.naming); // X49q review: the text must match the pin
    const pickupWrong = listingProblem("-", 1, pickup, byAddress); // the pickup is for all items: no item name with it
    if (pickupWrong) return setProblem(pickupWrong);
    for (const it of items) {
      const wrong = listingProblem(it.title, it.servings, pickup, byAddress);
      if (wrong) return setProblem(one || !it.title.trim() ? wrong : it.title.trim() + ": " + wrong);
    }
    setProblem("");
    busy.current = true;
    onBusy(true);
    setSending(true);
    let place: Place;
    try {
      place = byAddress && picked ? { lat: picked.lat, lng: picked.lng } : await currentPosition();
    } catch { // location is off, or this device has none
      busy.current = false;
      onBusy(false);
      setSending(false);
      return setProblem(PLACE.offPost);
    }
    const sent: ItemForm[] = []; // X48u review: the items that posted, as the user left them, for the label report
    const left: ItemForm[] = [];
    let done = 0;
    for (let k = 0; k < items.length; k++) {
      const it = items[k];
      if (!one) setProgress(postingText(k + 1, items.length));
      const base = { title: it.title.trim(), number_of_serving: Number(it.servings), pickup_text: pickup.trim(), request_id: answer.requestId,
        details: it.details, place, address: byAddress && picked ? { text: picked.text } : undefined };
      try {
        const photoId = photoToPost(it.photo_id, touch[it.item] ?? { kind: "idle" });
        await listings.create(photoId ? { ...base, photo_id: photoId } : { ...base, photo: shot });
        done++;
        sent.push(it);
      } catch (err) {
        if (err instanceof ApiError && err.status === 401) { busy.current = false; onBusy(false); return ended(); }
        if (one) setProblem(err instanceof Error ? err.message : String(err));
        left.push(it);
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
      const kept = notPostedItems(left, now.current.items, REVIEW.notPosted); // X49 review: as they are now (by item number)
      now.current = { items: kept, pickup: now.current.pickup };
      setItems(kept);
      setPartial(true);
      setProblem(postedSoFar.current > 0 ? postedSummary(postedSoFar.current, left.length) : REVIEW.nonePosted);
    }
  };

  const pickupField = (
    <Field ref={pickupBox} testID="pickup" label={byAddress ? PICKUP.notes : one ? REVIEW.pickup : REVIEW.pickupAll} value={pickup} multiline
      maxLength={LIMITS.pickup} placeholder={byAddress ? PICKUP.notesHint : REVIEW.pickupHint} returnKeyType="done" submitBehavior="blurAndSubmit"
      onChangeText={(t) => change(now.current.items, t.replace(/\n/g, " "), "hand")} />
  );
  const pickupMark = chat.pickupMarked ? (
    <Muted style={{ color: col.link, marginTop: space.hair, marginLeft: space.inset }}>{VOICE.markPickup}</Muted>
  ) : null;
  // X49j: the pickup part: the address (then the map), the safety line, then the instructions; or the X49e row
  const pickupPart = byAddress ? (
    <>
      <PickupAddress mapsKey={mapsKey} picked={picked} onPicked={(a) => { setPicked(a); setNeedAddress(false); }} locked={sending} onNaming={setNaming}
        onFail={() => { setMapsKey(""); setPicked(null); }} need={needAddress} needSeq={needSeq} />
      <Muted lines={2} style={{ marginTop: space.hair, marginLeft: space.inset }}>{PICKUP.safety}</Muted>
      <View style={{ marginTop: space.group }}>{pickupField}</View>{pickupMark}
    </>
  ) : <>{pickupField}{pickupMark}</>; // X49q: no "Pickup place" row: the location is taken at Post
  let fields: React.ReactNode;
  if (one) {
    const it = items[0];
    const note = sourceNote(answer.source === "barcode" ? barcode : "", it.description);
    const set = (f: Partial<ItemForm>) => change([{ ...(now.current.items[0] ?? it), ...f }], now.current.pickup, "hand"); // X48u review: the latest item
    fields = (
      <View style={{ flex: 1, minWidth: 0 }}>
        <Rise i={2}><Field testID="title" label={REVIEW.title} value={it.title} maxLength={LIMITS.title} returnKeyType="next"
          onSubmitEditing={() => pickupBox.current?.focus()}
          onChangeText={(t) => set({ title: t })} /></Rise>
        {note ? <Rise i={3}><Muted style={{ marginTop: space.inset, marginLeft: space.inset }}>{note}</Muted></Rise> : null}
        {hasMore(it) ? <Rise i={3}><View style={{ marginTop: space.inset }}>{moreFor(it, 1)}</View></Rise> : null}
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
        <Rise i={5}>{pickupPart}</Rise>
        {problem ? <ErrorText>{problem}</ErrorText> : null}
      </View>
    );
  } else {
    fields = (
      <View style={{ flex: 1, minWidth: 0 }}>
        {items.map((it, k) => (
          <Rise key={it.item} i={2 + k}>
            <ItemCard it={it} n={k + 1} touch={moreFor(it, k + 1)} mark={chat.marks[it.item]} editId={editOf(it)}
              onChange={(f) => change(now.current.items.map((x) => (x.item === it.item ? f : x)), now.current.pickup, "hand")}
              onRemove={() => { removed.current.add(it.item); change(now.current.items.filter((x) => x.item !== it.item), now.current.pickup, "hand"); }} /></Rise>
        ))}
        <Rise i={2 + items.length}>{pickupPart}</Rise>
        {problem ? <ErrorText>{problem}</ErrorText> : null}
      </View>
    );
  }
  const title = one ? REVIEW.one : partial ? notPostedTitle(items.length) : foundTitle(items.length);
  const last = one ? 6 : 3 + items.length;
  const sheetItem = !wide ? items.find((x) => x.item === moreOpen && hasMore(x)) : undefined; // X49i: a phone opens More in a sheet
  return (
    <KeyboardAvoidingView style={{ flex: 1 }} behavior={RN.Platform.OS === "ios" ? "padding" : undefined}>
      <Sheet testID={one ? "your-listing" : "review-list"} kind="large" wide extraWide={column && items.length > 0} photo={shot.uri}>
        <View style={{ flexShrink: 1, flexGrow: 1 }}>
          <Rise i={0}><Title style={{ marginBottom: space.hair }}>{title}</Title></Rise>
          {one ? null : <Rise i={1}><Muted>{REVIEW.check}</Muted></Rise>}
          {wide ? (
            // X48s: the photo stays in place; only the list column scrolls, with the chat bar under it
            <View style={{ flexDirection: "row", gap: space.foot, alignItems: "flex-start", marginTop: space.inset, flexShrink: 1, minHeight: 0 }}>
              {/* X49 (option A): the scan photo is as high as the column */}
              <View style={{ flex: 1, minWidth: 0, alignSelf: "stretch", alignItems: "flex-start" }}>
                <Image source={{ uri: shot.uri }} resizeMode="cover"
                  style={{ height: "100%", aspectRatio: size.scanPhoto, maxWidth: "100%", borderRadius: radius.card }} /></View>
              <View style={{ flex: 1, minWidth: 0, alignSelf: "stretch" }}>
                <SheetBody>{fields}{items.length > 0 && !column ? chat.thread : null}</SheetBody>
                {items.length > 0 && !sending && !column ? chat.bar : null}
              </View>
              {column && items.length > 0 ? (
                // X49i: the chat (talk or type) in the third column, frosted
                <Frost round={{ borderRadius: radius.card }} style={{ width: size.chatColumn, alignSelf: "stretch", padding: space.group }}>
                  <View testID="chat-column" style={{ flex: 1, minHeight: 0 }}>
                    <Muted>{MORE.chat}</Muted>
                    <ScrollView style={{ flexShrink: 1, flexGrow: 1 }}>{chat.thread}</ScrollView>
                    {!sending ? chat.bar : null}
                  </View>
                </Frost>
              ) : null}
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
      </Sheet>
      {sheetItem ? (
        // X49i: on a phone More opens the bottom sheet (the card does not grow): the item name, Touch up, the details
        <Popup key={sheetItem.item} testID="more-sheet" small onDismiss={closeMore} dismissName={MORE.close}>
          <Title style={{ marginBottom: space.gap }}>{sheetItem.title.trim() || MORE.title}</Title>
          {moreInner(sheetItem, items.indexOf(sheetItem) + 1)}
        </Popup>
      ) : null}
    </KeyboardAvoidingView>
  );
}
