// X49 (T9, 2026-10-30): the details of one item on the review (what the scan found), written new for V3RDE
// (mobile/docs/style_guide.md "Review"; rules in src/details.ts). Phone: in a small Popup with the pills Change and
// Close. Wide screen: in the third column of the review, with a Change ChipBtn. Change opens the choices and the
// weight field; Done gives the new details to the review (it records the edit). The cost is never shown.
import { useState } from "react";
import { View } from "react-native";
import { weightInput, weightProblem } from "../capture";
import { applyDraft, detailLines, detailsPillShows, draftOf, type DetailDraft } from "../details";
import type { ItemDetails, ItemForm } from "../logic";
import { motion, space } from "../theme";
import { ChipBtn, ErrorText, Field, Muted, Pill, Row, Title, TypedText } from "../ui";
import { DETAILS } from "../words";

/** True when the item has a Details pill: it has details now, or the scanner gave it details (`scanned`). */
export function hasDetails(it: ItemForm, scanned?: ItemForm): boolean {
  return detailsPillShows(it.details, scanned?.details, DETAILS);
}

/** One row of choice chips under its label; a tap on the chosen chip clears it. */
function Choices<T extends string | number>({ label, options, value, onChange }: {
  label: string; options: [string, T][]; value: T | undefined; onChange: (v: T | undefined) => void;
}) {
  return (
    <View accessibilityRole="radiogroup" aria-label={label} style={{ gap: space.hair }}>
      <Muted style={{ marginLeft: space.inset }}>{label}</Muted>
      <Row tight style={{ flexWrap: "wrap", rowGap: space.inset }}>
        {options.map(([t, v]) => <ChipBtn key={t} title={t} name={t} on={value === v} onPress={() => onChange(value === v ? undefined : v)} />)}
      </Row>
    </View>
  );
}

function EditDetails({ d, locked, onCancel, onDone }: {
  d: ItemDetails | undefined; locked: boolean; onCancel: () => void; onDone: (d: ItemDetails | undefined) => void;
}) {
  const [x, setX] = useState<DetailDraft>(() => draftOf(d));
  const [problem, setProblem] = useState("");
  const set = (p: Partial<DetailDraft>) => setX((old) => ({ ...old, ...p }));
  const done = () => {
    if (locked) return; // X49 review: not while the listings post
    const wrong = weightProblem(x.weight);
    if (wrong) return setProblem(wrong);
    onDone(applyDraft(d, x));
  };
  return (
    <View style={{ gap: space.gap }}>
      <Choices label={DETAILS.condition} value={x.condition} onChange={(v) => set({ condition: v })}
        options={[[DETAILS.ready, "ready_to_eat" as const], [DETAILS.cooking, "cooking_required" as const], [DETAILS.sealed, "sealed" as const]]} />
      <Choices label={DETAILS.food} value={x.diet} onChange={(v) => set({ diet: v })}
        options={[[DETAILS.vegChoice, 0 as const], [DETAILS.meatChoice, 1 as const]]} />
      <Choices label={DETAILS.portions} value={x.portions} onChange={(v) => set({ portions: v })}
        options={[[DETAILS.bulkChoice, "bulk" as const], [DETAILS.singleChoice, "individual" as const]]} />
      <Field testID="details-weight" label={DETAILS.weight} value={x.weight} keyboardType="decimal-pad" inputMode="decimal" returnKeyType="done" onSubmitEditing={done}
        onChangeText={(t) => { set({ weight: weightInput(t) }); setProblem(""); }} />
      {problem ? <ErrorText>{problem}</ErrorText> : null}
      <Row style={{ marginTop: space.gap }}>
        <Pill testID="details-cancel" kind="secondary" title={DETAILS.cancel} grow onPress={onCancel} />
        <Pill testID="details-done" title={DETAILS.done} grow onPress={locked ? undefined : done} />
      </Row>
    </View>
  );
}

/**
 * The details of one item: its title, then the details typing in (or the Change form). `onClose` (phone): the pills
 * Change and Close; without it (the column of a wide screen): a Change ChipBtn with the pencil.
 */
export function DetailsView({ it, editing, locked, onEdit, onCancel, onDone, onClose }: {
  it: ItemForm; editing: boolean; onEdit: () => void; onCancel: () => void; onDone: (d: ItemDetails | undefined) => void; onClose?: () => void;
  /** X49 review: the listings post: no Change and no Done. */
  locked: boolean;
}) {
  const lines = detailLines(it.details, DETAILS);
  return (
    <View testID="details" style={{ gap: space.gap }}>
      <Title>{it.title.trim() || DETAILS.pill}</Title>
      {editing ? <EditDetails d={it.details} locked={locked} onCancel={onCancel} onDone={onDone} /> : (
        <>
          {lines.length > 0 ? <TypedText lines={lines} delay={motion.growMs} /> : <Muted>{DETAILS.none}</Muted>}
          {onClose ? (
            <Row style={{ marginTop: space.gap }}>
              <Pill testID="details-change" kind="secondary" title={DETAILS.change} name={DETAILS.changeName} grow onPress={locked ? undefined : onEdit} />
              <Pill testID="details-close" title={DETAILS.close} name={DETAILS.closeName} grow onPress={onClose} />
            </Row>
          ) : locked ? null : (
            <Row style={{ justifyContent: "flex-end" }}>
              <ChipBtn testID="details-change" glyph="pencil" title={DETAILS.change} name={DETAILS.changeName} onPress={onEdit} />
            </Row>
          )}
        </>
      )}
    </View>
  );
}
