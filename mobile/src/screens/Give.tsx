// Give, written new for V3RDE (NEW_WORK_UI.md, UI 13; mobile/docs/style_guide.md "Give").
// "Your listings": the listings of the giver, photo first, in 1, 2 or 3 columns, with the live time pill.
// "+ New listing" opens the new-listing flow (Capture); it opens by itself when nothing of the giver is up.
// Phone: the pill is at the bottom. Wide screen: it is next to the title.
import { useCallback, useEffect, useRef, useState } from "react";
import { ScrollView, View, useWindowDimensions } from "react-native";
import { ApiError, listings, type Listing } from "../api";
import { cameraFirst, isGone, timeText } from "../listing";
import { layoutFor, space } from "../theme";
import { Grid, ListingCard, Muted, Pill, ScreenHead, useNow } from "../ui";
import NewListing from "./Capture";
import { Doodles as Wallpaper } from "../doodles";
import { useSession } from "../session";
import { GIVE, RECEIVE, postedText, servings } from "../words";

export default function GiveScreen() {
  const { say, ended } = useSession();
  const page = layoutFor(useWindowDimensions().width);
  const clockNow = useNow(1000);
  const [mine, setMine] = useState<Listing[] | null>(null); // null until the first answer
  const [capturing, setCapturing] = useState(false); // the new-listing flow is open
  const arrived = useRef(true); // the camera opens by itself only when the screen comes, never after a post
  const reload = useCallback(async () => {
    try {
      const got = (await listings.mine()).items;
      setMine(got);
      if (arrived.current && cameraFirst(got)) setCapturing(true);
    } catch (err) {
      if (err instanceof ApiError && err.status === 401) return ended();
      say(err instanceof Error ? err.message : String(err));
      setMine([]);
    } finally {
      arrived.current = false;
    }
  }, [ended, say]);
  useEffect(() => { reload(); }, [reload]);
  const side = page.topBar ? space.pad : space.gutter;
  const lane = { width: "100%", maxWidth: page.maxWidth, alignSelf: "center", paddingHorizontal: side } as const;
  const add = <Pill testID="new" title={GIVE.add} onPress={() => setCapturing(true)} />;
  return (
    <View style={{ flex: 1, minHeight: 0 }}>
      <Wallpaper />
      <ScrollView contentContainerStyle={[lane, { paddingTop: page.topBar ? space.pad : 0, paddingBottom: space.pad }]}>
        <ScreenHead title={GIVE.title} action={page.dialogs ? add : null} />
        {mine === null ? <Muted>{GIVE.loading}</Muted> : mine.length === 0
          ? <Muted center style={{ paddingVertical: space.bar }}>{GIVE.empty}</Muted>
          : <Grid columns={page.columns}>{mine.map((l) => (
              <ListingCard key={l.id} testID={"mine-" + l.id} photoPath={l.photo_url} title={l.title} line={servings(l.number_of_serving)}
                time={timeText(l, RECEIVE, clockNow)} timeName={RECEIVE.timeLeft} gone={isGone(l, clockNow)} tall={false} />
            ))}</Grid>}</ScrollView>
      {page.dialogs ? null : <View style={[lane, { paddingVertical: space.item }]}>{add}</View>}
      {capturing ? <NewListing onClose={() => { setCapturing(false); reload(); }}
        onPosted={(n) => { setCapturing(false); say(postedText(n)); reload(); }} /> : null}
    </View>
  );
}
