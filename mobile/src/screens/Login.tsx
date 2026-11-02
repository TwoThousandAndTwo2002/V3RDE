// The sign-in screen, written new for V3RDE (NEW_WORK_UI.md, UI 8; mobile/docs/style_guide.md "Sign-in").
// One button: Continue signs in. X49w (T9): an unknown email shows "No account uses this email." and the link "Create an
// account"; a wrong password shows "Wrong password." and the link "Forgot password?". Before a try, only "About" shows. Phone: the button sits at the
// bottom. Wide screen: a centred card, which fades and rises away (`leaving`) when the next screen comes in
// (AuthFrame). "Forgot password?" opens the reset screen (UI 9). "About" (#78), at the right of the same row, opens
// the About page: on the web in the same tab (the page is on the same site), on a phone in the browser.
import React, { useRef, useState } from "react";
import { Linking, Platform, TextInput, View } from "react-native";
import { ApiError, auth, type User } from "../api";
import { space } from "../theme";
import { AuthFrame } from "./AuthFrame";
import { ABOUT_PATH, ABOUT_URL, RESET, SIGN_IN, cleanEmail, failure, formProblem, type Offer } from "../signin";
import { ErrorText, Field, FieldButton, Muted, Pill, TextLink, Title } from "../ui";
import { LanguageButton } from "../language";
import { SignInBrand } from "../brand";

/** Opens the About page: the same tab on the web, the browser on a phone. */
function openAbout() {
  if (Platform.OS === "web") window.location.assign(ABOUT_PATH);
  else Linking.openURL(ABOUT_URL).catch(() => {});
}

export default function Login({ onSignedIn, onForgot, leaving }: { onSignedIn: (u: User) => void; onForgot: () => void; leaving?: boolean }) {
  const [email, setEmail] = useState<string>("");
  const [password, setPassword] = useState<string>("");
  const [visible, setVisible] = useState(false);
  const [problem, setProblem] = useState("");
  const [offer, setOffer] = useState<Offer>(""); // X49w: the one link after a failed sign-in
  const unknown = offer === "create";
  const [waiting, setWaiting] = useState(false);
  const busy = useRef(false); // one request at a time, also before the next render (Enter, "Create one")
  const passwordBox = useRef<React.ComponentRef<typeof TextInput>>(null);

  const submit = async (mode: "login" | "signup") => {
    if (busy.current || leaving) return; // not again from under the role sheet
    setProblem("");
    setOffer("");
    const wrong = formProblem(email, password, mode);
    if (wrong) return setProblem(wrong);
    busy.current = true;
    setWaiting(true);
    try {
      const who = await auth[mode](cleanEmail(email), password);
      setPassword(""); // the password does not stay in memory after sign-in
      onSignedIn(who);
    } catch (e) {
      const said = failure(mode, e instanceof ApiError ? e.status : undefined, e instanceof Error ? e.message : String(e),
        e instanceof ApiError ? e.body?.code : undefined);
      setOffer(said.offer);
      setProblem(said.text);
    } finally {
      busy.current = false;
      setWaiting(false);
    }
  };

  const fields = (
    <View>
      <SignInBrand />
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
        <Muted style={{ marginTop: space.inset, marginStart: space.inset }}>{SIGN_IN.rule}</Muted>
        {problem ? <ErrorText style={{ marginStart: space.inset }}>{problem}</ErrorText> : null}
        {/* X49w: one row on the same left edge as the notes: the one link that helps (or none), and About at the right */}
        <View style={{ marginTop: space.gap, marginHorizontal: space.inset, flexDirection: "row", justifyContent: "space-between" }}>
          {offer === "create" ? <TextLink testID="create" onPress={() => submit("signup")}>{SIGN_IN.create}</TextLink>
            : offer === "forgot" ? <TextLink testID="forgot" onPress={() => busy.current || onForgot()}>{RESET.forgot}</TextLink>
            : <View />}
          {/* X49x: the language choice, then About, at the end of the row */}
          <View style={{ flexDirection: "row", alignItems: "center", gap: space.group }}>
            <LanguageButton />
            <TextLink testID="about" onPress={openAbout}>{SIGN_IN.about}</TextLink></View></View>
      </View>
    </View>
  );
  const go = <Pill testID="continue" title={SIGN_IN.go} busy={waiting} busyName={SIGN_IN.waiting} onPress={() => submit("login")} />;

  return <AuthFrame action={go} leaving={leaving}>{fields}</AuthFrame>;
}
