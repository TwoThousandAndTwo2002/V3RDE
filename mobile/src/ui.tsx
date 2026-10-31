// V3RDE shared UI parts, written new for V3RDE (NEW_WORK_UI.md, UI 5; mobile/docs/style_guide.md).
// Every colour, size and font comes from theme.ts (tests/style.test.mts checks it).
import React, { useEffect, useRef as useBox, useState } from "react";
import * as RN from "react-native";
import { ActivityIndicator, Animated, Image, Pressable, ScrollView, StyleSheet, Text, TextInput, View, useWindowDimensions } from "react-native";
import type { StyleProp, TextInputProps, TextStyle, ViewStyle } from "react-native";
import { BlurView } from "expo-blur";
import { LinearGradient as ShadeGradient } from "expo-linear-gradient";
import Svg, { Path } from "react-native-svg";
import { useSafeAreaInsets as useInsets } from "react-native-safe-area-context";
import { photoUrl as photoUri } from "./api";
import { LIMITS } from "./capture";
import { camera, glyph, layoutFor, motion, opacity, radius, shade, size, space, type, useColors } from "./theme";
import { typeDelay } from "./details";

// ---------------------------------------------------------------- text

export function Title({ children, center, style }: { children: React.ReactNode; center?: boolean; style?: StyleProp<TextStyle> }) {
  const col = useColors();
  return (
    <Text accessibilityRole="header" style={[type.title, { color: col.fg, textAlign: center ? "center" : "left" }, style]}>
      {children}
    </Text>
  );
}

export function Body({ children, style, lines }: { children: React.ReactNode; style?: StyleProp<TextStyle>; lines?: number }) {
  const col = useColors();
  return <Text numberOfLines={lines} style={[type.body, { color: col.fg }, style]}>{children}</Text>;
}

export function Muted({ children, style, lines, center }: {
  children: React.ReactNode; style?: StyleProp<TextStyle>; lines?: number; center?: boolean;
}) {
  const col = useColors();
  return (
    <Text numberOfLines={lines} style={[type.body, { color: col.muted, textAlign: center ? "center" : "left" }, style]}>
      {children}
    </Text>
  );
}

export function ErrorText({ children, center, lines, style }: {
  children: React.ReactNode; center?: boolean; lines?: number; style?: StyleProp<TextStyle>;
}) {
  const col = useColors();
  return (
    <Text accessibilityRole="alert" numberOfLines={lines}
      style={[type.body, { color: col.danger, textAlign: center ? "center" : "left", marginTop: space.gap }, style]}>
      {children}
    </Text>
  );
}

// ---------------------------------------------------------------- icons and buttons

export type Glyph = "close" | "minus" | "plus" | "upload" | "give" | "receive" | "menu" | "mic" | "stop" | "sparkle" | "info" | "pencil";

/** The square box of each glyph (pt). */
const GLYPH_BOX: Record<Glyph, number> = {
  close: glyph.box, minus: glyph.box, plus: glyph.box, upload: glyph.uploadBox, give: glyph.iconBox, receive: glyph.iconBox, menu: glyph.iconBox,
  mic: glyph.iconBox, stop: glyph.iconBox, sparkle: glyph.iconBox, info: glyph.iconBox, pencil: glyph.iconBox,
};

/** A line glyph (close ✕, minus −, plus +, upload, give, receive, menu ☰): one SVG path with round ends, in its own square box. */
export function GlyphIcon({ name, color }: { name: Glyph; color: string }) {
  const box = GLYPH_BOX[name];
  return (
    <Svg width={box} height={box} viewBox={`0 0 ${box} ${box}`} accessible={false}>
      <Path d={glyph[name]} stroke={color} strokeWidth={glyph.stroke} strokeLinecap="round" strokeLinejoin="round" fill="none" />
    </Svg>
  );
}

/** A round icon button: 38 pt inside a review card, 44 pt (the touch size) elsewhere. */
export function IconBtn({ glyph, label, onPress, big, testID }: { glyph: Glyph; label: string; onPress: () => void; big?: boolean; testID?: string }) {
  const col = useColors();
  const d = big ? size.iconBig : size.iconSmall;
  return (
    <Pressable testID={testID} accessibilityRole="button" accessibilityLabel={label} onPress={onPress} hitSlop={big ? 0 : (size.iconBig - d) / 2}
      style={({ pressed }) => ({ width: d, height: d, borderRadius: radius.round, alignItems: "center", justifyContent: "center",
        backgroundColor: col.iconFill, opacity: pressed ? opacity.pressed : 1 })}>
      <GlyphIcon name={glyph} color={col.fg} /></Pressable>
  );
}

/** A pill button: primary (red) or secondary (white with a line). `grow` shares a row with another pill. */
export function Pill({ title, name, onPress, kind = "primary", busy, busyName, grow, testID }: {
  title: string; name?: string; onPress?: () => void; kind?: "primary" | "secondary"; busy?: boolean; busyName?: string; grow?: boolean;
  testID?: string;
}) {
  const col = useColors();
  const isPrimary = kind !== "secondary";
  const fg = isPrimary ? col.onAccent : col.fg;
  return (
    <Pressable testID={testID} accessibilityRole="button" accessibilityLabel={busy && busyName ? busyName : name ?? title} aria-busy={!!busy}
      onPress={busy ? undefined : onPress}
      style={(state) => [{
        height: size.control, borderRadius: radius.round, alignItems: "center", justifyContent: "center",
        paddingHorizontal: size.pillPadX, backgroundColor: isPrimary ? col.accent : col.card,
        borderWidth: size.border, borderColor: isPrimary ? col.accent : col.line,
        opacity: busy ? opacity.busy : state.pressed ? opacity.pressed : hovered(state) ? opacity.hover : 1,
      }, grow ? { flex: 1 } : null]}>
      {busy ? <ActivityIndicator color={fg} /> : <Text numberOfLines={1} style={[type.button, { color: fg }]}>{title}</Text>}</Pressable>
  );
}

/**
 * X49: a small pill (`size.iconSmall` high, on `iconFill`) with a glyph and a short word: Touch up, Details, Change.
 * `on` (the part it opens is open) = an `accent` border, and the word and glyph in `link` (`accent` in light mode).
 * With no glyph it is a choice chip (a radio for a screen reader): a `line` border when it is off.
 */
export function ChipBtn({ glyph: g, title, name, on, onPress, testID, wide }: {
  glyph?: Glyph; title?: string; name: string; on?: boolean; onPress: () => void; testID?: string;
  /** X49i: as wide as its row (More). */
  wide?: boolean;
}) {
  const col = useColors();
  const fg = on ? col.link : col.fg;
  const choice = !g;
  return (
    <Pressable testID={testID} accessibilityRole={choice ? "radio" : "button"} accessibilityLabel={name}
      accessibilityState={choice ? { checked: !!on } : { expanded: on }} aria-checked={choice ? !!on : undefined}
      aria-expanded={choice ? undefined : on} onPress={onPress} hitSlop={(size.iconBig - size.iconSmall) / 2}
      style={(state) => ({ height: size.iconSmall, minWidth: size.iconSmall, paddingHorizontal: title ? space.group : 0, borderRadius: radius.round,
        alignSelf: wide ? "stretch" : undefined,
        flexDirection: "row", alignItems: "center", justifyContent: "center", gap: space.inset, backgroundColor: col.iconFill,
        borderWidth: size.border, borderColor: on ? col.accent : choice ? col.line : col.iconFill,
        opacity: state.pressed ? opacity.pressed : hovered(state) ? opacity.hover : 1 })}>
      {g ? <GlyphIcon name={g} color={fg} /> : null}
      {title ? <Text numberOfLines={1} style={[type.button, { color: fg }]}>{title}</Text> : null}
    </Pressable>
  );
}

/** A row of controls with the standard gap (10 pt), or the gap inside a card (8 pt) with `tight`. */
export function Row({ children, style, tight }: { children: React.ReactNode; style?: StyleProp<ViewStyle>; tight?: boolean }) {
  return <View style={[{ flexDirection: "row", alignItems: "center", gap: tight ? space.gap : space.item }, style]}>{children}</View>;
}

// ---------------------------------------------------------------- camera

/** A dark round button on the live camera (close, upload), 38 pt with a 44 pt hit area. */
export function CameraBtn({ glyph, label, onPress, style }: { glyph: Glyph; label: string; onPress: () => void; style?: StyleProp<ViewStyle> }) {
  const col = useColors();
  return (
    <Pressable accessibilityRole="button" accessibilityLabel={label} onPress={onPress} hitSlop={(size.iconBig - size.iconSmall) / 2}
      style={({ pressed }) => [{ position: "absolute", width: size.iconSmall, height: size.iconSmall, borderRadius: radius.round,
        alignItems: "center", justifyContent: "center", backgroundColor: col.cameraBtn, opacity: pressed ? opacity.pressed : 1 }, style]}>
      <GlyphIcon name={glyph} color={col.cameraFg} /></Pressable>
  );
}

/** The shutter: a white ring around a white disc, centred at the bottom of the camera (or of the dialog photo). */
export function Shutter({ onPress, label = "Take photo", bottom = camera.shutterBottom }: { onPress: () => void; label?: string; bottom?: number }) {
  const col = useColors();
  const inner = camera.shutter - 2 * (camera.shutterRing + camera.shutterGap);
  return (
    <View pointerEvents="box-none" style={{ position: "absolute", left: 0, right: 0, bottom, alignItems: "center" }}>
      <Pressable onPress={onPress} accessibilityRole="button" accessibilityLabel={label}
        style={({ pressed }) => ({ width: camera.shutter, height: camera.shutter, borderRadius: radius.round, borderWidth: camera.shutterRing,
          borderColor: col.cameraFg, alignItems: "center", justifyContent: "center", opacity: pressed ? opacity.pressed : 1 })}>
        <View style={{ width: inner, height: inner, borderRadius: radius.round, backgroundColor: col.cameraFg }} /></Pressable>
    </View>
  );
}

/** A line of help on the live camera, in a dark pill above the shutter. */
export function Hint({ children, alert }: { children: React.ReactNode; alert?: boolean }) {
  const col = useColors();
  return (
    <View pointerEvents="none" style={{ position: "absolute", left: space.gutter, right: space.gutter, bottom: camera.hintBottom, alignItems: "center" }}>
      <Text accessibilityRole={alert ? "alert" : undefined} accessibilityLiveRegion={alert ? "assertive" : "none"}
        style={[type.body, { color: col.cameraFg, backgroundColor: col.cameraBtn, borderRadius: radius.round, overflow: "hidden",
        paddingHorizontal: camera.hintPadX, paddingVertical: camera.hintPadY, textAlign: "center" }]}>{children}</Text>
    </View>
  );
}

/** A progress bar: the accent over the soft accent, fully round. `share` is 0..1. */
export function Progress({ share }: { share: number }) {
  const col = useColors();
  const k = Math.max(0, Math.min(1, share));
  return (
    <View accessibilityRole="progressbar" accessibilityValue={{ min: 0, max: 100, now: Math.round(k * 100) }}
      aria-valuemin={0} aria-valuemax={100} aria-valuenow={Math.round(k * 100)} style={{ height: size.progressH, borderRadius: radius.round, backgroundColor: col.accentSoft, overflow: "hidden" }}>
      <View style={{ width: `${k * 100}%`, height: "100%", borderRadius: radius.round, backgroundColor: col.accent }} />
    </View>
  );
}

// ---------------------------------------------------------------- fields

/** True while the mouse is on a control: react-native-web adds `hovered` to the Pressable state (web only). */
function hovered(state: object): boolean {
  return (state as { hovered?: boolean }).hovered === true;
}

/**
 * No browser focus ring on a field (the field draws its own focus border). react-native-web passes the style to the
 * DOM; React Native has no "none" outline style, so the web style is given here only.
 */
const NO_RING = RN.Platform.OS === "web" ? ({ outlineStyle: "none" } as unknown as TextStyle) : null;

/**
 * A text field: a white pill with a line (48 pt), or a box with several lines (72 pt). While it has the focus,
 * its border is `size.focusBorder` wide in `accent`. `right` puts a text button (FieldButton) inside the field.
 * The label is also the accessible name of the input.
 */
export function Field({ label, multiline, center, right, style, onFocus, onBlur, ref, ...rest }:
  TextInputProps & { label?: string; center?: boolean; right?: React.ReactNode; ref?: React.Ref<React.ComponentRef<typeof TextInput>> }) {
  const col = useColors();
  const [focused, setFocused] = useState(false);
  const input = (
    <TextInput
      ref={ref}
      placeholderTextColor={col.placeholder}
      multiline={multiline}
      onFocus={(e) => { setFocused(true); onFocus?.(e); }}
      onBlur={(e) => { setFocused(false); onBlur?.(e); }}
      aria-label={label}
      {...rest}
      style={[type.body, {
        color: col.fg, backgroundColor: col.card,
        borderWidth: focused ? size.focusBorder : size.border, borderColor: focused ? col.accent : col.line,
        borderRadius: radius[multiline ? "card" : "round"], paddingLeft: size.fieldPadX,
        paddingRight: right ? size.fieldSlot : size.fieldPadX,
        height: multiline ? size.controlTall : size.control, textAlign: center ? "center" : "left",
        textAlignVertical: multiline ? "top" : "center", paddingTop: multiline ? size.fieldPadTop : 0,
      }, NO_RING, style]}
    />
  );
  const box = right ? (
    <View style={{ justifyContent: "center" }}>{input}
      <View style={{ position: "absolute", right: space.gap }}>{right}</View>
    </View>
  ) : input;
  if (!label) return box;
  return (
    <View>
      <Muted style={{ marginLeft: space.inset, marginTop: space.item, marginBottom: space.hair }}>{label}</Muted>
      {box}
    </View>
  );
}

/**
 * A text button inside a field (Show, Hide): `link` text; `space.item` at the sides and `space.group` above and
 * below (a 44 pt touch area). `label` is the name a screen reader reads ("Show password").
 */
export function FieldButton({ title, label, onPress }: { title: string; label?: string; onPress: () => void }) {
  const col = useColors();
  return (
    <Pressable accessibilityRole="button" accessibilityLabel={label ?? title} onPress={onPress}
      style={({ pressed }) => ({ paddingHorizontal: space.item, paddingVertical: space.group, opacity: pressed ? opacity.pressed : 1 })}>
      <Text style={[type.button, { color: col.link }]}>{title}</Text></Pressable>
  );
}

/** An action in a line of text ("Create one"): `type.button`, `link`, underlined; a screen reader reads a button. */
export function TextLink({ children, onPress, testID, name, touch }: {
  children: React.ReactNode; onPress: () => void; testID?: string; name?: string; touch?: boolean;
}) {
  const col = useColors();
  // R5: `touch` gives the link a 44 pt tall touch area (space.group above and below the 20 pt line; Platform rule)
  return (
    <Text testID={testID} accessibilityRole="button" accessibilityLabel={name} onPress={onPress}
      style={[type.button, { color: col.link, textDecorationLine: "underline" }, touch ? { paddingVertical: space.group } : null]}>{children}</Text>
  );
}

// ---------------------------------------------------------------- motion

/** One row of a sheet: it rises after the rows before it (index i). */
export function Rise({ i, children }: { i: number; children: React.ReactNode }) {
  const [t] = useState(() => new Animated.Value(0));
  useEffect(() => Animated.timing(t, { toValue: 1, duration: motion.rowMs, delay: i * motion.rowStepMs, useNativeDriver: true }).start(), [t, i]);
  const translateY = t.interpolate({ inputRange: [0, 1], outputRange: [motion.rowRise, 0] });
  return <Animated.View style={{ opacity: t, transform: [{ translateY }] }}>{children}</Animated.View>;
}

/** True while the device asks for less motion ("reduce motion"); false when it is not known. */
export function useStill(): boolean {
  const [still, setStill] = useState(false);
  useEffect(function askMotion() {
    let live = true;
    RN.AccessibilityInfo.isReduceMotionEnabled().then((on) => { if (live) setStill(on); }).catch(() => undefined);
    const sub = RN.AccessibilityInfo.addEventListener("reduceMotionChanged", (on) => setStill(on));
    return () => { live = false; sub.remove(); };
  }, []);
  return still;
}

/**
 * X49: lines that type in, one letter each `motion.letterMs` and a `motion.lineMs` pause at the end of a line,
 * `delay` ms after the part shows. The first line is `fg`, the next ones `muted`. With "reduce motion" on, all of
 * it shows at once. A screen reader reads all the text at once.
 */
export function TypedText({ lines, delay }: { lines: string[]; delay: number }) {
  const col = useColors();
  const still = useStill();
  const all = lines.join("\n");
  const [n, setN] = useState(0);
  useEffect(function typeIn() {
    if (still) return;
    let k = 0;
    let timer: ReturnType<typeof setTimeout>;
    setN(0);
    const step = () => {
      k++;
      setN(k);
      const next = typeDelay(all, k, motion.letterMs, motion.lineMs);
      if (next >= 0) timer = setTimeout(step, next);
    };
    timer = setTimeout(step, delay);
    return () => clearTimeout(timer);
  }, [all, delay, still]);
  const shown = (still ? all : all.slice(0, n)).split("\n");
  return (
    <View accessible accessibilityLabel={lines.join(" ")} style={{ gap: space.hair }}>
      {shown.map((l, k) => <Text key={k} style={[type.body, { color: k === 0 ? col.fg : col.muted }]}>{l}</Text>)}
    </View>
  );
}

/** X49: a part that opens: it fades in and grows in `motion.growMs` (at once with "reduce motion"). */
export function Grow({ children, style }: { children: React.ReactNode; style?: StyleProp<ViewStyle> }) {
  const still = useStill();
  const [t] = useState(() => new Animated.Value(0));
  useEffect(() => Animated.timing(t, { toValue: 1, duration: still ? 0 : motion.growMs, useNativeDriver: true }).start(), [t, still]);
  const scale = t.interpolate({ inputRange: [0, 1], outputRange: [motion.growFrom, 1] });
  return <Animated.View style={[{ opacity: t, transform: [{ scale }] }, style]}>{children}</Animated.View>;
}

// ---------------------------------------------------------------- sheets and dialogs

/** The frosted layer of a sheet or dialog: the blur of what is behind it, with a tint over it. */
export function Frost({ round, children, style }: { round: ViewStyle; children: React.ReactNode; style?: StyleProp<ViewStyle> }) {
  const col = useColors();
  return (
    <View style={[round, { overflow: "hidden" }, style]}>
      {/* X49h: the blur and the tint take the corners too (a round button showed a square blur on the web) */}
      <BlurView intensity={size.blur} tint={col.blurTint === "dark" ? "dark" : "light"} style={[StyleSheet.absoluteFill, round]} />
      <View style={[StyleSheet.absoluteFill, round, { backgroundColor: col.sheetTint }]} />
      {children}
    </View>
  );
}

/** The photo behind a sheet or dialog, with the shade over it (darker to the bottom). */
function Backdrop({ photo }: { photo: string }) {
  const col = useColors();
  return (
    <>
      <Image source={{ uri: photo }} resizeMode="cover" style={StyleSheet.absoluteFill} />
      <ShadeGradient pointerEvents="none" colors={[col.shadeTop, col.shadeMid, col.shadeEnd]} locations={[0, shade.mid, 1]}
        style={StyleSheet.absoluteFill} />
    </>
  );
}

/**
 * A sheet over a photo. Compact width: a bottom sheet with a grab bar ("large" = as tall as its content, at most
 * 85 % of the screen height; "small" = at least 210 pt). Medium and expanded width: a centred dialog ("wide" =
 * 860 pt with the photo on the left; X49 "extraWide" = 1140 pt, the review with the details column; else 440 pt).
 */
export function Sheet({ kind, wide, extraWide, photo, children, testID, onClose, closeName }: {
  kind: "large" | "small"; wide?: boolean; extraWide?: boolean; photo?: string; children: React.ReactNode; testID?: string;
  onClose?: () => void; closeName?: string;
}) {
  const { width, height } = useWindowDimensions();
  const dialogs = layoutFor(width).dialogs;
  const col = useColors();
  const [rise] = useState(() => new Animated.Value(0));
  useEffect(() => Animated.spring(rise, { toValue: 1, useNativeDriver: true, bounciness: motion.sheetBounce, speed: motion.sheetSpeed }).start(), [rise]);
  const shift = rise.interpolate({ inputRange: [0, 1], outputRange: [height * motion.sheetShift, 0] });
  const inset = useInsets();
  const back = photo ? <Backdrop photo={photo} /> : null;
  // onClose: a ✕ in the top corner, and a tap on the photo closes the sheet (people did not know the tap)
  const tapOut = onClose ? <Pressable accessible={false} onPress={onClose} style={StyleSheet.absoluteFill} /> : null;
  const cross = onClose ? (
    <View style={{ position: "absolute", top: space.item, right: space.item }}>
      <IconBtn testID="sheet-close" glyph="close" label={closeName ?? ""} onPress={onClose} /></View>
  ) : null;
  if (dialogs) {
    const w = extraWide ? size.dialogExtraWide : wide ? size.dialogWide : size.dialogSmall;
    return (
      <View testID={testID} style={{ flex: 1, backgroundColor: col.bg }}>
        {back}{tapOut}
        <View pointerEvents="box-none" style={[StyleSheet.absoluteFill, { alignItems: "center", justifyContent: "center" }]}>
          <Animated.View style={{ width: Math.min(w, width - 2 * space.gutter), maxHeight: Math.round(height * size.dialogMax),
            transform: [{ translateY: shift }] }}>
            <Frost round={{ borderRadius: radius.sheet }} style={{ padding: space.pad, flexShrink: 1 }}>{children}{cross}</Frost></Animated.View>
        </View>
      </View>
    );
  }
  const large = kind === "large";
  return (
    <View testID={testID} style={{ flex: 1, backgroundColor: col.bg }}>
      {back}{tapOut}
      <Animated.View style={{ position: "absolute", left: 0, right: 0, bottom: 0, maxHeight: Math.round(height * size.sheetLarge), // whole pt: the parts stay on whole pixels
        minHeight: large ? undefined : size.sheetSmall, transform: [{ translateY: shift }] }}>
        <Frost round={{ borderTopLeftRadius: radius.sheet, borderTopRightRadius: radius.sheet }}
          style={{ flexShrink: 1, flexGrow: 1, paddingHorizontal: space.gutter, paddingBottom: space.foot + inset.bottom }}>
          <View style={{ alignSelf: "center", width: size.handleW, height: size.handleH, borderRadius: radius.round,
            backgroundColor: col.handle, marginTop: size.handleTop, marginBottom: space.gutter }} />
          {children}{cross}
        </Frost></Animated.View>
    </View>
  );
}

/** The props that make a view a modal dialog for screen readers (iOS: accessibilityViewIsModal; web: dialog). */
const MODAL = { accessibilityViewIsModal: true, role: "dialog" as const, "aria-modal": true };

/**
 * Web: the focus goes into `el` and Tab and Shift+Tab stay in it (a modal dialog, WAI-ARIA APG). Returns the
 * function that ends it and gives the focus back.
 */
function trapFocus(el: HTMLElement | null): (() => void) | undefined {
  if (!el || typeof document === "undefined") return undefined;
  const before = document.activeElement as HTMLElement | null;
  const inside = () => Array.from(el.querySelectorAll<HTMLElement>('[tabindex]:not([tabindex="-1"]), button, input, textarea, a[href]'));
  before?.blur?.();
  const first = inside()[0];
  first?.focus?.();
  const tab = (e: KeyboardEvent) => {
    if (e.key !== "Tab") return;
    const all = inside();
    if (all.length === 0) return;
    const at = all.indexOf(document.activeElement as HTMLElement);
    const next = e.shiftKey ? (at <= 0 ? all.length - 1 : at - 1) : (at === -1 || at === all.length - 1 ? 0 : at + 1);
    e.preventDefault();
    all[next].focus();
  };
  document.addEventListener("keydown", tab, true);
  return () => { document.removeEventListener("keydown", tab, true); before?.focus?.(); };
}

/**
 * A sheet over the screen on view (the role sheet). Behind it: `scrimDim` (it fades in in `motion.dimMs`), or no
 * dim with `dim={false}`. Compact width: a bottom sheet with the grab bar, `size.sheetMedium` of the screen height.
 * Wide: a centred dialog, `size.dialogSmall` wide. With `onDismiss`, a tap on the dim layer or Esc (web) closes it.
 * X48t: `small` (a confirm): on a compact width the sheet is the small sheet (at least `size.sheetSmall`, as high as
 * its content), not `size.sheetMedium` of the screen.
 */
export function Popup({ dim = true, onDismiss, dismissName, children, testID, small }: {
  dim?: boolean; onDismiss?: () => void; dismissName?: string; children: React.ReactNode; testID?: string; small?: boolean;
}) {
  const { width, height } = useWindowDimensions();
  const dialogs = layoutFor(width).dialogs;
  const col = useColors();
  const inset = useInsets();
  const [rise] = useState(() => new Animated.Value(0));
  const [fade] = useState(() => new Animated.Value(0));
  useEffect(function riseIn() {
    Animated.spring(rise, { toValue: 1, useNativeDriver: true, bounciness: motion.sheetBounce, speed: motion.sheetSpeed }).start();
    Animated.timing(fade, { toValue: 1, duration: motion.dimMs, useNativeDriver: true }).start();
  }, [rise, fade]);
  const box = useBox<React.ComponentRef<typeof View>>(null);
  useEffect(function escCloses() {
    if (RN.Platform.OS !== "web" || !onDismiss) return;
    const escape = (e: KeyboardEvent) => e.key === "Escape" && onDismiss();
    window.addEventListener("keydown", escape);
    return () => window.removeEventListener("keydown", escape);
  }, [onDismiss]);
  useEffect(function backCloses() {
    if (RN.Platform.OS === "web" || !onDismiss) return;
    const sub = RN.BackHandler.addEventListener("hardwareBackPress", () => { onDismiss(); return true; });
    return () => sub.remove();
  }, [onDismiss]);
  useEffect(function focusStaysIn() {
    if (RN.Platform.OS !== "web") return;
    return trapFocus(box.current as unknown as HTMLElement | null);
  }, []);
  const shift = rise.interpolate({ inputRange: [0, 1], outputRange: [height * motion.sheetShift, 0] });
  const tapOut = onDismiss ? (
    <Pressable accessibilityRole="button" accessibilityLabel={dismissName} onPress={onDismiss} style={StyleSheet.absoluteFill} />
  ) : null;
  const layer = dim ? <Animated.View pointerEvents="none" style={[StyleSheet.absoluteFill, { backgroundColor: col.scrimDim, opacity: fade }]} /> : null;
  if (dialogs) {
    return (
      <View testID={testID} style={StyleSheet.absoluteFill}>
        {layer}{tapOut}
        <View pointerEvents="box-none" style={[StyleSheet.absoluteFill, { alignItems: "center", justifyContent: "center" }]}>
          <Animated.View ref={box} {...MODAL} style={{ width: Math.min(size.dialogSmall, width - 2 * space.gutter),
            maxHeight: Math.round(height * size.dialogMax), transform: [{ translateY: shift }] }}>
            <Frost round={{ borderRadius: radius.sheet }} style={{ padding: space.pad, flexShrink: 1 }}>{children}</Frost></Animated.View>
        </View>
      </View>
    );
  }
  return (
    <View testID={testID} style={StyleSheet.absoluteFill}>
      {layer}{tapOut}
      <Animated.View ref={box} {...MODAL} style={{ position: "absolute", left: 0, right: 0, bottom: 0, minHeight: small ? size.sheetSmall : Math.round(height * size.sheetMedium),
        maxHeight: Math.round(height * size.sheetLarge), transform: [{ translateY: shift }] }}>
        <Frost round={{ borderTopLeftRadius: radius.sheet, borderTopRightRadius: radius.sheet }}
          style={{ flex: 1, paddingHorizontal: space.gutter, paddingBottom: space.foot + inset.bottom }}>
          <View style={{ alignSelf: "center", width: size.handleW, height: size.handleH, borderRadius: radius.round,
            backgroundColor: col.handle, marginTop: size.handleTop, marginBottom: space.gutter }} />
          {children}
        </Frost></Animated.View>
    </View>
  );
}

/** A solid card dialog on a dimmed page (the camera on a wide screen): 640 pt, 16 pt padding. */
export function CardDialog({ children, testID }: { children: React.ReactNode; testID?: string }) {
  const col = useColors();
  return (
    <View testID={testID} style={{ flex: 1, backgroundColor: col.bg }}>
      <View style={[StyleSheet.absoluteFill, { backgroundColor: col.scrimDim, alignItems: "center", justifyContent: "center" }]}>
        <View style={{ width: size.dialogCamera, maxWidth: "100%", padding: space.gutter, borderRadius: radius.sheet, backgroundColor: col.card }}>
          {children}
        </View>
      </View>
    </View>
  );
}

/** The scrolling body of a large sheet (it ends 8 pt under its content; the pills stay under it). */
export function SheetBody({ children }: { children: React.ReactNode }) {
  return <ScrollView style={{ flexShrink: 1 }} contentContainerStyle={{ paddingBottom: space.gap }}>{children}</ScrollView>;
}

/** The two pills at the bottom of a large sheet, 16 pt under its body. */
export function SheetPills({ children }: { children: React.ReactNode }) {
  return <Row style={{ marginTop: space.gutter }}>{children}</Row>;
}

/** A photo that may still be loading (an empty frame in the soft accent). Square, or `side` × `side`. */
export function Photo({ uri, side, round, style }: { uri?: string; side?: number; round: keyof typeof radius; style?: StyleProp<ViewStyle> }) {
  const col = useColors();
  const box: ViewStyle = side ? { width: side, height: side } : { width: "100%", aspectRatio: 1 };
  return (
    <View style={[box, { borderRadius: radius[round], overflow: "hidden", backgroundColor: col.accentSoft }, style]}>
      {uri ? <Image source={{ uri }} resizeMode="cover" style={StyleSheet.absoluteFill} /> : null}
    </View>
  );
}

/**
 * A short message at the bottom of the screen (`fg` and `bg` reversed), above the home bar. The live region is
 * always there, so a screen reader reads each new text; iOS gets an announcement.
 */
export function Toast({ text }: { text: string }): React.ReactElement {
  const col = useColors();
  const inset = useInsets();
  useEffect(function announce() {
    if (text && RN.Platform.OS === "ios") RN.AccessibilityInfo.announceForAccessibility(text);
  }, [text]);
  return (
    <View pointerEvents="none" accessibilityLiveRegion="polite" aria-live="polite"
      style={{ position: "absolute", left: space.gutter, right: space.gutter, alignItems: "center",
        bottom: (layoutFor(useWindowDimensions().width).dialogs ? size.toastBottomWide : size.toastBottom) + inset.bottom }}>
      {text ? (
        <Text testID="toast" style={[type.body, { color: col.bg, backgroundColor: col.fg, borderRadius: radius.round, overflow: "hidden",
          paddingHorizontal: camera.hintPadX, paddingVertical: camera.hintPadY, textAlign: "center" }]}>{text}</Text>
      ) : null}
    </View>
  );
}

// ---------------------------------------------------------------- listings

/** The photo of a listing: the route path, as a URI the <Image> can load (api.photoUrl); "" until it is known. */
export function usePhoto(path?: string): string {
  const [uri, setUri] = useState("");
  useEffect(function loadPhoto() {
    let live = true; // false after unmount or a new path
    setUri("");
    if (path) photoUri(path).then((u) => live && setUri(u)).catch(() => undefined);
    return () => void (live = false);
  }, [path]);
  return uri;
}

/** The time now, again every `ms` (for the time pills). */
export function useNow(ms = 1000): number {
  const [now, setNow] = useState(() => Date.now());
  useEffect(function tick() {
    const timer = setInterval(() => setNow(Date.now()), ms);
    return () => clearInterval(timer);
  }, [ms]);
  return now;
}

/** The time pill of a card: m:ss on `accent`, or "Collected" / "Expired" on `iconFill`. `spoken` goes before m:ss. */
export function TimePill({ text, gone, spoken }: { text: string; gone: boolean; spoken?: string }) {
  const col = useColors();
  return (
    <View accessible aria-label={gone || !spoken ? text : spoken + " " + text} style={{ width: size.timeW, paddingVertical: size.timePadY, borderRadius: radius.round, alignItems: "center",
      backgroundColor: gone ? col.iconFill : col.accent }}>
      <Text style={[type.button, { color: gone ? col.muted : col.onAccent, fontVariant: ["tabular-nums"] }]}>{text}</Text></View>
  );
}

/**
 * A photo-first listing card: the photo (`size.feedPhoto` on Receive, `size.minePhoto` on Give), with a panel on
 * it: the title, the servings and the time pill, and below them `extra` (the pickup text, "Your listing", Grab).
 */
export function ListingCard({ photoPath, title, line, time, timeName, gone, tall, extra, testID, corner }: {
  photoPath: string; title: string; line: string; time: string; timeName?: string; gone: boolean; tall: boolean; extra?: React.ReactNode;
  testID?: string;
  /** X49h: a control at the top right of the photo, `space.inset` from the edges (Delete on Give). */
  corner?: React.ReactNode;
}) {
  const col = useColors();
  const uri = usePhoto(photoPath);
  return (
    <View testID={testID} style={{ borderRadius: radius.card, overflow: "hidden", backgroundColor: col.accentSoft }}>
      <View style={{ height: tall ? size.feedPhoto : size.minePhoto, opacity: gone ? opacity.gone : 1 }}>
        {uri ? <Image source={{ uri }} resizeMode="cover" accessibilityIgnoresInvertColors accessibilityLabel={title} alt={title}
          style={StyleSheet.absoluteFill} /> : null}</View>
      <View style={{ position: "absolute", left: space.inset, right: space.inset, bottom: space.inset, padding: space.group,
        gap: space.hair, borderRadius: radius.photo, backgroundColor: col.panel }}>
        <View style={{ gap: space.hair, opacity: gone ? opacity.gone : 1 }}>
          <Text numberOfLines={2} style={[type.button, { color: col.fg }]}>{title}</Text>
          <Row tight style={{ justifyContent: "space-between" }}><Muted>{line}</Muted><TimePill text={time} gone={gone} spoken={timeName} /></Row>
        </View>
        {extra /* R5 check: the actions stay at full strength on a gone card (4.5:1) */}
      </View>
      {corner ? <View style={{ position: "absolute", top: space.inset, right: space.inset }}>{corner}</View> : null}
    </View>
  );
}

/**
 * X49h (T9: "an X with a semi opaque frosted circle"): a round frosted icon button on a photo, `size.iconSmall`
 * (its touch area grows to `size.iconBig`), the glyph in `fg` on the frosted layer of the sheets.
 */
export function FrostBtn({ glyph: g, label, onPress, testID }: { glyph: Glyph; label: string; onPress: () => void; testID?: string }) {
  const col = useColors();
  return (
    <Pressable testID={testID} accessibilityRole="button" accessibilityLabel={label} onPress={onPress} hitSlop={(size.iconBig - size.iconSmall) / 2}
      style={(state) => ({ width: size.iconSmall, height: size.iconSmall, borderRadius: radius.round,
        opacity: state.pressed ? opacity.pressed : hovered(state) ? opacity.hover : 1 })}>
      <Frost round={{ borderRadius: radius.round }} style={{ width: size.iconSmall, height: size.iconSmall, alignItems: "center", justifyContent: "center" }}>
        {/* a View: the glyph paints over the two layers of the frost (they are positioned) */}
        <View><GlyphIcon name={g} color={col.fg} /></View></Frost>
    </Pressable>
  );
}

/** X49h (T9: "the same size as the timer pill"): a primary pill the size of the time pill (`size.timeW`, `size.timePadY`,
 *  `type.button` in `onAccent` on `accent`); its touch area grows by `space.group`. */
export function SmallPill({ title, name, onPress, testID }: { title: string; name?: string; onPress: () => void; testID?: string }) {
  const col = useColors();
  return (
    <Pressable testID={testID} accessibilityRole="button" accessibilityLabel={name ?? title} onPress={onPress} hitSlop={space.group}
      style={(state) => ({ width: size.timeW, paddingVertical: size.timePadY, borderRadius: radius.round, alignItems: "center",
        backgroundColor: col.accent, opacity: state.pressed ? opacity.pressed : hovered(state) ? opacity.hover : 1 })}>
      <Text numberOfLines={1} style={[type.button, { color: col.onAccent }]}>{title}</Text></Pressable>
  );
}

/** Cards in `columns` equal columns, `space.grid` apart. */
export function Grid({ columns, children: cards }: { columns: number; children: React.ReactNode[] }) {
  const half = space.grid / 2;
  return (
    <View style={{ flexDirection: "row", flexWrap: "wrap", marginHorizontal: -half }}>
      {cards.map((card, k) => (
        <View key={(card as { key?: string | null }).key ?? k} style={{ width: `${100 / columns}%`, paddingHorizontal: half, marginBottom: space.grid }}>{card}</View>
      ))}
    </View>
  );
}

// ---------------------------------------------------------------- screen head

/** U19: the title row of a role screen (Give, Receive): one height (a control), so the title is at the same level
 *  in both roles, with or without the "+ New listing" pill next to it. */
export function ScreenHead({ title, action }: { title: string; action?: React.ReactNode }) {
  return (
    <View style={{ minHeight: size.control, marginBottom: space.gutter, flexDirection: "row", alignItems: "center", justifyContent: "space-between" }}>
      <Title>{title}</Title>{action ?? null}</View>
  );
}

// ---------------------------------------------------------------- review card

/**
 * One found item in the review list: the item photo (94 pt), then two rows: the title field with ✕ (remove),
 * and − servings +. `n` is the item number (1, 2, …) for the labels. `error` shows under the rows.
 */
export function ReviewCard({ uri, n, title, servings, error, extra, onTitle, onServings, onRemove, onLess, onMore }: {
  uri?: string; n: number; title: string; servings: number | string; error?: string; extra?: React.ReactNode;
  onTitle?: (t: string) => void; onServings?: (t: string) => void; onRemove: () => void; onLess: () => void; onMore: () => void;
}) {
  const col = useColors();
  return (
    <View style={{ marginTop: space.item, flexDirection: "row", gap: space.item, padding: space.inset, borderRadius: radius.card,
      borderWidth: size.border, borderColor: col.line, backgroundColor: col.card }}>
      <Photo uri={uri} side={size.thumb} round="photo" style={{ alignSelf: "center" }} />
      <View style={{ flex: 1, gap: space.gap }}>
        <Row tight>
          <View style={{ flex: 1 }}><Field accessibilityLabel={"Title of item " + n} value={title} onChangeText={onTitle} maxLength={LIMITS.title} /></View>
          <IconBtn glyph="close" label={"Remove item " + n} onPress={onRemove} />
        </Row>
        <Row tight>
          <IconBtn glyph="minus" label={"Fewer servings of item " + n} onPress={onLess} />
          <View style={{ flex: 1 }}>
            <Field accessibilityLabel={"Servings of item " + n} value={String(servings)} onChangeText={onServings} keyboardType="number-pad" center />
          </View>
          <IconBtn glyph="plus" label={"More servings of item " + n} onPress={onMore} />
        </Row>
        {extra}
        {error ? <ErrorText lines={2}>{error}</ErrorText> : null}
      </View>
    </View>
  );
}
