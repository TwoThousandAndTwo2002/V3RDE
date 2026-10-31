// The pickup address of the review, written new for V3RDE (X49j, #80; mobile/docs/style_guide.md "Review"; rules and
// the Maps calls in src/maps.ts). T9 2026-10-31: "we need to start with address and the user selects from auto
// complete. but there is only one suggestion given. then once this is locked in map can render."
import { useEffect, useRef, useState } from "react";
import { Pressable, View } from "react-native";
import { SUGGEST_MIN, SUGGEST_WAIT_MS, addressLines, drawMap, loadMaps, newSession, suggest, type PickedAddress, type Suggestion } from "../maps";
import { opacity, radius, size, space, useColors } from "../theme";
import { Body, ErrorText, Field, GlyphIcon, Muted, TextLink } from "../ui";
import { PICKUP } from "../words";

/** The small map of the chosen address (web: the Maps JavaScript API), with the pin of the app in its centre. */
function MapBox({ mapsKey, at, name }: { mapsKey: string; at: { lat: number; lng: number }; name: string }) {
  const col = useColors();
  const box = useRef<React.ComponentRef<typeof View>>(null);
  useEffect(function draw() {
    const el = box.current as unknown as HTMLElement | null;
    if (el) drawMap(mapsKey, el, at).catch(() => undefined); // no map: the address text stays on view
  }, [mapsKey, at.lat, at.lng]); // eslint-disable-line react-hooks/exhaustive-deps -- the point decides
  return (
    <View accessible accessibilityLabel={name} aria-label={name} style={{ height: size.mapH, marginTop: space.gap, borderRadius: radius.card,
      overflow: "hidden", borderWidth: size.border, borderColor: col.line, backgroundColor: col.iconFill }}>
      {/* the size is set here too: the Maps API sets its own position on this element */}
      <View ref={box} style={{ position: "absolute", top: 0, left: 0, right: 0, bottom: 0, width: "100%", height: size.mapH }} />
      <View pointerEvents="none" style={{ position: "absolute", top: 0, left: 0, right: 0, bottom: 0, alignItems: "center", justifyContent: "center" }}>
        <View style={{ width: size.iconSmall, height: size.iconSmall, borderRadius: radius.round, backgroundColor: col.accent,
          alignItems: "center", justifyContent: "center" }}><GlyphIcon name="pin" color={col.onAccent} /></View>
      </View>
    </View>
  );
}

export function PickupAddress({ mapsKey, picked, onPicked, locked, onFail }: {
  mapsKey: string; picked: PickedAddress | null; onPicked: (a: PickedAddress | null) => void;
  /** The listings post: no change. */
  locked: boolean;
  /** X49j review: Maps does not work (no script, a refused key, the search fails): the review shows the X49e place row. */
  onFail: () => void;
}) {
  const col = useColors();
  const [text, setText] = useState("");
  const [hit, setHit] = useState<Suggestion | null>(null);
  const [problem, setProblem] = useState("");
  const [busy, setBusy] = useState(false);
  const session = useRef<Promise<unknown> | null>(null); // one Places session from the first key to the choice (billing)
  useEffect(function load() { // the script loads at once; no script in MAPS_LOAD_MS: the place row
    let on = true;
    loadMaps(mapsKey).catch(() => { if (on) onFail(); });
    return () => { on = false; };
  }, [mapsKey]); // eslint-disable-line react-hooks/exhaustive-deps -- onFail is a new function each render
  const round = useRef(0); // only the answer to the last text counts
  useEffect(function ask() {
    if (picked) return;
    const t = text.trim();
    const r = ++round.current;
    if (t.length < SUGGEST_MIN) { setHit(null); setProblem(""); return; }
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
  }, [text, picked, mapsKey]);
  const choose = async () => {
    if (!hit || busy || locked) return;
    setBusy(true);
    try {
      const a = await hit.pick();
      session.current = null; // the session ends with the choice
      setHit(null);
      onPicked(a);
    } catch {
      setProblem(PICKUP.failed);
    } finally {
      setBusy(false);
    }
  };
  if (picked) {
    const l = addressLines(picked.text);
    return (
      <View testID="pickup-address-locked" style={{ marginTop: space.group }}>
        <Muted style={{ marginLeft: space.inset, marginBottom: space.hair }}>{PICKUP.address}</Muted>
        <View style={{ paddingHorizontal: space.group, paddingVertical: space.inset, borderRadius: radius.card, borderWidth: size.border,
          borderColor: col.line, backgroundColor: col.card }}>
          <Body>{l.main}</Body>{l.rest ? <Muted>{l.rest}</Muted> : null}</View>
        {locked ? null : (
          <View style={{ marginTop: space.hair, marginLeft: space.inset, alignSelf: "flex-start" }}>
            <TextLink testID="pickup-address-change" onPress={() => { setText(""); onPicked(null); }}>{PICKUP.change}</TextLink></View>
        )}
        <MapBox mapsKey={mapsKey} at={picked} name={PICKUP.mapName(picked.text)} />
      </View>
    );
  }
  return (
    <View style={{ marginTop: space.group }}>
      <Field testID="pickup-address" label={PICKUP.address} value={text} placeholder={PICKUP.hint} maxLength={200}
        autoComplete="street-address" onChangeText={setText} />
      {hit ? (
        <Pressable testID="pickup-suggestion" accessibilityRole="button" accessibilityLabel={hit.main + ", " + hit.secondary} onPress={choose}
          style={(state) => ({ marginTop: space.hair, paddingHorizontal: space.group, paddingVertical: space.inset, borderRadius: radius.card,
            borderWidth: size.border, borderColor: col.line, backgroundColor: col.card, opacity: busy ? opacity.busy : state.pressed ? opacity.pressed : 1 })}>
          <Body>{hit.main}</Body>{hit.secondary ? <Muted>{hit.secondary}</Muted> : null}
        </Pressable>
      ) : null}
      {problem ? <ErrorText>{problem}</ErrorText> : <Muted lines={2} style={{ marginTop: space.hair, marginLeft: space.inset }}>{PICKUP.choose}</Muted>}
      {problem === PICKUP.failed ? (
        <View style={{ marginTop: space.hair, marginLeft: space.inset, alignSelf: "flex-start" }}>
          <TextLink testID="pickup-use-location" onPress={onFail}>{PICKUP.useLocation}</TextLink></View>
      ) : null}
    </View>
  );
}
