// The photo touch-up of one item on the review, written new for V3RDE (NEW_WORK_UI.md, UI 16;
// mobile/docs/style_guide.md "Touch-up"). One pill starts a job: "Touch up" (X48r: one touch-up, the auto one; X49: a ChipBtn,
// with the Details pill of the review next to it in every state). While it
// runs a line says so. When the edit passed the check, the original and the edit show side by side; the
// original is chosen until the user taps the edit. The state lives in the review (it decides the photo to post).
import { ActivityIndicator, Pressable, Text, View } from "react-native";
import { touchup } from "../api";
import { itemPhotoPath } from "../logic";
import type { TouchState } from "../touchup";
import { opacity, radius, size, space, type, useColors } from "../theme";
import { ChipBtn, ErrorText, Muted, Photo, Row, usePhoto } from "../ui";
import { TOUCH } from "../words";

function Choice({ uri, label, name, on, onPress, testID }: {
  uri: string; label: string; name: string; on: boolean; onPress?: () => void; testID: string;
}) {
  const col = useColors();
  return (
    <Pressable testID={testID} accessibilityRole="radio" aria-checked={on} accessibilityState={{ checked: on }} accessibilityLabel={name}
      onPress={onPress} style={({ pressed }) => ({ alignItems: "center", gap: space.hair, opacity: pressed ? opacity.pressed : 1 })}>
      <View style={{ borderRadius: radius.photo, borderWidth: size.focusBorder, borderColor: on ? col.accent : col.card }}>
        <Photo uri={uri || undefined} side={size.choiceThumb} round="photo" /></View>
      <Text style={[type.body, { color: on ? col.fg : col.muted }]}>{label}</Text></Pressable>
  );
}

/** The pill row of an item on the review: Touch up (when it can start) and `side` (X49: the Details pill). */
export function PillRow({ children }: { children: React.ReactNode }) {
  return <Row style={{ flexWrap: "wrap", columnGap: space.gap, rowGap: space.hair }}>{children}</Row>;
}

export function TouchUp({ n, photoId, state, locked, onStart, onChoose, side }: {
  n: number; photoId: string; state: TouchState; locked: boolean;
  onStart: () => void; onChoose: (c: "original" | "edit") => void;
  /** X49: the Details pill of the item, next to Touch up (it shows in every state). */
  side?: React.ReactNode;
}) {
  const of = " " + TOUCH.ofItem(n); // the item number in every name: a list has several of these
  const original = usePhoto(itemPhotoPath(photoId) || undefined);
  const edit = usePhoto(state.kind === "offer" ? touchup.photoPath(state.id) : undefined);
  const col = useColors();
  if (state.kind === "working") {
    return (
      <View style={{ gap: space.hair }}>
        <View accessibilityLiveRegion="polite" aria-live="polite">
          <Row tight style={{ marginTop: space.hair }}><ActivityIndicator color={col.accent} /><Muted>{TOUCH.working}</Muted></Row></View>
        {side ? <PillRow>{side}</PillRow> : null}
      </View>
    );
  }
  if (state.kind === "offer") {
    return (
      <View style={{ gap: space.hair }}>
        <View accessibilityRole="radiogroup" aria-label={TOUCH.choose + of} accessibilityLiveRegion="polite" aria-live="polite"
          style={{ marginTop: space.hair, gap: space.hair }}>
          <Muted>{TOUCH.choose}</Muted>
          <Row>
            <Choice testID={"original-" + n} uri={original} label={TOUCH.original} name={TOUCH.original + of} on={state.chosen === "original"}
              onPress={locked ? undefined : () => onChoose("original")} />
            <Choice testID={"edit-" + n} uri={edit} label={TOUCH.edit} name={TOUCH.edit + of} on={state.chosen === "edit"}
              onPress={locked ? undefined : () => onChoose("edit")} />
          </Row>
        </View>
        {side ? <PillRow>{side}</PillRow> : null}
      </View>
    );
  }
  const line = state.kind === "kept" ? <Muted>{TOUCH.kept}</Muted>
    : state.kind === "failed" ? <ErrorText>{TOUCH.failed}</ErrorText>
    : state.kind === "error" ? <ErrorText>{state.text}</ErrorText> : null;
  return (
    <View style={{ marginTop: space.hair, gap: space.hair }}>
      <View accessibilityLiveRegion="polite" aria-live="polite">{line}</View>
      {locked && !side ? null : (
        <PillRow>
          {locked ? null : <ChipBtn testID={"touchup-" + n} glyph="sparkle" title={TOUCH.short} name={TOUCH.start + of} onPress={onStart} />}
          {side}
        </PillRow>
      )}
    </View>
  );
}
