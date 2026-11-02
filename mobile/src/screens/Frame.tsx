// The frame of the signed-in screens, written new for V3RDE (NEW_WORK_UI.md, UI 11; mobile/docs/style_guide.md
// "Frame"). Phone and tablet: a ☰ header that opens the role sheet. Wide screen: a top bar with the brand, the
// role tabs (Give, Receive) and Log out; the role sheet is not needed there. LG1: the brand is the V3RDE logo
// (src/brand.tsx): the mark and the wordmark in the top bar, the mark alone in the phone header.
import type { ReactNode } from "react";
import { Pressable, Text, View, useWindowDimensions } from "react-native";
import * as RN from "react-native";
import { layoutFor, opacity, radius, size, space, type, useColors } from "../theme";
import { IconBtn } from "../ui";
import { PhoneBrand, TopBrand } from "../brand";
import { FRAME, ROLE, roleCard, type RoleName } from "../words";

/** Web: Left and Right on the tab list choose the other role and move the focus to its tab. */
function arrows(role: RoleName, onRole: (r: RoleName) => void): object {
  if (RN.Platform.OS !== "web") return {};
  const onKeyDown = (e: { key: string }) => {
    if (e.key !== "ArrowLeft" && e.key !== "ArrowRight") return;
    const other: RoleName = role === "give" ? "receive" : "give";
    onRole(other);
    setTimeout(() => (document.querySelector(`[data-testid="tab-${other}"]`) as HTMLElement | null)?.focus(), 0);
  };
  return { onKeyDown };
}

/** True while the mouse is on a control (react-native-web adds `hovered` to the Pressable state). */
const isHovered = (state: object) => (state as { hovered?: boolean }).hovered === true;

/** A role tab. Only the chosen tab is a Tab-key stop; the arrow keys move to the other tab (WAI-ARIA tabs). */
function Tab({ role, on, onPress }: { role: RoleName; on: boolean; onPress: () => void }) {
  const col = useColors();
  return (
    <Pressable testID={"tab-" + role} accessibilityRole="tab" aria-selected={on} aria-controls="role-panel" focusable={on} onPress={onPress}
      style={(state) => ({ height: size.control, paddingHorizontal: size.tabPadX, borderRadius: radius.round, justifyContent: "center",
        backgroundColor: on ? col.accentSoft : isHovered(state) ? col.iconFill : col.bg, opacity: state.pressed ? opacity.pressed : 1 })}>
      <Text style={[type.button, { color: on ? col.link : col.muted }]}>{roleCard(role).title}</Text></Pressable>
  );
}

export function Frame({ role, onRole, onMenu, onLogout, children }: {
  role: RoleName; onRole: (r: RoleName) => void; onMenu: () => void; onLogout: () => void; children: ReactNode;
}) {
  const col = useColors();
  const page = layoutFor(useWindowDimensions().width);
  const lane = { width: "100%", maxWidth: page.maxWidth, alignSelf: "center", flexDirection: "row", alignItems: "center" } as const;
  const bar = page.topBar ? (
    <View style={{ borderBottomWidth: size.border, borderBottomColor: col.line }}>
      <View style={[lane, { height: size.topBar, paddingHorizontal: space.pad }]}>
        <View style={{ flex: 1, alignItems: "flex-start" }}><TopBrand /></View>
        <View accessibilityRole="tablist" aria-label={FRAME.tabs} style={{ flexDirection: "row", gap: space.inset }}
          {...arrows(role, onRole)}>
          <Tab role="give" on={role === "give"} onPress={() => onRole("give")} />
          <Tab role="receive" on={role === "receive"} onPress={() => onRole("receive")} /></View>
        <View style={{ flexGrow: 1, flexBasis: 0, alignItems: "flex-end" }}>
          <Pressable testID="topbar-logout" accessibilityRole="button" onPress={onLogout}
            style={(state) => ({ height: size.control, paddingHorizontal: size.tabPadX, borderRadius: radius.round, justifyContent: "center",
              borderWidth: size.border, borderColor: col.line, backgroundColor: isHovered(state) ? col.iconFill : col.bg,
              opacity: state.pressed ? opacity.pressed : 1 })}>
            <Text style={[type.button, { color: col.muted }]}>{ROLE.logout}</Text></Pressable></View>
      </View>
    </View>
  ) : (
    <View style={[lane, { height: size.header, paddingHorizontal: space.gutter }]}>
      <View style={{ flex: 1 }}><IconBtn big glyph="menu" label={FRAME.menu} onPress={onMenu} testID="menu" /></View>
      <PhoneBrand />
      <View style={{ flexGrow: 1, flexBasis: 0 }} />
    </View>
  );
  return (
    // U19: flex 1 + minHeight 0 bound the frame to the window, so the list of a role scrolls in it (with flexGrow
    // only, the frame grew to the height of the whole list and the page could not scroll to older items)
    <View style={{ flex: 1, minHeight: 0 }}>
      {bar}
      <View nativeID="role-panel" {...(page.topBar ? { role: "tabpanel" as const } : {})} style={{ flex: 1, minHeight: 0 }}>{children}</View>
    </View>
  );
}
