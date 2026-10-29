// Reset password by email link, written new for V3RDE (NEW_WORK_UI.md, UI 9; mobile/docs/style_guide.md "Reset").
// Without a token: the email field and "Send link", then "Check your email" (read out at once; the same answer for any email, so the
// screen tells nothing about which accounts exist). With the token of a link: a new password, then the user is
// signed in. A link that is unknown, used or too old shows "This link has expired or was used." with "Send a new link".
import React, { useRef, useState } from "react";
import { View } from "react-native";
import { ApiError, auth, type User } from "../api";
import { space } from "../theme";
import { EMAIL_FORM, EXPIRED_ERROR, MIN_PASSWORD, RESET, SIGN_IN, cleanEmail } from "../signin";
import { ErrorText, Field, FieldButton, Muted, Pill, TextLink, Title } from "../ui";
import { AuthFrame } from "./AuthFrame";

export function ForgotPassword({ onBack, startEmail = "" }: { onBack: () => void; startEmail?: string }) {
  const [email, setEmail] = useState(startEmail);
  const [sentTo, setSentTo] = useState("");
  const [problem, setProblem] = useState("");
  const [waiting, setWaiting] = useState(false);
  const busy = useRef(false);
  const send = async () => {
    if (busy.current) return;
    setProblem("");
    const e = cleanEmail(email);
    if (!EMAIL_FORM.test(e)) return setProblem(SIGN_IN.badEmail);
    busy.current = true;
    setWaiting(true);
    try {
      await auth.resetRequest(e);
      setSentTo(e);
    } catch (err) {
      setProblem(err instanceof Error ? err.message : String(err));
    } finally {
      busy.current = false;
      setWaiting(false);
    }
  };
  if (sentTo) {
    return (
      <AuthFrame action={<Pill testID="back" title={RESET.back} onPress={onBack} />}>
        <View accessibilityLiveRegion="polite" aria-live="polite">
          <Title>{RESET.sentTitle}</Title>
          <Muted style={{ marginTop: space.inset }}>{RESET.sent(sentTo)}</Muted></View>
      </AuthFrame>
    );
  }
  return (
    <AuthFrame action={<Pill testID="send" title={RESET.send} busy={waiting} busyName={RESET.sending} onPress={send} />}>
      <Title>{RESET.askTitle}</Title>
      <Muted style={{ marginTop: space.inset }}>{RESET.askLine}</Muted>
      <View style={{ marginTop: space.pad }}>
        <Field testID="email" label={SIGN_IN.email} value={email} onChangeText={setEmail} placeholder={SIGN_IN.emailHint}
          autoCapitalize="none" autoCorrect={false} autoComplete="email" textContentType="emailAddress" keyboardType="email-address"
          inputMode="email" returnKeyType="send" submitBehavior="submit" blurOnSubmit={false} onSubmitEditing={send} />
        {problem ? <ErrorText>{problem}</ErrorText> : null}
        <View style={{ marginTop: space.gutter, marginLeft: space.inset, flexDirection: "row" }}>
          <TextLink testID="backlink" onPress={() => busy.current || onBack()}>{RESET.back}</TextLink></View>
      </View>
    </AuthFrame>
  );
}

export function NewPassword({ token, onSignedIn, onAgain, leaving }: {
  token: string; onSignedIn: (u: User) => void; onAgain: () => void; leaving?: boolean;
}) {
  const [fresh, setFresh] = useState("");
  const [visible, setVisible] = useState(false);
  const [problem, setProblem] = useState("");
  const [expired, setExpired] = useState(false);
  const [waiting, setWaiting] = useState(false);
  const busy = useRef(false);
  const save = async () => {
    if (busy.current || leaving) return; // not again from under the role sheet
    setProblem("");
    setExpired(false);
    if (fresh.length < MIN_PASSWORD) return setProblem(RESET.short);
    busy.current = true;
    setWaiting(true);
    try {
      const who = await auth.resetConfirm(token, fresh);
      setFresh(""); // the password does not stay in memory
      onSignedIn(who);
    } catch (err) {
      if (err instanceof ApiError && err.status === 400 && err.message === EXPIRED_ERROR) setExpired(true);
      else setProblem(err instanceof Error ? err.message : String(err));
    } finally {
      busy.current = false;
      setWaiting(false);
    }
  };
  return (
    <AuthFrame leaving={leaving} action={<Pill testID="save" title={RESET.save} busy={waiting} busyName={RESET.saving} onPress={save} />}>
      <Title>{RESET.newTitle}</Title>
      <Muted style={{ marginTop: space.inset }}>{RESET.newLine}</Muted>
      <View style={{ marginTop: space.pad }}>
        <Field testID="password" label={RESET.newPassword} value={fresh} onChangeText={setFresh} secureTextEntry={!visible} autoFocus
          autoCapitalize="none" autoCorrect={false} spellCheck={false} autoComplete="new-password" textContentType="newPassword"
          returnKeyType="go" submitBehavior="submit" blurOnSubmit={false} onSubmitEditing={save}
          right={<FieldButton title={visible ? SIGN_IN.hide : SIGN_IN.show} label={visible ? SIGN_IN.hideName : SIGN_IN.showName}
            onPress={() => setVisible((v) => !v)} />} />
        {problem ? <ErrorText>{problem}</ErrorText> : null}
        {expired ? (
          <ErrorText>{RESET.expired + " "}<TextLink testID="again" onPress={onAgain}>{RESET.again}</TextLink></ErrorText>
        ) : null}
      </View>
    </AuthFrame>
  );
}
