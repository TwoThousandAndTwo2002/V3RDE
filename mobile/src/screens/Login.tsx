// The sign-in screen, written new for V3RDE (NEW_WORK_UI.md, UI 8; mobile/docs/style_guide.md "Sign-in").
// One button: Continue signs in. A 401 shows "No account found. Create one" (the same for an unknown email and
// a wrong password, so the screen tells nothing about which accounts exist). Phone: the button sits at the
// bottom. Wide screen: a centred card, which fades and rises away (`leaving`) when the next screen comes in.
import React, { useRef, useState, useEffect } from "react";
import { Animated, KeyboardAvoidingView, ScrollView, TextInput, View, useWindowDimensions } from "react-native";
import * as RN from "react-native";
import { ApiError, auth, type User } from "../api";
import { layoutFor, motion, radius, size, space, useColors } from "../theme";
import { Doodles as Wallpaper } from "../doodles";
import { SIGN_IN, cleanEmail, failure, formProblem } from "../signin";
import { ErrorText, Field, FieldButton, Muted, Pill, TextLink, Title } from "../ui";

export default function Login({ onSignedIn, leaving }: { onSignedIn: (u: User) => void; leaving?: boolean }) {
  const col = useColors();
  const { width } = useWindowDimensions();
  const wide = layoutFor(width).dialogs;
  const [email, setEmail] = useState<string>("");
  const [password, setPassword] = useState<string>("");
  const [visible, setVisible] = useState(false);
  const [problem, setProblem] = useState("");
  const [unknown, setUnknown] = useState(false);
  const [waiting, setWaiting] = useState(false);
  const busy = useRef(false); // one request at a time, also before the next render (Enter, "Create one")
  const passwordBox = useRef<React.ComponentRef<typeof TextInput>>(null);
  const [away] = useState(() => new Animated.Value(0));
  useEffect(() => Animated.timing(away, { toValue: leaving ? 1 : 0, duration: motion.handoffMs, useNativeDriver: RN.Platform.OS !== "web" }).start(), [leaving, away]);

  const submit = async (mode: "login" | "signup") => {
    if (busy.current) return;
    setProblem("");
    setUnknown(false);
    const wrong = formProblem(email, password);
    if (wrong) return setProblem(wrong);
    busy.current = true;
    setWaiting(true);
    try {
      const who = await auth[mode](cleanEmail(email), password);
      setPassword(""); // the password does not stay in memory after sign-in
      onSignedIn(who);
    } catch (e) {
      const said = failure(mode, e instanceof ApiError ? e.status : undefined, e instanceof Error ? e.message : String(e));
      setUnknown(said.offer);
      setProblem(said.text);
    } finally {
      busy.current = false;
      setWaiting(false);
    }
  };

  const fields = (
    <View>
      <Title>{SIGN_IN.title}</Title>
      <Muted style={{ marginTop: space.inset }}>{SIGN_IN.line}</Muted>
      <View style={{ marginTop: space.pad }}>
        <Field testID="email" label={SIGN_IN.email} value={email} onChangeText={setEmail} placeholder={SIGN_IN.emailHint}
          autoCapitalize="none" autoCorrect={false} autoComplete="email" textContentType="emailAddress" keyboardType="email-address"
          inputMode="email" returnKeyType="next" onSubmitEditing={() => passwordBox.current?.focus()} />
        <Field ref={passwordBox} testID="password" label={SIGN_IN.password} value={password} onChangeText={setPassword}
          secureTextEntry={!visible} autoCapitalize="none" autoCorrect={false} spellCheck={false}
          autoComplete={unknown ? "new-password" : "current-password"} textContentType={unknown ? "newPassword" : "password"}
          returnKeyType="go" submitBehavior="submit" blurOnSubmit={false} onSubmitEditing={() => submit("login")}
          right={<FieldButton title={visible ? SIGN_IN.hide : SIGN_IN.show} label={visible ? SIGN_IN.hideName : SIGN_IN.showName}
            onPress={() => setVisible((v) => !v)} />} />
        <Muted style={{ marginTop: space.inset, marginLeft: space.inset }}>{SIGN_IN.rule}</Muted>
        {problem ? <ErrorText>{problem}</ErrorText> : null}
        {unknown ? (
          <ErrorText>{SIGN_IN.noAccount + " "}<TextLink testID="create" onPress={() => submit("signup")}>{SIGN_IN.create}</TextLink></ErrorText>
        ) : null}
      </View>
    </View>
  );
  const go = <Pill testID="continue" title={SIGN_IN.go} busy={waiting} busyName={SIGN_IN.waiting} onPress={() => submit("login")} />;

  if (wide) {
    const fade = away.interpolate({ inputRange: [0, 1], outputRange: [1, 0] });
    const lift = away.interpolate({ inputRange: [0, 1], outputRange: [0, -motion.handoffLift] });
    return (
      <View style={{ flex: 1, backgroundColor: col.bg }} pointerEvents={leaving ? "none" : "auto"}>
        <Wallpaper />
        <ScrollView contentContainerStyle={{ flexGrow: 1, alignItems: "center", justifyContent: "center", padding: space.pad }}>
          <Animated.View style={{ width: "100%", maxWidth: size.dialogSmall, padding: space.pad, borderRadius: radius.sheet,
            borderWidth: size.border, borderColor: col.line, backgroundColor: col.card, opacity: fade, transform: [{ translateY: lift }] }}>
            {fields}
            <View style={{ marginTop: space.pad }}>{go}</View>
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
        {fields}</ScrollView>
      <View style={{ padding: space.gutter }}>{go}</View></KeyboardAvoidingView>
  );
}
