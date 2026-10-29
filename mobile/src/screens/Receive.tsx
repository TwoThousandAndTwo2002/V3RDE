// Receive, written new for V3RDE (NEW_WORK_UI.md, UI 12; mobile/docs/style_guide.md "Receive").
// "Available now": the listings that are up, photo first, in 1, 2 or 3 columns; more come as the list scrolls
// (pull down to load again on a phone). Grab takes one; then a small sheet over its photo gives the pickup with
// Maps and WhatsApp links. The user's own listings are not in this list (U19: they are under Give).
import { useCallback, useEffect, useRef, useState } from "react";
import { Linking, Modal, RefreshControl, ScrollView, View, useWindowDimensions } from "react-native";
import * as RN from "react-native";
import { ApiError, listings, type Listing } from "../api";
import { chatLink, isGone, loadOthers, mapLink, othersOnly, timeText, withChange } from "../listing";
import { layoutFor, size, space, useColors } from "../theme";
import { Grid, ListingCard, Muted, Pill, Rise, Row, ScreenHead, Sheet, Title, useNow, usePhoto } from "../ui";
import { Doodles as Wallpaper } from "../doodles";
import { useSession } from "../session";
import { RECEIVE, grabMessage, servings } from "../words";

/** Opens a link outside the app: a new browser tab on the web. */
function openOut(url: string) {
  if (RN.Platform.OS === "web") window.open(url, "_blank", "noopener");
  else Linking.openURL(url).catch(() => undefined);
}

export default function ReceiveScreen() {
  const col = useColors();
  const { who, say, ended } = useSession();
  const page = layoutFor(useWindowDimensions().width);
  const now = useNow(1000);
  const [shown, setShown] = useState<Listing[]>([]);
  const [first, setFirst] = useState(true); // the first page is not here yet
  const [pulling, setPulling] = useState(false);
  const [taking, setTaking] = useState("");
  const [taken, setTaken] = useState<Listing | null>(null);
  // the feed outside React: the cursor, a load in progress, the end, the items, and the number of the refresh
  const feed = useRef({ next: "", busy: false, end: false, items: [] as Listing[], round: 0 });
  const grabbing = useRef(false); // one grab at a time, also before the next render
  const viewH = useRef(0);
  const takenPhoto = usePhoto(taken?.photo_url);

  const more = useCallback(async (again = false) => {
    const f = feed.current; // the feed state, outside React
    if (!again && (f.busy || f.end)) return;
    const round = again ? ++f.round : f.round; // a refresh makes the answers of earlier loads stale
    f.busy = true;
    try {
      // U19: pages until one brings food of others (the user's own listings are not shown)
      const got = await loadOthers((after) => listings.active(after), again ? [] : f.items, again ? "" : f.next, who.id,
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
  }, [ended, say, who.id]);
  useEffect(() => { more(true); }, [more]);

  const grab = async (l: Listing) => {
    if (grabbing.current) return;
    if (isGone(l)) return say(RECEIVE.gone);
    grabbing.current = true;
    setTaking(l.id);
    try {
      const got = await listings.grab(l.id);
      feed.current.items = withChange(feed.current.items, got);
      setShown(feed.current.items);
      setTaken({ ...l, ...got });
    } catch (err) {
      if (err instanceof ApiError && err.status === 401) return ended();
      if (err instanceof ApiError && (err.status === 409 || err.status === 404)) { // collected, expired or removed
        feed.current.items = withChange(feed.current.items, { id: l.id, status: "collected" });
        setShown(feed.current.items);
        return say(RECEIVE.gone);
      }
      say(err instanceof Error ? err.message : String(err));
    } finally {
      grabbing.current = false;
      setTaking("");
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
  const cards = others.map((l) => {
    const gone = isGone(l, now);
    return (
      <ListingCard key={l.id} testID={"card-" + l.id} photoPath={l.photo_url} title={l.title} line={servings(l.number_of_serving)}
        time={timeText(l, RECEIVE, now)} timeName={RECEIVE.timeLeft} gone={gone} tall extra={(
          <>
            <Muted lines={1}>{l.pickup_text}</Muted>
            {!gone ? (
              <View style={{ marginTop: space.gap }}>
                <Pill testID={"grab-" + l.id} title={RECEIVE.grab} name={RECEIVE.grab + ": " + l.title} busy={taking === l.id}
                  busyName={RECEIVE.grabbing} onPress={() => grab(l)} /></View>
            ) : null}
          </>
        )} />
    );
  });
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
          <RefreshControl refreshing={pulling} tintColor={col.accent} onRefresh={() => { setPulling(true); more(true); }} />)}>
        <ScreenHead title={RECEIVE.title} />
        {first || (others.length === 0 && !feed.current.end) ? <Muted>{RECEIVE.loading}</Muted> : others.length === 0
          ? <Muted center style={{ paddingVertical: space.bar }}>{RECEIVE.empty}</Muted>
          : <Grid columns={page.columns}>{cards}</Grid>}</ScrollView>
      <Modal visible={!!taken} transparent animationType="none" statusBarTranslucent onRequestClose={() => setTaken(null)}>
        {taken ? (
          <Sheet testID="grab-sheet" kind="small" photo={takenPhoto || undefined} onClose={() => setTaken(null)} closeName={RECEIVE.close}>
            <Rise i={0}><Title center>{RECEIVE.grabbed}</Title></Rise>
            <Rise i={1}><Muted center lines={2} style={{ marginTop: space.hair }}>{taken.title + " · " + taken.pickup_text}</Muted></Rise>
            <Rise i={2}><Row style={{ marginTop: space.gutter }}>
              <Pill testID="maps" kind="secondary" title={RECEIVE.maps} grow onPress={() => openOut(mapLink(taken.pickup_text))} />
              <Pill testID="chat" title={RECEIVE.chat} grow onPress={() => openOut(chatLink(grabMessage(taken, who.email)))} />
            </Row></Rise>
          </Sheet>
        ) : null}
      </Modal>
    </View>
  );
}
