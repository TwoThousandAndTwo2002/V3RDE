// Give, written new for V3RDE (NEW_WORK_UI.md, UI 13; mobile/docs/style_guide.md "Give").
// "Your listings": the listings of the giver, photo first, in 1, 2 or 3 columns, with the live time pill.
// "+ New listing" opens the new-listing flow (Capture); it opens by itself when nothing of the giver is up.
// Phone: the pill is at the bottom. Wide screen: it is next to the title.
// X48t: a listing nobody has grabbed has "Delete" (it asks first, in a small popup); a grabbed one says why not.
// R5: an expired listing nobody grabbed also has the red "Rescue" pill: the rescue popup (Rescue.tsx).
import { useCallback, useEffect, useRef, useState } from "react";
import { ScrollView, View, useWindowDimensions } from "react-native";
import { ApiError, listings, type Listing } from "../api";
import { cameraFirst, canDelete, isGone, listingChange, timeText } from "../listing";
import { layoutFor, space } from "../theme";
import { ChipBtn, Field, FrostBtn, Grid, IconBtn, ListingCard, Muted, Pill, Popup, Row, ScreenHead, SmallPill, Title, useNow } from "../ui";
import { LIMITS, servingsInput, stepServings } from "../capture";
import NewListing from "./Capture";
import RescueScreen from "./Rescue";
import { canRescue } from "../rescue";
import { Doodles as Wallpaper } from "../doodles";
import { useSession } from "../session";
import { GIVE, RECEIVE, RESCUE, REVIEW, postedText, servings } from "../words";

export default function GiveScreen() {
  const { say, ended } = useSession();
  const page = layoutFor(useWindowDimensions().width);
  const clockNow = useNow(1000);
  const [mine, setMine] = useState<Listing[] | null>(null); // null until the first answer
  const [capturing, setCapturing] = useState(false); // the new-listing flow is open
  const [asking, setAsking] = useState<Listing | null>(null); // X48t: the listing to delete, while the popup asks
  const [deleting, setDeleting] = useState(false);
  const [rescuing, setRescuing] = useState<Listing | null>(null); // R5: the listing of the open rescue popup
  const [editing, setEditing] = useState<Listing | null>(null); // X60: the listing that the popup changes
  const [count, setCount] = useState(""); // X60: the servings in the popup (text, as on the review)
  const [name, setName] = useState(""); // X60: the title in the popup
  const [reset, setReset] = useState(false); // X60: reset the timer to 30 minutes from now (always on an ended listing)
  const [saving, setSaving] = useState(false);
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
  // X49h (T9): Delete is an X in a frosted round button on the photo; Rescue is a pill the size of the time pill
  // X60 (T9): nobody has grabbed it: a pencil next to the X changes the name, the servings and the timer;
  // on an ended listing the same popup posts it again (the timer reset is always on)
  const open = (l: Listing) => { setName(l.title); setCount(String(l.number_of_serving)); setReset(isGone(l, clockNow)); setEditing(l); };
  const del = (l: Listing) => (canDelete(l) ? (
    <Row tight>
      <FrostBtn testID={"edit-" + l.id} glyph="pencil" label={GIVE.editName(l.title)} onPress={() => open(l)} />
      <FrostBtn testID={"delete-" + l.id} glyph="close" label={GIVE.removeName(l.title)} onPress={() => setAsking(l)} />
    </Row>) : null);
  const savingNow = useRef(false); // a fast double tap sends one change
  const save = async (l: Listing) => {
    if (savingNow.current) return;
    const asked = listingChange(l, name, count, reset, Date.now());
    if (asked === null) return setEditing(null); // nothing changed
    if ("problem" in asked) return say(asked.problem);
    const again = isGone(l, Date.now());
    savingNow.current = true;
    setSaving(true);
    try {
      const got = await listings.update(l.id, asked.change);
      setMine((all) => (all ?? []).map((x) => (x.id === l.id ? got : x)));
      setEditing(null);
      say(again ? GIVE.postedAgain : GIVE.saved);
    } catch (err) {
      if (err instanceof ApiError && err.status === 401) return ended();
      // X60 review: a 409 is a grab, or the timer ended on the way (the service says "listing expired")
      const conflict = err instanceof ApiError && err.status === 409;
      const ended409 = conflict && /expired/.test(String((err as ApiError).body?.error ?? ""));
      say(ended409 ? GIVE.changeEnded : conflict ? GIVE.changeGrabbed
        : err instanceof ApiError && err.status === 400 ? err.message : GIVE.saveFailed);
      if (err instanceof ApiError && (err.status === 409 || err.status === 404)) { setEditing(null); reload(); }
    } finally {
      savingNow.current = false;
      setSaving(false);
    }
  };
  const extra = (l: Listing) => (!canDelete(l) ? <Muted>{GIVE.grabbed}</Muted> : !canRescue(l, clockNow) ? null : (
    <Row tight style={{ justifyContent: "flex-end" }}>
      <SmallPill testID={"rescue-" + l.id} name={RESCUE.pillName(l.title)} title={RESCUE.pill} onPress={() => setRescuing(l)} />
    </Row>));
  return (
    <View style={{ flex: 1, minHeight: 0 }}>
      <Wallpaper />
      <ScrollView contentContainerStyle={[lane, { paddingTop: page.topBar ? space.pad : 0, paddingBottom: space.pad }]}>
        <ScreenHead title={GIVE.title} action={page.dialogs ? add : null} />
        {mine === null ? <Muted>{GIVE.loading}</Muted> : mine.length === 0
          ? <Muted center style={{ paddingVertical: space.bar }}>{GIVE.empty}</Muted>
          : <Grid columns={page.columns}>{mine.map((l) => (
              <ListingCard key={l.id} testID={"mine-" + l.id} photoPath={l.photo_url} title={l.title} line={servings(l.number_of_serving)}
                time={timeText(l, RECEIVE, clockNow)} timeName={RECEIVE.timeLeft} gone={isGone(l, clockNow)} tall={false} extra={extra(l)} corner={del(l)} />
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
      {editing ? (() => {
        const again = isGone(editing, clockNow);
        return (
        <Popup testID="listing-edit" small onDismiss={() => { if (!saving) setEditing(null); }} dismissName={GIVE.keep}>
          <Title style={{ marginBottom: space.hair }}>{again ? GIVE.againTitle : GIVE.editTitle}</Title>
          <Muted>{again ? GIVE.againBody : GIVE.editBody}</Muted>
          <View style={{ marginTop: space.gutter }}>
            <Field testID="edit-title" label={REVIEW.title} value={name} maxLength={LIMITS.title} onChangeText={setName} /></View>
          <Muted style={{ marginTop: space.group, marginStart: space.inset, marginBottom: space.hair }}>{REVIEW.servings}</Muted>
          <Row>
            <IconBtn big glyph="minus" label={REVIEW.fewer} onPress={() => setCount((c) => stepServings(c, -1))} />
            <View style={{ flex: 1, minWidth: 0 }}>
              <Field testID="edit-servings" accessibilityLabel={REVIEW.servings} value={count} center keyboardType="number-pad"
                inputMode="numeric" onChangeText={(t) => setCount(servingsInput(t))} /></View>
            <IconBtn big glyph="plus" label={REVIEW.more} onPress={() => setCount((c) => stepServings(c, 1))} />
          </Row>
          {again ? null : (
            <Row style={{ marginTop: space.group }}>
              <ChipBtn testID="edit-reset" title={GIVE.timerReset} name={GIVE.timerReset} on={reset} onPress={() => setReset((r) => !r)} />
            </Row>)}
          <Row style={{ marginTop: space.gutter }}>
            <Pill testID="edit-keep" kind="secondary" title={GIVE.keep} grow onPress={saving ? undefined : () => setEditing(null)} />
            <Pill testID="edit-save" title={again ? GIVE.postAgain : GIVE.save} grow busy={saving} onPress={() => save(editing)} />
          </Row>
        </Popup>);
      })() : null}
      {rescuing ? <RescueScreen listing={rescuing} onClose={() => setRescuing(null)} /> : null}
      {capturing ? <NewListing onClose={() => { setCapturing(false); reload(); }}
        onPosted={(n) => { setCapturing(false); say(postedText(n)); reload(); }} /> : null}
    </View>
  );
}
