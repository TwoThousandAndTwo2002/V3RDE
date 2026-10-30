// Give, written new for V3RDE (NEW_WORK_UI.md, UI 13; mobile/docs/style_guide.md "Give").
// "Your listings": the listings of the giver, photo first, in 1, 2 or 3 columns, with the live time pill.
// "+ New listing" opens the new-listing flow (Capture); it opens by itself when nothing of the giver is up.
// Phone: the pill is at the bottom. Wide screen: it is next to the title.
// X48t: a listing nobody has grabbed has "Delete" (it asks first, in a small popup); a grabbed one says why not.
import { useCallback, useEffect, useRef, useState } from "react";
import { ScrollView, View, useWindowDimensions } from "react-native";
import { ApiError, listings, type Listing } from "../api";
import { cameraFirst, canDelete, isGone, timeText } from "../listing";
import { layoutFor, space } from "../theme";
import { Grid, ListingCard, Muted, Pill, Popup, Row, ScreenHead, TextLink, Title, useNow } from "../ui";
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
  const [asking, setAsking] = useState<Listing | null>(null); // X48t: the listing to delete, while the popup asks
  const [deleting, setDeleting] = useState(false);
  const deletingNow = useRef(false); // X48t review: a fast double tap sends one delete
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
  const remove = async (l: Listing) => {
    if (deletingNow.current) return;
    deletingNow.current = true;
    setDeleting(true);
    try {
      await listings.remove(l.id);
      setMine((all) => (all ?? []).filter((x) => x.id !== l.id));
      say(GIVE.removed);
    } catch (err) {
      if (err instanceof ApiError && err.status === 401) return ended();
      const gone = err instanceof ApiError && err.status === 404; // already deleted (another device): it is gone
      say(gone ? GIVE.removed : err instanceof ApiError && err.status === 409 ? GIVE.grabbed : GIVE.removeFailed);
      reload();
    } finally {
      deletingNow.current = false;
      setDeleting(false);
      setAsking(null);
    }
  };
  const close = () => { if (!deletingNow.current) setAsking(null); }; // X48t review: the popup stays while it deletes
  const extra = (l: Listing) => (!canDelete(l) ? <Muted>{GIVE.grabbed}</Muted>
    : <TextLink testID={"delete-" + l.id} name={GIVE.removeName(l.title)} onPress={() => setAsking(l)}>{GIVE.remove}</TextLink>);
  return (
    <View style={{ flex: 1, minHeight: 0 }}>
      <Wallpaper />
      <ScrollView contentContainerStyle={[lane, { paddingTop: page.topBar ? space.pad : 0, paddingBottom: space.pad }]}>
        <ScreenHead title={GIVE.title} action={page.dialogs ? add : null} />
        {mine === null ? <Muted>{GIVE.loading}</Muted> : mine.length === 0
          ? <Muted center style={{ paddingVertical: space.bar }}>{GIVE.empty}</Muted>
          : <Grid columns={page.columns}>{mine.map((l) => (
              <ListingCard key={l.id} testID={"mine-" + l.id} photoPath={l.photo_url} title={l.title} line={servings(l.number_of_serving)}
                time={timeText(l, RECEIVE, clockNow)} timeName={RECEIVE.timeLeft} gone={isGone(l, clockNow)} tall={false} extra={extra(l)} />
            ))}</Grid>}</ScrollView>
      {page.dialogs ? null : <View style={[lane, { paddingVertical: space.item }]}>{add}</View>}
      {asking ? (
        <Popup testID="delete-confirm" small onDismiss={close} dismissName={GIVE.keep}>
          <Title style={{ marginBottom: space.hair }}>{GIVE.confirm(asking.title)}</Title>
          <Muted>{GIVE.confirmBody}</Muted>
          <Row style={{ marginTop: space.gutter }}>
            <Pill testID="delete-keep" kind="secondary" title={GIVE.keep} grow onPress={deleting ? undefined : close} />
            <Pill testID="delete-yes" title={GIVE.remove} grow busy={deleting} onPress={() => remove(asking)} />
          </Row>
        </Popup>
      ) : null}
      {capturing ? <NewListing onClose={() => { setCapturing(false); reload(); }}
        onPosted={(n) => { setCapturing(false); say(postedText(n)); reload(); }} /> : null}
    </View>
  );
}
