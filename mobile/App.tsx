// V3RDE app (NEW_WORK_UI.md, UI 9): it loads the brand font, reads a reset link (web /reset#token=…, app
// v3rde://reset#token=…), asks the services if a session is open, and shows sign-in, reset or the signed-in line.
// The next screens (role, give, receive) come in the next changes.
import React, { useEffect, useState } from "react";
import { StatusBar as SystemBar } from "expo-status-bar";
import { Linking, View } from "react-native";
import * as RN from "react-native";
import { useFonts as useFontFiles } from "expo-font";
import { Poppins_400Regular as regularFile } from "@expo-google-fonts/poppins/400Regular";
import { Poppins_600SemiBold as semiboldFile } from "@expo-google-fonts/poppins/600SemiBold";
import { auth, type User as Account } from "./src/api";
import { font, motion, useColors } from "./src/theme";
import { Muted, Title } from "./src/ui";
import { resetToken } from "./src/signin";
import SignIn from "./src/screens/Login";
import { ForgotPassword, NewPassword } from "./src/screens/Reset";

type Stage =
  | { kind: "start" } | { kind: "signin" } | { kind: "forgot" } | { kind: "newpass"; token: string }
  | { kind: "leaving"; who: Account; from: "signin" | "newpass"; token?: string } | { kind: "in"; who: Account };

/** The reset token of the link that opened the app ("" when none); on the web the token leaves the address bar. */
async function openingToken(): Promise<string> {
  if (RN.Platform.OS === "web") {
    const token = resetToken(window.location.pathname + window.location.search + window.location.hash);
    if (token) window.history.replaceState(null, "", "/");
    return token;
  }
  return resetToken(await Linking.getInitialURL());
}

const Shell = () => {
  const [ready] = useFontFiles({ [font.regular]: regularFile, [font.semibold]: semiboldFile });
  const col = useColors();
  const [stage, setStage] = useState<Stage>({ kind: "start" });
  useEffect(function openStart() {
    // a link that comes while the app starts wins: the start answers change only the "start" stage
    const fromStart = (next: Stage) => setStage((now) => (now.kind === "start" ? next : now));
    openingToken().catch(() => "").then((token) => {
      if (token) return fromStart({ kind: "newpass", token });
      auth.me().then((who) => fromStart({ kind: "in", who })).catch(() => fromStart({ kind: "signin" }));
    });
    const sub = Linking.addEventListener("url", ({ url }) => {
      const token = resetToken(url);
      if (token) setStage({ kind: "newpass", token });
    });
    return () => sub.remove();
  }, []);
  // after sign-in (or a new password) the screen fades out (wide screens: the card lifts), then the next screen comes
  useEffect(function handOff() {
    if (stage.kind !== "leaving") return;
    const timer = setTimeout(() => setStage({ kind: "in", who: stage.who }), motion.handoffMs);
    return () => clearTimeout(timer);
  }, [stage]);
  const signedIn = (from: "signin" | "newpass", token?: string) => (who: Account) => setStage({ kind: "leaving", who, from, token });
  const leaving = stage.kind === "leaving";
  return (
    <View style={{ flex: 1, backgroundColor: col.bg }}>
      {ready && (stage.kind === "signin" || (leaving && stage.from === "signin")) ? (
        <SignIn leaving={leaving} onSignedIn={signedIn("signin")} onForgot={() => setStage({ kind: "forgot" })} />
      ) : null}
      {ready && stage.kind === "forgot" ? <ForgotPassword onBack={() => setStage({ kind: "signin" })} /> : null}
      {ready && (stage.kind === "newpass" || (leaving && stage.from === "newpass")) ? (
        <NewPassword token={stage.kind === "newpass" ? stage.token : stage.kind === "leaving" ? stage.token ?? "" : ""} leaving={leaving}
          onSignedIn={signedIn("newpass", stage.kind === "newpass" ? stage.token : undefined)} onAgain={() => setStage({ kind: "forgot" })} />
      ) : null}
      {ready && stage.kind === "in" ? (
        <View style={{ flex: 1, alignItems: "center", justifyContent: "center" }}>
          <Title center>V3RDE</Title>
          <Muted center>{stage.who.email}</Muted>
        </View>
      ) : null}
      <SystemBar style={"auto"} />
    </View>
  );
};
export default Shell;
