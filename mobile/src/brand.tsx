// The V3RDE logo (LG1, written new for V3RDE in the Hackathon; T9 Nov 1: the mark T1 "V³" and the wordmark G; the
// approved mockup https://claude.ai/artifact/1B1TJxdtLPX6h1Qig13z1u; mobile/docs/style_guide.md "Logo").
// Every size comes from the theme: the mark is as high as the line of the text it stands next to.
import { Text, View } from "react-native";
import Svg, { G, Path, Polygon } from "react-native-svg";
import { logo, space, type, useColors } from "./theme";
import { FRAME } from "./words";

/** A text style of the theme (type.title, type.display): the mark is as high as its line. */
type Line = (typeof type)[keyof typeof type];

/** The mark: three V's, back to front (accentSoft, accent, fg). Hidden from screen readers (its row has the name). */
export function Mark({ h }: { h: number }) {
  const col = useColors();
  return (
    <Svg width={(h * logo.viewW) / logo.viewH} height={h} viewBox={`0 0 ${logo.viewW} ${logo.viewH}`}
      accessibilityElementsHidden importantForAccessibility="no-hide-descendants" aria-hidden>
      <G transform={`translate(${logo.depth2}, ${logo.depth2})`}><Polygon points={logo.v} fill={col.accentSoft} /></G>
      <G transform={`translate(${logo.depth1}, ${logo.depth1})`}><Polygon points={logo.v} fill={col.accent} /></G>
      <Polygon points={logo.v} fill={col.fg} />
    </Svg>
  );
}

/** The leaf full stop: its bottom on the baseline of the line (logo.baselineEm). */
function Leaf({ t }: { t: Line }) {
  const col = useColors();
  const s = t.fontSize * logo.leafEm;
  return (
    <Svg width={s} height={s} viewBox={`0 0 ${logo.leafBox} ${logo.leafBox}`} aria-hidden
      style={{ marginBottom: t.lineHeight / 2 - logo.baselineEm * t.fontSize, marginLeft: t.fontSize * logo.leafGapEm }}>
      <Path d={logo.leaf} fill={col.accent} />
    </Svg>
  );
}

/** The wordmark: "V3RDE" in fg and the accent leaf. */
export function Word({ t }: { t: Line }) {
  const col = useColors();
  return (
    <View style={{ flexDirection: "row", alignItems: "flex-end" }} aria-hidden>
      <Text style={[t, { color: col.fg }]}>{FRAME.brand}</Text>
      <Leaf t={t} />
    </View>
  );
}

/** A row of the mark (as high as the line) and the wordmark, with one accessible name. */
function Row({ t, testID }: { t: Line; testID: string }) {
  return (
    <View testID={testID} accessibilityRole="header" aria-label={FRAME.brand}
      style={{ flexDirection: "row", alignItems: "center", gap: space.gap }}>
      <Mark h={t.lineHeight} />
      <Word t={t} />
    </View>
  );
}

/** The wide top bar (approved b): the mark and the wordmark in type.title, at the left. */
export function TopBrand() {
  return <Row t={type.title} testID="brand-top" />;
}

/** The phone and tablet header (approved c): the mark only, as high as a title line, in the centre. */
export function PhoneBrand() {
  return <View testID="brand-phone" accessibilityRole="image" aria-label={FRAME.brand}><Mark h={type.title.lineHeight} /></View>;
}

/** Above the title of the sign-in and reset screens (approved b): the mark and the wordmark in type.display. */
export function SignInBrand() {
  return <View style={{ marginBottom: space.pad, alignItems: "flex-start" }}><Row t={type.display} testID="brand-signin" /></View>;
}
