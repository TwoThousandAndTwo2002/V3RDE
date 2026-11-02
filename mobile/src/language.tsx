// X49x (T9 Nov 1: the language choice on the sign-in screen and in the menu): the chosen language for every
// screen, the small "globe + language name" button, and the sheet with the list of languages. The words and the
// files are in src/lang.ts.
import React, { createContext, useContext } from "react";
import { Pressable, Text, View } from "react-native";
import { SOURCE, shortName, type Language } from "./lang";
import { opacity, radius, size, space, type, useColors } from "./theme";
import { GlyphIcon, Muted, Popup, Rise, Title } from "./ui";
import { LANG, ROLE } from "./words";

export type LanguageChoice = { languages: Language[]; now: Language; open: () => void };
export const LanguageContext = createContext<LanguageChoice>({ languages: [SOURCE], now: SOURCE, open: () => undefined });
export const useLanguage = () => useContext(LanguageContext);

const isHovered = (state: object) => (state as { hovered?: boolean }).hovered === true;

/** The button that opens the language sheet: the globe and the name of the language now, in that language. Not
 *  shown when the box has only English. */
export function LanguageButton({ testID = "language", look = "link" }: {
  testID?: string;
  /** X65 (T9 Nov 2: "the same oval style as the others"): "link" on the sign-in row; "pill" in the role sheet (as its Log
   *  out pill: `Pill` secondary, the width of the sheet); "bar" in the wide top bar (as its Log out oval). */
  look?: "link" | "pill" | "bar";
}) {
  const col = useColors();
  const { languages, now, open } = useLanguage();
  if (languages.length < 2) return null;
  const oval = look !== "link";
  const fg = look === "link" ? col.link : look === "bar" ? col.muted : col.fg;
  return (
    <Pressable testID={testID} accessibilityRole="button" accessibilityLabel={LANG.name(now.native)} onPress={open}
      style={(state) => ({ flexDirection: "row", alignItems: "center", gap: space.gap,
        ...(oval ? { flexShrink: 1, minWidth: 0, height: size.control, borderRadius: radius.round, borderWidth: size.border, borderColor: col.line, justifyContent: "center",
          paddingHorizontal: look === "pill" ? size.pillPadX : size.tabPadX,
          backgroundColor: look === "pill" ? col.card : isHovered(state) ? col.iconFill : col.bg } : {}),
        opacity: state.pressed ? opacity.pressed : oval && look === "pill" && isHovered(state) ? opacity.hover : 1 })}>
      <GlyphIcon name="globe" color={look === "link" ? col.link : fg} />
      <Text numberOfLines={1} style={[type.button, { color: fg, flexShrink: 1 }]}>{shortName(now.native)}</Text></Pressable>
  );
}

function LanguageRow({ l, chosen, onPress }: { l: Language; chosen: boolean; onPress: () => void }) {
  const col = useColors();
  return (
    <Pressable testID={"language-" + l.code} accessibilityRole="button" accessibilityState={{ selected: chosen }}
      accessibilityLabel={l.native + (l.name !== l.native ? ", " + l.name : "") + (chosen ? ". " + ROLE.selected : "")} onPress={onPress}
      style={(state) => ({ flexDirection: "row", alignItems: "center", gap: space.group, minHeight: size.control, paddingHorizontal: space.group,
        borderRadius: radius.card, borderWidth: size.border, borderColor: chosen || isHovered(state) ? col.accent : col.line,
        backgroundColor: col.card, opacity: state.pressed ? opacity.pressed : 1 })}>
      <Text {...({ lang: l.code } as object)} style={[type.button, { color: col.fg, flexShrink: 1 }]}>{l.native}</Text>
      <View style={{ flex: 1 }} />
      {l.name !== l.native ? <Muted>{l.name}</Muted> : null}</Pressable>
  );
}

/** The list of languages, each in its own name (the English name under it). A tap changes every screen at once. */
export function LanguageSheet({ onPick, onDismiss }: { onPick: (code: string) => void; onDismiss: () => void }) {
  const { languages, now } = useLanguage();
  return (
    <Popup testID="language-sheet" onDismiss={onDismiss} dismissName={LANG.close}>
      <Rise i={0}><Title>{LANG.title}</Title></Rise>
      <Rise i={1}><View style={{ marginTop: space.gutter, gap: space.item }}>
        {languages.map((l) => <LanguageRow key={l.code} l={l} chosen={l.code === now.code} onPress={() => onPick(l.code)} />)}
      </View></Rise>
    </Popup>
  );
}
