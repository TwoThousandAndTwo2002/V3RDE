// V3RDE app (NEW_WORK_UI.md, UI 8): it loads the brand font, asks the services if a session is open, and shows
// the sign-in screen when it is not. After sign-in, a short line stands in for the next screens (role, give,
// receive), which come in the next changes.
import React, { useEffect, useState } from "react";
import { StatusBar as SystemBar } from "expo-status-bar";
import { View } from "react-native";
import { useFonts as useFontFiles } from "expo-font";
import { Poppins_400Regular as regularFile } from "@expo-google-fonts/poppins/400Regular";
import { Poppins_600SemiBold as semiboldFile } from "@expo-google-fonts/poppins/600SemiBold";
import { auth, type User as Account } from "./src/api";
import { font, motion, useColors } from "./src/theme";
import { Muted, Title } from "./src/ui";
import SignIn from "./src/screens/Login";

type Stage = { kind: "start" } | { kind: "signin" } | { kind: "leaving"; who: Account } | { kind: "in"; who: Account };

const Shell = () => {
  const [ready] = useFontFiles({ [font.regular]: regularFile, [font.semibold]: semiboldFile });
  const col = useColors();
  const [stage, setStage] = useState<Stage>({ kind: "start" });
  useEffect(() => { auth.me().then((who) => setStage({ kind: "in", who })).catch(() => setStage({ kind: "signin" })); }, []);
  // after sign-in the sign-in screen fades out (wide screens: the card lifts), then the next screen comes
  useEffect(function handOff() {
    if (stage.kind !== "leaving") return;
    const timer = setTimeout(() => setStage({ kind: "in", who: stage.who }), motion.handoffMs);
    return () => clearTimeout(timer);
  }, [stage]);
  return (
    <View style={{ flex: 1, backgroundColor: col.bg }}>
      {ready && (stage.kind === "signin" || stage.kind === "leaving") ? (
        <SignIn leaving={stage.kind === "leaving"} onSignedIn={(who) => setStage({ kind: "leaving", who })} />
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
