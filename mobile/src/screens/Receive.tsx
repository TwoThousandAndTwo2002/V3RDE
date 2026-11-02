// Receive, written new for V3RDE (NEW_WORK_UI.md, UI 12; mobile/docs/style_guide.md "Receive").
// "Available now": the listings that are up, photo first, in 1, 2 or 3 columns; more come as the list scrolls
// (pull down to load again on a phone). Grab takes one; then a small sheet over its photo gives the pickup with
// a Maps link (X63, T9 Nov 2: after a grab the grabber gets the place only, one Maps pill). The user's own listings are not in this list (U19: they are under Give).
// X49e (#74, the approved "distance" mockup v2): the list is for a place (the location) and a
// distance the user picks (1, 5 or 10 mi). Food farther than that shows in gray under "Farther than N mi" (up to
// 25 mi), and its grab asks first. "Your pickup" keeps the user's grabs, with "Release it" until the timer ends.
// X49q (T9 2026-10-31: "lets drop zip code", "Everywhere"): no ZIP code. With location off ("Not now", or the phone
// says no) the list shows all food with no distance, and a link asks for the location again.
import { useCallback, useEffect, useRef, useState } from "react";
import { Linking, Modal, RefreshControl, ScrollView, View, useWindowDimensions } from "react-native";
import * as RN from "react-native";
import { ApiError, listings, type Listing } from "../api";
import { clock, isGone, loadOthers, mapLink, othersOnly, secondsLeft, timeText } from "../listing";
import { REACHES, canRelease, currentPosition, isFar, milesText, minutesLeft, readPlaceChoice, savePlaceChoice,
  splitByReach, type Place, type Reach, type Saved } from "../place";
import { layoutFor, opacity, size, space, useColors } from "../theme";
import { ChipBtn, Grid, ListingCard, Muted, Pill, Popup, Rise, Row, ScreenHead, Sheet, TextLink, Title, useNow,
  usePhoto } from "../ui";
import { Doodles as Wallpaper } from "../doodles";
import { useSession } from "../session";
import { PLACE, RECEIVE, pickupLine, servings } from "../words";

/** Opens a link outside the app: a new browser tab on the web. */
function openOut(url: string) {
  if (RN.Platform.OS === "web") window.open(url, "_blank", "noopener");
  else Linking.openURL(url).catch(() => undefined);
}

/** A small popup with a title, a line, an optional field, and two pills (the delete confirm of Give). */
function Ask({ testID, title, line, field, no, yes, busy, onNo, onYes, onDismiss }: {
  testID: string; title: string; line: string; field?: React.ReactNode; no: string; yes: string; busy?: boolean;
  onNo: () => void; onYes: () => void; onDismiss?: () => void;
}) {
  return (
    <Popup testID={testID} small onDismiss={onDismiss} dismissName={PLACE.close}>
      <Title style={{ marginBottom: space.hair }}>{title}</Title>
      <Muted>{line}</Muted>
      {field ? <View style={{ marginTop: space.gutter }}>{field}</View> : null}
      <Row style={{ marginTop: space.gutter }}>
        <Pill testID={testID + "-no"} kind="secondary" title={no} grow onPress={busy ? undefined : onNo} />
        <Pill testID={testID + "-yes"} title={yes} grow busy={busy} onPress={onYes} />
      </Row>
    </Popup>
  );
}

export default function ReceiveScreen() {
  const col = useColors();
  const { who, say, ended } = useSession();
  const page = layoutFor(useWindowDimensions().width);
  const now = useNow(1000);
  const [saved, setSaved] = useState<Saved>(() => readPlaceChoice());
  const [place, setPlace] = useState<Place | null>(null);
  const [asking, setAsking] = useState<"" | "ask" | "off">(() => (saved.choice ? "" : "ask"));
  const [noPlace, setNoPlace] = useState(false); // X49q: no location: all food, with no distance
  const [locating, setLocating] = useState(false);
  const [shown, setShown] = useState<Listing[]>([]);
  const [pickups, setPickups] = useState<Listing[]>([]);
  const [first, setFirst] = useState(true); // the first page is not here yet
  const [pulling, setPulling] = useState(false);
  const [taking, setTaking] = useState("");
  const [taken, setTaken] = useState<Listing | null>(null);
  const [farAsk, setFarAsk] = useState<Listing | null>(null);
  const [releaseAsk, setReleaseAsk] = useState<Listing | null>(null);
  const [releasing, setReleasing] = useState(false);
  // the feed outside React: the cursor, a load in progress, the end, the items, and the number of the refresh
  const feed = useRef({ next: "", busy: false, end: false, items: [] as Listing[], round: 0 });
  const grabbing = useRef(false); // one grab at a time, also before the next render
  const viewH = useRef(0);
  const takenPhoto = usePhoto(taken?.photo_url);
  const reach = saved.reach;

  const keep = (s: Saved) => { setSaved(s); savePlaceChoice(s); };
  const locate = useCallback(async (s: Saved) => {
    setLocating(true);
    try {
      const at = await currentPosition();
      setPlace(at);
      setNoPlace(false);
      keep({ choice: { how: "location" }, reach: s.reach });
      setAsking("");
    } catch {
      setAsking("off"); // location is off, or this device has none
    } finally {
      setLocating(false);
    }
  }, []); // eslint-disable-line react-hooks/exhaustive-deps -- keep only sets state and storage
  useEffect(() => { if (saved.choice?.how === "location") locate(saved); }, []); // eslint-disable-line react-hooks/exhaustive-deps -- at the start
  const notNow = () => { setAsking(""); if (!place) setNoPlace(true); };

  const pickupRound = useRef(0); // X49e review: only the answer of the last "Your pickup" load is shown
  const loadPickups = useCallback(async () => {
    const round = ++pickupRound.current;
    try {
      const got = await listings.grabbed(place);
      if (round === pickupRound.current) setPickups(got.items ?? []);
    } catch (err) {
      if (round === pickupRound.current && err instanceof ApiError && err.status === 401) ended();
    }
  }, [place, ended]);

  const more = useCallback(async (again = false) => {
    const f = feed.current; // the feed state, outside React
    if ((!place && !noPlace) || (!again && (f.busy || f.end))) return;
    const round = again ? ++f.round : f.round; // a refresh makes the answers of earlier loads stale
    f.busy = true;
    try {
      // U19: pages until one brings food of others (the user's own listings are not shown)
      const got = await loadOthers((after) => listings.near(place, after), again ? [] : f.items, again ? "" : f.next, who.id,
        () => round !== f.round);
      if (!got || round !== f.round) return;
      Object.assign(f, { items: got.items, next: got.next, end: got.next === "" });
      setShown(got.items);
    } catch (err) {
      if (round !== f.round) return;
      if (err instanceof ApiError && err.status === 401) return ended();
      say(err instanceof Error ? err.message : String(err)); // a failed refresh keeps the list on view
    } finally {
      if (round === f.round) {
        f.busy = false;
        setFirst(false);
        setPulling(false);
      }
    }
  }, [ended, say, who.id, place, noPlace]);
  useEffect(() => { if (place || noPlace) { setFirst(true); more(true); loadPickups(); } }, [more, loadPickups, place, noPlace]);

  const grab = async (l: Listing) => {
    if (grabbing.current) return;
    if (isGone(l)) return say(RECEIVE.gone);
    grabbing.current = true;
    setTaking(l.id);
    try {
      const got = await listings.grab(l.id);
      feed.current.items = feed.current.items.filter((x) => x.id !== l.id); // X49e: it moves to "Your pickup"
      setShown(feed.current.items);
      const mine = { ...l, ...got };
      pickupRound.current++; // an older "Your pickup" answer must not remove this grab
      setPickups((all) => [mine, ...all.filter((x) => x.id !== l.id)]);
      setTaken(mine);
    } catch (err) {
      if (err instanceof ApiError && err.status === 401) return ended();
      if (err instanceof ApiError && (err.status === 409 || err.status === 404)) { // collected, expired or removed
        feed.current.items = feed.current.items.map((x) => (x.id === l.id ? { ...x, status: "collected" } : x));
        setShown(feed.current.items);
        return say(RECEIVE.gone);
      }
      say(err instanceof Error ? err.message : String(err));
    } finally {
      grabbing.current = false;
      setTaking("");
    }
  };
  const tryGrab = (l: Listing) => (isFar(l, reach) ? setFarAsk(l) : grab(l)); // X49e: a far grab asks first

  const release = async (l: Listing) => {
    if (releasing) return;
    setReleasing(true);
    try {
      await listings.release(l.id);
      pickupRound.current++;
      setPickups((all) => all.filter((x) => x.id !== l.id));
      say(PLACE.released);
      more(true);
    } catch (err) {
      if (err instanceof ApiError && err.status === 401) return ended();
      say(err instanceof ApiError && (err.status === 409 || err.status === 404) ? PLACE.releaseEnded
        : err instanceof Error ? err.message : String(err));
      loadPickups();
    } finally {
      setReleasing(false);
      setReleaseAsk(null);
    }
  };

  useEffect(function escCloses() {
    if (RN.Platform.OS !== "web" || !taken) return;
    const escape = (e: KeyboardEvent) => e.key === "Escape" && setTaken(null);
    window.addEventListener("keydown", escape);
    return () => window.removeEventListener("keydown", escape);
  }, [taken]);

  const side = page.topBar ? space.pad : space.gutter;
  const others = othersOnly(shown, who.id);
  const { near, far } = splitByReach(others, reach);
  const line = (l: Listing) => servings(l.number_of_serving) + (l.distance_mi !== undefined ? " · " + milesText(l.distance_mi) : "");
  const card = (l: Listing, gray: boolean) => {
    const gone = isGone(l, now);
    return (
      <View key={l.id} style={{ opacity: gray ? opacity.gone : 1 }}>
        <ListingCard testID={"card-" + l.id} photoPath={l.photo_url} title={l.title} line={line(l)}
          time={timeText(l, RECEIVE, now)} timeName={RECEIVE.timeLeft} gone={gone} tall extra={(
            <>
              {/* X49j: before a grab, only the instructions (the address shows after the grab) */}
              {l.pickup_text ? <Muted lines={1} style={{ opacity: gone ? opacity.gone : 1 }}>{l.pickup_text}</Muted> : null}
              {!gone ? (
                <View style={{ marginTop: space.gap }}>
                  <Pill testID={"grab-" + l.id} title={RECEIVE.grab} name={RECEIVE.grab + ": " + l.title} busy={taking === l.id}
                    busyName={RECEIVE.grabbing} onPress={() => tryGrab(l)} /></View>
              ) : null}
            </>
          )} /></View>
    );
  };
  const pickupCard = (l: Listing) => {
    const left = secondsLeft(l, now);
    return (
      <ListingCard key={l.id} testID={"pickup-" + l.id} photoPath={l.photo_url} title={l.title} line={line(l)}
        time={left === 0 ? RECEIVE.expired : clock(left)} timeName={RECEIVE.timeLeft} gone={left === 0} tall extra={(
          <>
            <Muted lines={2} style={{ opacity: left === 0 ? opacity.gone : 1 }}>{pickupLine(l)}</Muted>
            <Row style={{ marginTop: space.gap }}>
              <Pill testID={"maps-" + l.id} title={RECEIVE.maps} grow onPress={() => openOut(mapLink(l.pickup_address?.text || l.pickup_text))} />
            </Row>
            {canRelease(l, who.id, now) ? (
              <View style={{ marginTop: space.gap, alignSelf: "center" }}>
                <TextLink testID={"release-" + l.id} name={PLACE.releaseName(l.title)} onPress={() => setReleaseAsk(l)}>{PLACE.release}</TextLink></View>
            ) : null}
          </>
        )} />
    );
  };
  const head = place ? (
    <View style={{ marginBottom: space.gutter, gap: space.gap }}>
      <Row tight>
        {REACHES.map((d) => <ChipBtn key={d} testID={"reach-" + d} title={PLACE.reach(d)} name={PLACE.reachName(d)} on={d === reach}
          onPress={() => keep({ ...saved, reach: d as Reach })} />)}
      </Row>
      <Row tight><Muted>{PLACE.nearLocation}</Muted>
        <TextLink testID="place-change" name={PLACE.changeName} onPress={() => setAsking("ask")}>{PLACE.change}</TextLink></Row>
    </View>
  ) : noPlace ? ( // X49q: location off: all food, no distance
    <View style={{ marginBottom: space.gutter }}>
      <Row tight><Muted>{PLACE.noDistance}</Muted>
        <TextLink testID="place-use" onPress={() => locate(saved)}>{PLACE.useLocation}</TextLink></Row>
    </View>
  ) : null;
  let body: React.ReactNode;
  if ((!place && !noPlace) || first) body = <Muted>{locating ? PLACE.locating : RECEIVE.loading}</Muted>;
  else {
    const empty = near.length === 0 && far.length === 0;
    body = (
      <>
        {pickups.length > 0 ? (
          <View testID="your-pickup" style={{ marginBottom: space.gutter }}>
            <Muted style={{ marginBottom: space.gap }}>{PLACE.yourPickup}</Muted>
            <Grid columns={page.columns}>{pickups.map(pickupCard)}</Grid>
            {near.length > 0 ? <Muted style={{ marginTop: space.gutter }}>{place ? PLACE.moreNear : RECEIVE.title}</Muted> : null}
          </View>
        ) : null}
        {empty && !feed.current.end ? <Muted>{RECEIVE.loading}</Muted>
          : empty ? <Muted center style={{ paddingVertical: space.bar }}>{RECEIVE.empty}</Muted>
          : near.length === 0 ? <Muted center lines={3} style={{ paddingVertical: space.bar }}>{PLACE.emptyNear(reach)}</Muted>
          : <Grid columns={page.columns}>{near.map((l) => card(l, false))}</Grid>}
        {far.length > 0 ? (
          <View testID="farther" style={{ marginTop: space.gutter }}>
            <Muted style={{ marginBottom: space.gap }}>{PLACE.farther(reach)}</Muted>
            <Grid columns={page.columns}>{far.map((l) => card(l, true))}</Grid>
          </View>
        ) : null}
      </>
    );
  }
  return (
    <View style={{ flex: 1, minHeight: 0 }}>
      <Wallpaper />
      <ScrollView scrollEventThrottle={200}
        contentContainerStyle={{ width: "100%", maxWidth: page.maxWidth, alignSelf: "center", paddingHorizontal: side,
          paddingTop: page.topBar ? space.pad : 0, paddingBottom: space.pad }}
        onScroll={(ev) => {
          const { layoutMeasurement: view, contentOffset: at, contentSize: all } = ev.nativeEvent;
          if (view.height + at.y >= all.height - size.loadAhead) more();
        }}
        onLayout={(ev) => { viewH.current = ev.nativeEvent.layout.height; }}
        onContentSizeChange={(_w, h) => { if (viewH.current > 0 && h < viewH.current + size.loadAhead) more(); }}
        refreshControl={RN.Platform.OS === "web" ? undefined : (
          <RefreshControl refreshing={pulling} tintColor={col.accent} onRefresh={() => { setPulling(true); more(true); loadPickups(); }} />)}>
        <ScreenHead title={RECEIVE.title} />
        {head}
        {body}</ScrollView>
      <Modal visible={!!taken} transparent animationType="none" statusBarTranslucent onRequestClose={() => setTaken(null)}>
        {taken ? (
          <Sheet testID="grab-sheet" kind="small" photo={takenPhoto || undefined} onClose={() => setTaken(null)} closeName={RECEIVE.close}>
            <Rise i={0}><Title center>{RECEIVE.grabbed}</Title></Rise>
            <Rise i={1}><Muted center lines={2} style={{ marginTop: space.hair }}>{taken.title + " · " + pickupLine(taken)}</Muted></Rise>
            <Rise i={2}><Row style={{ marginTop: space.gutter }}>
              <Pill testID="maps" title={RECEIVE.maps} grow onPress={() => openOut(mapLink(taken.pickup_address?.text || taken.pickup_text))} />
            </Row></Rise>
          </Sheet>
        ) : null}
      </Modal>
      {asking === "ask" ? (
        <Ask testID="place-ask" title={PLACE.askTitle} line={PLACE.askLine} no={PLACE.notNow} yes={PLACE.useLocation} busy={locating}
          onNo={notNow} onYes={() => locate(saved)} onDismiss={locating ? undefined : notNow} />
      ) : null}
      {asking === "off" ? ( // X49q: location is off, or the phone has none: no ZIP; all food, with no distance
        <Ask testID="place-off" title={PLACE.offTitle} line={PLACE.offLine} no={PLACE.notNow} yes={PLACE.tryAgain} busy={locating}
          onNo={notNow} onYes={() => locate(saved)} onDismiss={locating ? undefined : notNow} />
      ) : null}
      {farAsk ? (
        <Ask testID="far-ask" title={PLACE.farTitle(milesText(farAsk.distance_mi))} line={PLACE.farLine(reach, minutesLeft(farAsk, now))}
          no={PLACE.cancel} yes={RECEIVE.grab} onNo={() => setFarAsk(null)} onDismiss={() => setFarAsk(null)}
          onYes={() => { const l = farAsk; setFarAsk(null); grab(l); }} />
      ) : null}
      {releaseAsk ? (
        <Ask testID="release-ask" title={PLACE.releaseTitle(releaseAsk.title)} line={PLACE.releaseLine} no={PLACE.keep}
          yes={PLACE.releaseYes} busy={releasing} onNo={() => setReleaseAsk(null)}
          onDismiss={releasing ? undefined : () => setReleaseAsk(null)} onYes={() => release(releaseAsk)} />
      ) : null}
    </View>
  );
}
