// The pickup address of the review, written new for V3RDE (X49j, #80; mobile/docs/style_guide.md "Review"; rules and
// the Maps calls in src/maps.ts). T9 2026-10-31: "we need to start with address and the user selects from auto
// complete. but there is only one suggestion given. then once this is locked in map can render."
// X49q (T9 2026-10-31, tester feedback; the approved "No ZIP" mockup v2, then T9's changes):
// - the map moves (the pin stays in its centre: the point is the centre), and the address follows the pin when it
//   moves more than ADDRESS_FOLLOW_M ("can the user set the location on a map and have it be another way to update
//   the address? ... yes");
// - "Use my current location" fills the address from the phone location;
// - the "choose" error shows under the field;
// - no "Change the address" link: a tap on the address box changes it ("its implied that they want to change the
//   address"); then the block moves up and the map grows to size.mapTall; a tap outside the block closes it.
import { useEffect, useRef, useState } from "react";
import { Platform, Pressable, View } from "react-native";
import { ADDRESS_FOLLOW_M, NAME_LIMIT_MS, NAME_WAIT_MS, SUGGEST_MIN, SUGGEST_WAIT_MS, addressAt, addressLines, drawMap, loadMaps, metersBetween,
  newSession, suggest, type DrawnMap, type PickedAddress, type Suggestion } from "../maps";
import { currentPosition } from "../place";
import { opacity, radius, size, space, useColors } from "../theme";
import { Body, ErrorText, Field, GlyphIcon, IconBtn, Muted } from "../ui";
import { PICKUP } from "../words";

type Point = { lat: number; lng: number };

/** The map of the chosen address (web: the Maps JavaScript API), with the pin of the app in its centre. The giver
 *  moves the map to put the pin on the pickup spot; `onMove` gets the new centre. Drawn again only for a new
 *  `drawKey` (a new address from the search or the location), never for a move; a new height keeps the map. */
function MapBox({ mapsKey, at, name, drawKey, tall, still, onMove }: { mapsKey: string; at: Point; name: string; drawKey: number;
  tall: boolean; still: boolean; onMove: (p: Point) => void }) {
  const col = useColors();
  const box = useRef<React.ComponentRef<typeof View>>(null);
  const move = useRef(onMove);
  move.current = onMove;
  const h = tall ? size.mapTall : size.mapH;
  const drawn = useRef<DrawnMap | null>(null);
  const stillNow = useRef(still);
  stillNow.current = still;
  useEffect(function draw() {
    const el = box.current as unknown as HTMLElement | null;
    let on = true;
    if (el) drawMap(mapsKey, el, at, (p) => move.current(p)).then((m) => {
      if (!on) return m.off();
      drawn.current = m;
      if (stillNow.current) m.hold(true);
    }).catch(() => undefined);
    return () => { on = false; drawn.current?.off(); drawn.current = null; };
  }, [mapsKey, drawKey]); // eslint-disable-line react-hooks/exhaustive-deps -- a new address decides, not each move
  useEffect(() => { drawn.current?.hold(still); }, [still]); // X49q review: no moves while the listing posts
  return (
    <View testID="pickup-map" accessible accessibilityLabel={name} aria-label={name} style={{ height: h, marginTop: space.gap,
      borderRadius: radius.card, overflow: "hidden", borderWidth: size.border, borderColor: col.line, backgroundColor: col.iconFill }}>
      {/* the size is set here too: the Maps API sets its own position on this element */}
      <View ref={box} style={{ position: "absolute", top: 0, left: 0, right: 0, bottom: 0, width: "100%", height: h }} />
      <View pointerEvents="none" style={{ position: "absolute", top: 0, left: 0, right: 0, bottom: 0, alignItems: "center", justifyContent: "center" }}>
        <View style={{ width: size.iconSmall, height: size.iconSmall, borderRadius: radius.round, backgroundColor: col.accent,
          alignItems: "center", justifyContent: "center" }}><GlyphIcon name="pin" color={col.onAccent} /></View>
      </View>
    </View>
  );
}

export function PickupAddress({ mapsKey, picked, onPicked, locked, onFail, need, needSeq = 0, onNaming }: {
  mapsKey: string; picked: PickedAddress | null; onPicked: (a: PickedAddress | null) => void;
  /** The listings post: no change. */
  locked: boolean;
  /** X49j review: Maps does not load (no script, a refused key): the review takes the location at Post. */
  onFail: () => void;
  /** X49q: Post was pressed with no address: the error shows under the field. */
  need?: boolean;
  /** X49w: goes up at each Post that has no address: the page scrolls to this field (T9: "the screen should scroll to the address area"). */
  needSeq?: number;
  /** X49q review: true while the address of a moved pin is being found (Post waits: the text must match the pin). */
  onNaming?: (busy: boolean) => void;
}) {
  const col = useColors();
  const [text, setText] = useState("");
  const [hit, setHit] = useState<Suggestion | null>(null);
  const [problem, setProblem] = useState("");
  const [busy, setBusy] = useState(false);
  const [finding, setFinding] = useState(false); // "Use my current location" runs
  const [editing, setEditing] = useState(false); // X49q: the giver changes a chosen address: the large map
  const [drawKey, setDrawKey] = useState(0); // a new address from the search or the location: the map is drawn again
  const [naming, setNaming] = useState(false); // the address of a moved pin is being found
  const typed = useRef(""); // the field now, for a late address answer (X49q review: never over what the user typed)
  typed.current = text;
  const block = useRef<React.ComponentRef<typeof View>>(null);
  const now = useRef(picked); // the address now, for the map's moves (a move answers after renders)
  now.current = picked;
  const named = useRef<Point | null>(null); // the point of the address text now
  const geoRound = useRef(0); // only the answer to the last move counts
  const session = useRef<Promise<unknown> | null>(null); // one Places session from the first key to the choice (billing)
  useEffect(function load() { // the script loads at once; no script in MAPS_LOAD_MS: the location at Post
    let on = true;
    loadMaps(mapsKey).catch(() => { if (on) onFail(); });
    return () => { on = false; };
  }, [mapsKey]); // eslint-disable-line react-hooks/exhaustive-deps -- onFail is a new function each render
  const round = useRef(0); // only the answer to the last text counts
  useEffect(function ask() {
    if (picked && !editing) return;
    const t = text.trim();
    const r = ++round.current;
    if (t.length < SUGGEST_MIN || (picked && t === picked.text)) { setHit(null); setProblem(""); return; }
    const id = setTimeout(async () => {
      try {
        session.current = session.current ?? newSession(mapsKey); // one token, also for two fast rounds
        const got = await suggest(mapsKey, t, await session.current);
        if (r !== round.current) return;
        setHit(got);
        setProblem(got ? "" : PICKUP.none);
      } catch {
        if (r === round.current) { setHit(null); setProblem(PICKUP.failed); session.current = null; }
      }
    }, SUGGEST_WAIT_MS);
    return () => clearTimeout(id);
  }, [text, picked, editing, mapsKey]);
  const seenNeed = useRef(needSeq); // X49w review: a new mount does not scroll for an older Post
  useEffect(function showNeed() { // X49w: Post with no address brings the address field into view
    if (needSeq === seenNeed.current || Platform.OS !== "web") return;
    seenNeed.current = needSeq;
    (block.current as unknown as HTMLElement | null)?.scrollIntoView?.({ block: "center", behavior: "smooth" });
  }, [needSeq]);
  // X49q: the block moves to the top of the page, and a tap outside it closes the change (web: the map is web only)
  useEffect(function edit() {
    if (!editing || Platform.OS !== "web") return;
    const el = block.current as unknown as HTMLElement | null;
    el?.scrollIntoView?.({ block: "start", behavior: "smooth" });
    // a click, not a pointerdown: a finger that scrolls the page makes no click (X49q review)
    const outside = (e: Event) => { if (el && !el.contains(e.target as Node)) { setEditing(false); setHit(null); setProblem(""); } };
    document.addEventListener("click", outside, true);
    return () => document.removeEventListener("click", outside, true);
  }, [editing]);
  /** A new address from the search or the location: drawn again, and its point names the text. */
  const fresh = (a: PickedAddress) => {
    session.current = null; // the session ends with the choice
    setHit(null);
    named.current = { lat: a.lat, lng: a.lng };
    geoRound.current++;
    name(false);
    setDrawKey((k) => k + 1);
    onPicked(a);
  };
  const choose = async () => {
    if (!hit || busy || locked) return;
    setBusy(true);
    try {
      fresh(await hit.pick());
      setEditing(false);
    } catch {
      setProblem(PICKUP.failed);
    } finally {
      setBusy(false);
    }
  };
  const here = async () => { // the phone location, and its address (reverse geocoding)
    if (finding || locked) return;
    setFinding(true);
    setProblem("");
    try {
      const at = await currentPosition();
      const t = await addressAt(mapsKey, at).catch(() => "");
      fresh({ text: t || at.lat.toFixed(5) + ", " + at.lng.toFixed(5), lat: at.lat, lng: at.lng });
      setEditing(false);
    } catch {
      setProblem(PICKUP.hereOff);
    } finally {
      setFinding(false);
    }
  };
  const name = (on: boolean) => { setNaming(on); onNaming?.(on); };
  useEffect(() => () => { geoRound.current++; }, []); // closed: a waiting lookup does nothing
  /** X49q: the map stopped: the point is its centre; the address follows when the pin moved more than ADDRESS_FOLLOW_M.
   *  Every move cancels an earlier lookup; the lookup waits NAME_WAIT_MS (a second move in that time asks nothing);
   *  no address for the point: its coordinates (X49q review: the text always matches the pin). */
  const moved = (p: Point) => {
    const a = now.current;
    if (!a || locked) return;
    if (a.lat !== p.lat || a.lng !== p.lng) onPicked({ ...a, lat: p.lat, lng: p.lng });
    const r = ++geoRound.current;
    if (named.current && metersBetween(named.current, p) <= ADDRESS_FOLLOW_M) { name(false); return; }
    name(true);
    const was = typed.current;
    setTimeout(() => {
      if (r !== geoRound.current) return;
      const late = new Promise<string>((go) => setTimeout(() => go(""), NAME_LIMIT_MS)); // no answer: the coordinates
      Promise.race([addressAt(mapsKey, p).catch(() => ""), late]).then((found) => {
        if (r !== geoRound.current || !now.current) return; // a later move or a new address wins
        const t = found || p.lat.toFixed(5) + ", " + p.lng.toFixed(5);
        named.current = p;
        onPicked({ text: t, lat: p.lat, lng: p.lng }); // the point of this move: the address may answer before a render
        if (typed.current === was) setText(t); // the field changes only when the user did not type meanwhile
        name(false);
      });
    }, NAME_WAIT_MS);
  };
  if (!named.current && picked) named.current = { lat: picked.lat, lng: picked.lng };

  // X49w (T9: "a simple button to use current location ... an icon, i prefer this better than underlined text"):
  // a round icon button inside the field; while it works, the line under the field says so
  const hereBtn = <IconBtn testID="pickup-here" glyph="locate" label={finding ? PICKUP.finding : PICKUP.here} onPress={here} />;
  const hereLink = finding ? <Muted style={{ marginTop: space.hair, marginStart: space.inset }}>{PICKUP.finding}</Muted> : null;
  const suggestion = hit ? (
    <Pressable testID="pickup-suggestion" accessibilityRole="button" accessibilityLabel={hit.main + ", " + hit.secondary} onPress={choose}
      style={(state) => ({ marginTop: space.hair, paddingHorizontal: space.group, paddingVertical: space.inset, borderRadius: radius.card,
        borderWidth: size.border, borderColor: col.line, backgroundColor: col.card, opacity: busy ? opacity.busy : state.pressed ? opacity.pressed : 1 })}>
      <Body>{hit.main}</Body>{hit.secondary ? <Muted>{hit.secondary}</Muted> : null}
    </Pressable>
  ) : null;
  const field = (
    <Field testID="pickup-address" label={PICKUP.address} value={text} placeholder={PICKUP.hint} maxLength={200} autoFocus={editing}
      autoComplete="street-address" onChangeText={setText} right={hereBtn} />
  );
  if (picked) { // one block and one map for both modes (X49q review: a mode change keeps the map, no new map load)
    const l = addressLines(picked.text);
    return (
      <View ref={block} testID={editing ? "pickup-address-edit" : "pickup-address-locked"} style={{ marginTop: space.group }}>
        {editing ? (
          <>
            {field}{suggestion}
            {problem ? <ErrorText>{problem}</ErrorText> : <Muted lines={2} style={{ marginTop: space.hair, marginStart: space.inset }}>{naming ? PICKUP.naming : PICKUP.editHint}</Muted>}
            {hereLink}
          </>
        ) : (
          <>
            <Muted style={{ marginStart: space.inset, marginBottom: space.hair }}>{PICKUP.address}</Muted>
            <Pressable testID="pickup-address-box" accessibilityRole="button" accessibilityLabel={PICKUP.changeName(picked.text)}
              disabled={locked} onPress={() => { setText(picked.text); setProblem(""); setEditing(true); }}
              style={(state) => ({ paddingHorizontal: space.group, paddingVertical: space.inset, borderRadius: radius.card, borderWidth: size.border,
                borderColor: col.line, backgroundColor: col.card, opacity: state.pressed ? opacity.pressed : 1 })}>
              {naming ? <Muted>{PICKUP.naming}</Muted> : <><Body>{l.main}</Body>{l.rest ? <Muted>{l.rest}</Muted> : null}</>}</Pressable>
          </>
        )}
        <MapBox mapsKey={mapsKey} at={picked} name={PICKUP.mapName(picked.text)} drawKey={drawKey} tall={editing} still={locked} onMove={moved} />
        {editing ? null : <Muted lines={2} style={{ marginTop: space.hair, marginStart: space.inset }}>{PICKUP.moveHint}</Muted>}
      </View>
    );
  }
  return (
    <View ref={block} style={{ marginTop: space.group }}>
      {field}{suggestion}
      {problem ? <ErrorText>{problem}</ErrorText> : need ? <ErrorText>{PICKUP.need}</ErrorText>
        : <Muted lines={2} style={{ marginTop: space.hair, marginStart: space.inset }}>{PICKUP.choose}</Muted>}
      {hereLink}
    </View>
  );
}
