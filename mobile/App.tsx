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
import { font, layoutFor, motion, useColors } from "./src/theme";
import { Toast } from "./src/ui";
import { Doodles as Wallpaper } from "./src/doodles";
import { claimListing, resetToken } from "./src/signin";
import { FRAME, type RoleName } from "./src/words";
import { SessionContext, type Session } from "./src/session";
import SignIn from "./src/screens/Login";
import { ForgotPassword, NewPassword } from "./src/screens/Reset";
import { RoleSheet } from "./src/screens/Role";
import { Frame } from "./src/screens/Frame";
import ReceiveScreen from "./src/screens/Receive";
import { SOURCE, loadLanguages, saveLanguage, savedLanguage, startLanguage, switchLanguage, type Language } from "./src/lang";
import { LanguageContext, LanguageSheet, type LanguageChoice } from "./src/language";
import GiveScreen from "./src/screens/Give";

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

/** N4: the listing of the claim link that opened the app ("" when none); on the web the link leaves the address bar. */
async function openingClaim(): Promise<string> {
  if (RN.Platform.OS === "web") {
    const id = claimListing(window.location.pathname + window.location.search);
    if (id) window.history.replaceState(null, "", "/");
    return id;
  }
  return claimListing(await Linking.getInitialURL());
}

/** The screen of a role. */
const RoleScreen = ({ role }: { role: RoleName }) => (role === "receive" ? <ReceiveScreen /> : <GiveScreen />);

/** X49x: the most time the first screen waits for the words of the saved or browser language (then English). */
const LANGUAGE_WAIT_MS = 1500;

const Shell = () => {
  const [fontsReady] = useFontFiles({ [font.regular]: regularFile, [font.semibold]: semiboldFile });
  // X49x: the language: the published list, the one now, the sheet; the first screen waits for its words
  const [languages, setLanguages] = useState<Language[]>([SOURCE]);
  const [lang, setLang] = useState<Language>(SOURCE);
  const [langReady, setLangReady] = useState(false);
  const [langSheet, setLangSheet] = useState(false);
  const ready = fontsReady && langReady;
  useEffect(function firstLanguage() {
    let on = true;
    const late = setTimeout(() => on && setLangReady(true), LANGUAGE_WAIT_MS);
    loadLanguages().then(async (list) => {
      if (!on) return;
      setLanguages(list);
      const browser = typeof navigator !== "undefined" ? navigator.languages ?? [navigator.language] : [];
      const code = startLanguage(savedLanguage(), browser, list);
      const l = await switchLanguage(list.find((x) => x.code === code) ?? SOURCE);
      if (on && l) setLang(l); // null: the user picked a language while this one loaded
    }).finally(() => { clearTimeout(late); if (on) setLangReady(true); });
    return () => { on = false; clearTimeout(late); };
  }, []);
  const pickLanguage = useCallback((code: string) => {
    setLangSheet(false);
    const l = languages.find((x) => x.code === code) ?? SOURCE;
    saveLanguage(l.code);
    switchLanguage(l).then((now) => { if (now) setLang(now); }); // every screen renders again with the new words
  }, [languages]);
  const choice = useMemo<LanguageChoice>(() => ({ languages, now: lang, open: () => setLangSheet(true) }), [languages, lang]);
  const col = useColors();
  const inset = useInsets();
  const wide = layoutFor(useWindowDimensions().width).dialogs;
  const [stage, setStage] = useState<Stage>({ kind: "start" });
  const stageNow = useRef(stage);
  stageNow.current = stage;
  const [note, setNote] = useState("");
  // N4 (T9): a claim link (the Facebook post) skips the role sheet once: the next sign-in opens Receive
  const claim = useRef("");
  const afterSignIn = (who: Account, role: Stage): Stage => {
    if (!claim.current) return role;
    claim.current = "";
    return { kind: "in", who, role: "receive", menu: false };
  };
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
    // N4 review: a claim link that came while the app starts stays (only a found claim is written)
    openingClaim().catch(() => "").then((id) => { if (id) claim.current = id; return openingToken(); }).catch(() => "").then((token) => {
      if (token) return fromStart({ kind: "newpass", token });
      auth.me().then((who) => fromStart(afterSignIn(who, { kind: "role", who, under: "doodles" }))).catch(() => fromStart({ kind: "signin" }));
    });
    const sub = Linking.addEventListener("url", ({ url }) => {
      const token = resetToken(url);
      if (token) setStage({ kind: "newpass", token });
      const id = claimListing(url); // N4: a claim link while the app runs: Receive now, or after the sign-in
      if (id) { // N4 review: signed in (the role sheet, the handoff, a screen): Receive now; else after the sign-in
        const now = stageNow.current;
        if (now.kind === "in") setStage({ ...now, role: "receive", menu: false });
        else if (now.kind === "role" || now.kind === "leaving") setStage({ kind: "in", who: now.who, role: "receive", menu: false });
        else claim.current = id;
      }
    });
    return () => sub.remove();
  }, []);
  // wide screens: after sign-in (or a new password) the card fades out, then the role dialog rises
  useEffect(function handOff() {
    if (stage.kind !== "leaving") return;
    const timer = setTimeout(() => setStage(afterSignIn(stage.who, { kind: "role", who: stage.who, under: "handoff" })), motion.handoffMs);
    return () => clearTimeout(timer);
  }, [stage]);
  const signedIn = (from: From["from"], token?: string) => (who: Account) => {
    RN.Keyboard.dismiss(); // the keyboard of the sign-in screen does not stay over the role sheet
    setStage(wide ? { kind: "leaving", who, from, token } : afterSignIn(who, { kind: "role", who, under: "auth", from, token }));
  };
  const logout = useCallback(() => {
    claim.current = ""; // N4 review: a claim never waits for the next account
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
    <LanguageContext.Provider value={choice}>
    <View {...({ dir: lang.dir, lang: lang.code } as object)} style={{ flex: 1, backgroundColor: col.bg, paddingTop: inset.top, paddingBottom: inset.bottom,
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
            <RoleScreen role={stage.role} /></Frame>
        </SessionContext.Provider>
      ) : null}
      {/* X49x review: the language sheet shows alone (one focus trap); the role sheet comes back after it */}
      {ready && stage.kind === "role" && !langSheet ? (
        <RoleSheet dim={stage.under !== "handoff"} onLogout={logout}
          onPick={(role) => setStage({ kind: "in", who: stage.who, role, menu: false })} />
      ) : null}
      {ready && stage.kind === "in" && stage.menu && !langSheet ? (
        <RoleSheet dim chosen={stage.role} onLogout={logout} onDismiss={() => setStage({ ...stage, menu: false })}
          onPick={(role) => setStage({ kind: "in", who: stage.who, role, menu: false })} />
      ) : null}
      {ready && langSheet ? <LanguageSheet onPick={pickLanguage} onDismiss={() => setLangSheet(false)} /> : null}
      <Toast text={note} />
      <SystemBar style={"auto"} />
    </View>
    </LanguageContext.Provider>
  );
};

const App = () => <Insets><Shell /></Insets>;
export default App;
