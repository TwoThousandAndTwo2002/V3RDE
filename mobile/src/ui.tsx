// V3RDE shared UI parts, written new for V3RDE (NEW_WORK_UI.md, UI 5; mobile/docs/style_guide.md).
// Every colour, size and font comes from theme.ts (tests/style.test.mts checks it).
import React, { useEffect, useState } from "react";
import * as RN from "react-native";
import { ActivityIndicator, Animated, Image, Pressable, ScrollView, StyleSheet, Text, TextInput, View, useWindowDimensions } from "react-native";
import type { StyleProp, TextInputProps, TextStyle, ViewStyle } from "react-native";
import { BlurView } from "expo-blur";
import { LinearGradient as ShadeGradient } from "expo-linear-gradient";
import Svg, { Path } from "react-native-svg";
import { camera, glyph, layoutFor, motion, opacity, radius, shade, size, space, type, useColors } from "./theme";

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

export type Glyph = "close" | "minus" | "plus" | "upload";

/** A line glyph (close ✕, minus −, plus +, upload): one SVG path with round ends, in its own square box. */
export function GlyphIcon({ name, color }: { name: Glyph; color: string }) {
  const box = name === "upload" ? glyph.uploadBox : glyph.box;
  return (
    <Svg width={box} height={box} viewBox={`0 0 ${box} ${box}`} accessible={false}>
      <Path d={glyph[name]} stroke={color} strokeWidth={glyph.stroke} strokeLinecap="round" strokeLinejoin="round" fill="none" />
    </Svg>
  );
}

/** A round icon button: 38 pt inside a review card, 44 pt (the touch size) elsewhere. */
export function IconBtn({ glyph, label, onPress, big }: { glyph: Glyph; label: string; onPress: () => void; big?: boolean }) {
  const col = useColors();
  const d = big ? size.iconBig : size.iconSmall;
  return (
    <Pressable accessibilityRole="button" accessibilityLabel={label} onPress={onPress} hitSlop={big ? 0 : (size.iconBig - d) / 2}
      style={({ pressed }) => ({ width: d, height: d, borderRadius: radius.round, alignItems: "center", justifyContent: "center",
        backgroundColor: col.iconFill, opacity: pressed ? opacity.pressed : 1 })}>
      <GlyphIcon name={glyph} color={col.fg} /></Pressable>
  );
}

/** A pill button: primary (red) or secondary (white with a line). `grow` shares a row with another pill. */
export function Pill({ title, onPress, kind = "primary", busy, busyName, grow, testID }: {
  title: string; onPress?: () => void; kind?: "primary" | "secondary"; busy?: boolean; busyName?: string; grow?: boolean; testID?: string;
}) {
  const col = useColors();
  const isPrimary = kind !== "secondary";
  const fg = isPrimary ? col.onAccent : col.fg;
  return (
    <Pressable testID={testID} accessibilityRole="button" accessibilityLabel={busy && busyName ? busyName : title} aria-busy={!!busy}
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
export function Hint({ children }: { children: React.ReactNode }) {
  const col = useColors();
  return (
    <View pointerEvents="none" style={{ position: "absolute", left: space.gutter, right: space.gutter, bottom: camera.hintBottom, alignItems: "center" }}>
      <Text style={[type.body, { color: col.cameraFg, backgroundColor: col.cameraBtn, borderRadius: radius.round, overflow: "hidden",
        paddingHorizontal: camera.hintPadX, paddingVertical: camera.hintPadY, textAlign: "center" }]}>{children}</Text>
    </View>
  );
}

/** A progress bar: the accent over the soft accent, fully round. `share` is 0..1. */
export function Progress({ share }: { share: number }) {
  const col = useColors();
  const k = Math.max(0, Math.min(1, share));
  return (
    <View accessibilityRole="progressbar" style={{ height: size.progressH, borderRadius: radius.round, backgroundColor: col.accentSoft, overflow: "hidden" }}>
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
export function TextLink({ children, onPress, testID }: { children: React.ReactNode; onPress: () => void; testID?: string }) {
  const col = useColors();
  return (
    <Text testID={testID} accessibilityRole="button" onPress={onPress}
      style={[type.button, { color: col.link, textDecorationLine: "underline" }]}>{children}</Text>
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

// ---------------------------------------------------------------- sheets and dialogs

/** The frosted layer of a sheet or dialog: the blur of what is behind it, with a tint over it. */
function Frost({ round, children, style }: { round: ViewStyle; children: React.ReactNode; style?: StyleProp<ViewStyle> }) {
  const col = useColors();
  return (
    <View style={[round, { overflow: "hidden" }, style]}>
      <BlurView intensity={size.blur} tint={col.blurTint === "dark" ? "dark" : "light"} style={StyleSheet.absoluteFill} />
      <View style={[StyleSheet.absoluteFill, { backgroundColor: col.sheetTint }]} />
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
 * 860 pt with the photo on the left, else 440 pt).
 */
export function Sheet({ kind, wide, photo, children, testID }: {
  kind: "large" | "small"; wide?: boolean; photo?: string; children: React.ReactNode; testID?: string;
}) {
  const { width, height } = useWindowDimensions();
  const dialogs = layoutFor(width).dialogs;
  const col = useColors();
  const [rise] = useState(() => new Animated.Value(0));
  useEffect(() => Animated.spring(rise, { toValue: 1, useNativeDriver: true, bounciness: motion.sheetBounce, speed: motion.sheetSpeed }).start(), [rise]);
  const shift = rise.interpolate({ inputRange: [0, 1], outputRange: [height * motion.sheetShift, 0] });
  const back = photo ? <Backdrop photo={photo} /> : null;
  if (dialogs) {
    const w = wide ? size.dialogWide : size.dialogSmall;
    return (
      <View testID={testID} style={{ flex: 1, backgroundColor: col.bg }}>
        {back}
        <View style={[StyleSheet.absoluteFill, { alignItems: "center", justifyContent: "center" }]}>
          <Animated.View style={{ width: Math.min(w, width - 2 * space.gutter), maxHeight: Math.round(height * size.dialogMax),
            transform: [{ translateY: shift }] }}>
            <Frost round={{ borderRadius: radius.sheet }} style={{ padding: space.pad, flexShrink: 1 }}>{children}</Frost></Animated.View>
        </View>
      </View>
    );
  }
  const large = kind === "large";
  return (
    <View testID={testID} style={{ flex: 1, backgroundColor: col.bg }}>
      {back}
      <Animated.View style={{ position: "absolute", left: 0, right: 0, bottom: 0, maxHeight: Math.round(height * size.sheetLarge), // whole pt: the parts stay on whole pixels
        minHeight: large ? undefined : size.sheetSmall, transform: [{ translateY: shift }] }}>
        <Frost round={{ borderTopLeftRadius: radius.sheet, borderTopRightRadius: radius.sheet }}
          style={{ flexShrink: 1, flexGrow: 1, paddingHorizontal: space.gutter, paddingBottom: space.foot }}>
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

// ---------------------------------------------------------------- review card

/**
 * One found item in the review list: the item photo (94 pt), then two rows: the title field with ✕ (remove),
 * and − servings +. `n` is the item number (1, 2, …) for the labels. `error` shows under the rows.
 */
export function ReviewCard({ uri, n, title, servings, error, onTitle, onServings, onRemove, onLess, onMore }: {
  uri?: string; n: number; title: string; servings: number; error?: string;
  onTitle?: (t: string) => void; onServings?: (t: string) => void; onRemove: () => void; onLess: () => void; onMore: () => void;
}) {
  const col = useColors();
  return (
    <View style={{ marginTop: space.item, flexDirection: "row", gap: space.item, padding: space.inset, borderRadius: radius.card,
      borderWidth: size.border, borderColor: col.line, backgroundColor: col.card }}>
      <Photo uri={uri} side={size.thumb} round="photo" style={{ alignSelf: "center" }} />
      <View style={{ flex: 1, gap: space.gap }}>
        <Row tight>
          <View style={{ flex: 1 }}><Field accessibilityLabel={"Title of item " + n} value={title} onChangeText={onTitle} /></View>
          <IconBtn glyph="close" label={"Remove item " + n} onPress={onRemove} />
        </Row>
        <Row tight>
          <IconBtn glyph="minus" label={"Fewer servings of item " + n} onPress={onLess} />
          <View style={{ flex: 1 }}>
            <Field accessibilityLabel={"Servings of item " + n} value={String(servings)} onChangeText={onServings} keyboardType="number-pad" center />
          </View>
          <IconBtn glyph="plus" label={"More servings of item " + n} onPress={onMore} />
        </Row>
        {error ? <ErrorText lines={2}>{error}</ErrorText> : null}
      </View>
    </View>
  );
}
