// V3RDE app (NEW_WORK_UI.md, UI 10 and 11): it loads the brand font, reads a reset link (web /reset#token=…, app
// v3rde://reset#token=…), asks the services if a session is open, and shows sign-in, reset, the role sheet and
// the chosen screen in its frame (☰ header or top bar). Phone: the role sheet rises over the sign-in screen right
// after sign-in. Wide screen: the sign-in card fades out first, then the role dialog rises over the doodles. The
// screens keep clear of the phone's status bar and home bar (safe-area insets).
import React, { useCallback, useEffect, useMemo, useRef, useState } from "react"; // hooks only, no class
import { StatusBar as SystemBar } from "expo-status-bar";
import { Linking, View, useWindowDimensions } from "react-native";
import { SafeAreaProvider as Insets, useSafeAreaInsets as useInsets } from "react-native-safe-area-context";
import * as RN from "react-native";
import { useFonts as useFontFiles } from "expo-font";
import { Poppins_400Regular as regularFile } from "@expo-google-fonts/poppins/400Regular";
import { Poppins_600SemiBold as semiboldFile } from "@expo-google-fonts/poppins/600SemiBold";
import { auth, type User as Account } from "./src/api";
import { font, layoutFor, motion, space, useColors } from "./src/theme";
import { Muted, Title, Toast } from "./src/ui";
import { Doodles as Wallpaper } from "./src/doodles";
import { resetToken } from "./src/signin";
import { FRAME, roleCard, type RoleName } from "./src/words";
import { SessionContext, type Session } from "./src/session";
import SignIn from "./src/screens/Login";
import { ForgotPassword, NewPassword } from "./src/screens/Reset";
import { RoleSheet } from "./src/screens/Role";
import { Frame } from "./src/screens/Frame";
import ReceiveScreen from "./src/screens/Receive";

/** Where the role sheet is: over the auth screen it came from (phone), over the doodles, or over the doodles
 *  after the wide-screen handoff (no dim). */
type Under = "auth" | "doodles" | "handoff";
type From = { from: "signin" | "newpass"; token?: string };
type Stage =
  | { kind: "start" } | { kind: "signin" } | { kind: "forgot" } | { kind: "newpass"; token: string }
  | ({ kind: "leaving"; who: Account } & From)
  | ({ kind: "role"; who: Account; under: Under } & Partial<From>)
  | { kind: "in"; who: Account; role: RoleName; menu: boolean };

/** The reset token of the link that opened the app ("" when none); on the web the token leaves the address bar. */
async function openingToken(): Promise<string> {
  if (RN.Platform.OS === "web") {
    const token = resetToken(window.location.pathname + window.location.search + window.location.hash);
    if (token) window.history.replaceState(null, "", "/");
    return token;
  }
  return resetToken(await Linking.getInitialURL());
}

/** The screen of a role. The Give screen comes in the next change; until then a title. */
function RoleScreen({ who, role }: { who: Account; role: RoleName }) {
  if (role === "receive") return <ReceiveScreen />;
  return (
    <View style={{ flex: 1, alignItems: "center", justifyContent: "center", padding: space.gutter, gap: space.gap }}>
      <Title center>{roleCard(role).title}</Title>
      <Muted center>{who.email}</Muted>
    </View>
  );
}

const Shell = () => {
  const [ready] = useFontFiles({ [font.regular]: regularFile, [font.semibold]: semiboldFile });
  const col = useColors();
  const inset = useInsets();
  const wide = layoutFor(useWindowDimensions().width).dialogs;
  const [stage, setStage] = useState<Stage>({ kind: "start" });
  const stageNow = useRef(stage);
  stageNow.current = stage;
  const [note, setNote] = useState("");
  const noteTimer = useRef<ReturnType<typeof setTimeout> | undefined>(undefined);
  const say = useCallback((text: string, ms = 2600) => {
    setNote(text);
    clearTimeout(noteTimer.current);
    noteTimer.current = setTimeout(() => setNote(""), ms);
  }, []);
  useEffect(() => () => clearTimeout(noteTimer.current), []);
  useEffect(function openStart() {
    // a link that comes while the app starts wins: the start answers change only the "start" stage
    const fromStart = (next: Stage) => setStage((now) => (now.kind === "start" ? next : now));
    openingToken().catch(() => "").then((token) => {
      if (token) return fromStart({ kind: "newpass", token });
      auth.me().then((who) => fromStart({ kind: "role", who, under: "doodles" })).catch(() => fromStart({ kind: "signin" }));
    });
    const sub = Linking.addEventListener("url", ({ url }) => {
      const token = resetToken(url);
      if (token) setStage({ kind: "newpass", token });
    });
    return () => sub.remove();
  }, []);
  // wide screens: after sign-in (or a new password) the card fades out, then the role dialog rises
  useEffect(function handOff() {
    if (stage.kind !== "leaving") return;
    const timer = setTimeout(() => setStage({ kind: "role", who: stage.who, under: "handoff" }), motion.handoffMs);
    return () => clearTimeout(timer);
  }, [stage]);
  const signedIn = (from: From["from"], token?: string) => (who: Account) => {
    RN.Keyboard.dismiss(); // the keyboard of the sign-in screen does not stay over the role sheet
    setStage(wide ? { kind: "leaving", who, from, token } : { kind: "role", who, under: "auth", from, token });
  };
  const logout = useCallback(() => {
    auth.logout().catch(() => undefined); // the services end the session; the app leaves at once
    setStage({ kind: "signin" });
  }, []);
  const who = stage.kind === "in" ? stage.who : null;
  const session = useMemo<Session | null>(() => who && {
    who,
    goTo: (role) => setStage((now) => (now.kind === "in" ? { ...now, role, menu: false } : now)),
    say,
    ended: () => {
      if (stageNow.current.kind !== "in") return; // a late 401 after Log out changes nothing
      setStage({ kind: "signin" });
      say(FRAME.expired);
    },
  }, [who, say]);
  const authFrom = stage.kind === "leaving" || (stage.kind === "role" && stage.under === "auth") ? stage.from : undefined;
  const under = stage.kind === "leaving" || stage.kind === "role"; // the auth screen stays, frozen
  const tokenOf = stage.kind === "newpass" ? stage.token : stage.kind === "leaving" || stage.kind === "role" ? stage.token ?? "" : "";
  const showDoodles = (stage.kind === "role" && stage.under !== "auth");
  return (
    <View style={{ flex: 1, backgroundColor: col.bg, paddingTop: inset.top, paddingBottom: inset.bottom,
      paddingLeft: inset.left, paddingRight: inset.right }}>
      {ready && (stage.kind === "signin" || authFrom === "signin") ? (
        <SignIn leaving={under} onSignedIn={signedIn("signin")} onForgot={() => setStage({ kind: "forgot" })} />
      ) : null}
      {ready && stage.kind === "forgot" ? <ForgotPassword onBack={() => setStage({ kind: "signin" })} /> : null}
      {ready && (stage.kind === "newpass" || authFrom === "newpass") ? (
        <NewPassword token={tokenOf} leaving={under} onSignedIn={signedIn("newpass", tokenOf)} onAgain={() => setStage({ kind: "forgot" })} />
      ) : null}
      {ready && showDoodles ? <Wallpaper /> : null}
      {ready && stage.kind === "in" && session ? (
        <SessionContext.Provider value={session}>
          <Frame role={stage.role} onRole={session.goTo} onMenu={() => setStage({ ...stage, menu: true })} onLogout={logout}>
            <RoleScreen who={stage.who} role={stage.role} /></Frame>
        </SessionContext.Provider>
      ) : null}
      {ready && stage.kind === "role" ? (
        <RoleSheet dim={stage.under !== "handoff"} onLogout={logout}
          onPick={(role) => setStage({ kind: "in", who: stage.who, role, menu: false })} />
      ) : null}
      {ready && stage.kind === "in" && stage.menu ? (
        <RoleSheet dim chosen={stage.role} onLogout={logout} onDismiss={() => setStage({ ...stage, menu: false })}
          onPick={(role) => setStage({ kind: "in", who: stage.who, role, menu: false })} />
      ) : null}
      <Toast text={note} />
      <SystemBar style={"auto"} />
    </View>
  );
};

const App = () => <Insets><Shell /></Insets>;
export default App;
