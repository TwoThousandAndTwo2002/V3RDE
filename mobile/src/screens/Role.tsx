// The role sheet, written new for V3RDE (NEW_WORK_UI.md, UI 10; mobile/docs/style_guide.md "Role sheet").
// Give or Receive, and Log out. It rises over the screen on view: the sign-in screen right after sign-in on a
// phone, the doodles at start or after the wide-screen handoff, or a Give or Receive screen from ☰.
import { Pressable, Text, View } from "react-native";
import { opacity, radius, size, space, type, useColors } from "../theme";
import { GlyphIcon, Muted, Pill, Popup, Rise, Title } from "../ui";
import { ROLE, roleCard, type RoleName } from "../words";
import { LanguageButton } from "../language";

/** True while the mouse is on the card (react-native-web adds `hovered` to the Pressable state). */
const isHovered = (state: object) => (state as { hovered?: boolean }).hovered === true;

function RoleCard({ role, chosen, onPress }: { role: RoleName; chosen: boolean; onPress: () => void }) {
  const col = useColors();
  const words = roleCard(role, chosen);
  return (
    <Pressable testID={"role-" + role} accessibilityRole="button" accessibilityLabel={words.spoken} accessibilityState={{ selected: chosen }}
      onPress={onPress}
      style={(state) => ({ flexDirection: "row", alignItems: "center", gap: space.group, padding: space.group, borderRadius: radius.card,
        borderWidth: size.border, borderColor: chosen || isHovered(state) ? col.accent : col.line, backgroundColor: col.card,
        opacity: state.pressed ? opacity.pressed : 1 })}>
      <View style={{ width: size.iconBig, height: size.iconBig, borderRadius: radius.round, backgroundColor: col.accentSoft,
        alignItems: "center", justifyContent: "center" }}>
        <GlyphIcon name={role} color={col.accent} /></View>
      <View style={{ flex: 1, minWidth: 0 }}>
        <Text style={[type.button, { color: col.fg }]}>{words.title}</Text>
        <Muted>{words.line}</Muted></View></Pressable>
  );
}

export function RoleSheet({ dim, chosen, onPick, onLogout, onDismiss }: {
  dim: boolean; chosen?: RoleName; onPick: (r: RoleName) => void; onLogout: () => void; onDismiss?: () => void;
}) {
  return (
    <Popup testID="role-sheet" dim={dim} onDismiss={onDismiss} dismissName={ROLE.close}>
      <Rise i={0}><Title>{ROLE.title}</Title></Rise>
      <Rise i={1}><View style={{ marginTop: space.gutter }}>
        <RoleCard role="give" chosen={chosen === "give"} onPress={() => onPick("give")} /></View></Rise>
      <Rise i={2}><View style={{ marginTop: space.item }}>
        <RoleCard role="receive" chosen={chosen === "receive"} onPress={() => onPick("receive")} /></View></Rise>
      {/* X49x: the language choice, above Log out; X65: an oval pill, as Log out */}
      <Rise i={3}><View style={{ marginTop: space.gutter }}><LanguageButton testID="menu-language" look="pill" /></View></Rise>
      <Rise i={4}><View style={{ marginTop: space.gutter }}>
        <Pill testID="logout" kind="secondary" title={ROLE.logout} onPress={onLogout} /></View></Rise>
    </Popup>
  );
}
