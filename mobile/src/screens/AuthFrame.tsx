// The frame of the sign-in and reset screens, written new for V3RDE (NEW_WORK_UI.md, UI 9; it was in Login.tsx,
// UI 8). Phone: the content at the top, the action pill at the bottom. Wide screen: a centred card, which fades and
// rises away (`leaving`) when the next screen comes in. The doodle wallpaper is behind both.
import React, { useEffect, useState } from "react";
import { Animated, KeyboardAvoidingView, ScrollView, View, useWindowDimensions } from "react-native";
import * as RN from "react-native";
import { layoutFor, motion, radius, size, space, useColors } from "../theme";
import { Doodles as Wallpaper } from "../doodles";

export function AuthFrame({ children, action, leaving }: { children: React.ReactNode; action: React.ReactNode; leaving?: boolean }) {
  const col = useColors();
  const { width } = useWindowDimensions();
  const [away] = useState(() => new Animated.Value(0));
  useEffect(() => Animated.timing(away, { toValue: leaving ? 1 : 0, duration: motion.handoffMs, useNativeDriver: RN.Platform.OS !== "web" }).start(), [leaving, away]);
  if (layoutFor(width).dialogs) {
    const fade = away.interpolate({ inputRange: [0, 1], outputRange: [1, 0] });
    const lift = away.interpolate({ inputRange: [0, 1], outputRange: [0, -motion.handoffLift] });
    return (
      <View style={{ flex: 1, backgroundColor: col.bg }} pointerEvents={leaving ? "none" : "auto"}>
        <Wallpaper />
        <ScrollView contentContainerStyle={{ flexGrow: 1, alignItems: "center", justifyContent: "center", padding: space.pad }}>
          <Animated.View style={{ width: "100%", maxWidth: size.dialogSmall, padding: space.pad, borderRadius: radius.sheet,
            borderWidth: size.border, borderColor: col.line, backgroundColor: col.card, opacity: fade, transform: [{ translateY: lift }] }}>
            {children}
            <View style={{ marginTop: space.pad }}>{action}</View>
          </Animated.View></ScrollView>
      </View>
    );
  }
  return (
    <KeyboardAvoidingView style={{ flex: 1, backgroundColor: col.bg }} behavior={RN.Platform.OS === "ios" ? "padding" : undefined}
      pointerEvents={leaving ? "none" : "auto"}>
      <Wallpaper />
      <ScrollView keyboardShouldPersistTaps="handled"
        contentContainerStyle={{ flexGrow: 1, paddingHorizontal: space.gutter, paddingTop: Math.round(width * size.loginTop) }}>
        {children}</ScrollView>
      <View style={{ padding: space.gutter }}>{action}</View></KeyboardAvoidingView>
  );
}
