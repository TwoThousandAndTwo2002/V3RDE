// Give, written new for V3RDE (NEW_WORK_UI.md, UI 13; mobile/docs/style_guide.md "Give").
// "Your listings": the listings of the giver, photo first, in 1, 2 or 3 columns, with the live time pill.
// The new-listing flow (camera, scan, review, post) comes in the next changes.
import { useCallback, useEffect, useState } from "react";
import { ScrollView, View, useWindowDimensions } from "react-native";
import { ApiError, listings, type Listing } from "../api";
import { isGone, timeText } from "../listing";
import { layoutFor, space } from "../theme";
import { Grid, ListingCard, Muted, Title, useNow } from "../ui";
import { Doodles as Wallpaper } from "../doodles";
import { useSession } from "../session";
import { GIVE, RECEIVE, servings } from "../words";

export default function GiveScreen() {
  const { say, ended } = useSession();
  const page = layoutFor(useWindowDimensions().width);
  const clockNow = useNow(1000);
  const [mine, setMine] = useState<Listing[] | null>(null); // null until the first answer
  const reload = useCallback(async () => {
    try {
      setMine((await listings.mine()).items);
    } catch (err) {
      if (err instanceof ApiError && err.status === 401) return ended();
      say(err instanceof Error ? err.message : String(err));
      setMine([]);
    }
  }, [ended, say]);
  useEffect(() => { reload(); }, [reload]);
  const side = page.topBar ? space.pad : space.gutter;
  return (
    <View style={{ flex: 1, minHeight: 0 }}>
      <Wallpaper />
      <ScrollView contentContainerStyle={{ width: "100%", maxWidth: page.maxWidth, alignSelf: "center", paddingHorizontal: side,
        paddingTop: page.topBar ? space.pad : 0, paddingBottom: space.pad }}>
        <View style={{ marginBottom: space.gutter }}><Title>{GIVE.title}</Title></View>
        {mine === null ? <Muted>{GIVE.loading}</Muted> : mine.length === 0
          ? <Muted center style={{ paddingVertical: space.bar }}>{GIVE.empty}</Muted>
          : <Grid columns={page.columns}>{mine.map((l) => (
              <ListingCard key={l.id} testID={"mine-" + l.id} photoPath={l.photo_url} title={l.title} line={servings(l.number_of_serving)}
                time={timeText(l, RECEIVE, clockNow)} timeName={RECEIVE.timeLeft} gone={isGone(l, clockNow)} tall={false} />
            ))}</Grid>}</ScrollView>
    </View>
  );
}
