// Voice changes on the review, written new for V3RDE (NEW_WORK_UI.md, UI 17; mobile/docs/style_guide.md "Voice").
// Tap the microphone, say for example "six trays of pasta, the salad is gone, pick up at the back door", tap again
// (or the clip stops by itself before the service limit). The voice service gives a proposal; the app shows one
// line for each change and changes nothing until "Apply changes". Nothing posts until Post.
import { useEffect, useRef, useState } from "react";
import { ActivityIndicator, ScrollView, View } from "react-native";
import * as RN from "react-native";
import { ApiError, voice } from "../api";
import { deviceLocale, type ItemForm } from "../logic";
import { applyProposal, changesSomething, proposalLines, proposalOf, voiceError, voiceItems, type Proposal } from "../voice";
import { VoiceError, useVoiceRecorder, type VoiceClip } from "../voicerec";
import { ClipBusy } from "../clipsession";
import { radius, size, space, useColors } from "../theme";
import { Body, ErrorText, IconBtn, Muted, Pill, Row } from "../ui";
import { useSession } from "../session";
import { VOICE } from "../words";

type Talk = { kind: "idle" } | { kind: "sending" } | { kind: "proposal"; p: Proposal } | { kind: "error"; text: string };

export function VoiceBar({ items, pickup, onApply, onOpen }: {
  items: ItemForm[]; pickup: string; onApply: (items: ItemForm[], pickup: string) => void;
  /** True while a clip is out or a proposal waits: Post waits for it. */
  onOpen: (open: boolean) => void;
}) {
  const col = useColors();
  const { ended } = useSession();
  const [talk, setTalk] = useState<Talk>({ kind: "idle" });
  const sent = useRef(false); // one clip goes one time (the automatic stop and the Stop tap give the same clip)
  const live = useRef(true); // false after Start over or the end of the flow: a late answer changes nothing
  useEffect(() => () => { live.current = false; onOpen(false); }, []); // eslint-disable-line react-hooks/exhaustive-deps -- at the end
  const show = (t: Talk) => {
    if (!live.current) return;
    setTalk(t);
    onOpen(t.kind === "sending" || t.kind === "proposal");
  };
  const send = async (clip: VoiceClip) => {
    if (sent.current) return;
    sent.current = true;
    show({ kind: "sending" });
    try {
      const p = proposalOf(await voice(clip, voiceItems(items, VOICE.unnamed), pickup, deviceLocale()));
      if (live.current && RN.Platform.OS === "ios") RN.AccessibilityInfo.announceForAccessibility(proposalLines(items, pickup, p, VOICE).join(". "));
      show({ kind: "proposal", p });
    } catch (err) {
      if (!live.current) return;
      if (err instanceof ApiError && err.status === 401) return ended();
      const code = err instanceof ApiError && typeof err.body?.code === "string" ? err.body.code : "";
      show({ kind: "error", text: voiceError(err instanceof ApiError ? err.status : 0, code, VOICE) });
    }
  };
  const rec = useVoiceRecorder({ onAutoStop: send });
  const tap = async () => {
    if (talk.kind === "sending") return;
    if (rec.recording) {
      try { await send(await rec.stop()); } catch { show({ kind: "error", text: VOICE.failed }); }
      return;
    }
    sent.current = false;
    show({ kind: "idle" });
    try {
      await rec.start();
    } catch (err) {
      if (err instanceof ClipBusy) return; // the clip of the automatic stop is still on its way
      show({ kind: "error", text: err instanceof VoiceError && err.code === "denied" ? VOICE.micOff : VOICE.failed });
    }
  };
  let box: React.ReactNode;
  if (talk.kind === "proposal") {
    const lines = proposalLines(items, pickup, talk.p, VOICE);
    const useful = changesSomething(items, pickup, talk.p);
    const done = () => show({ kind: "idle" });
    box = (
      <View testID="voice-proposal" style={{ padding: space.group, gap: space.hair, borderRadius: radius.card, borderWidth: size.border,
        borderColor: col.accent, backgroundColor: col.card }}>
        <ScrollView style={{ maxHeight: size.proposalMax }}>
          {talk.p.transcript ? <Muted lines={2}>{VOICE.heard(talk.p.transcript)}</Muted> : null}
          {lines.map((l, k) => <Body key={k}>{"• " + l}</Body>)}</ScrollView>
        <Row style={{ marginTop: space.gap }}>
          {useful ? <Pill testID="voice-keep" kind="secondary" title={VOICE.keep} onPress={done} grow /> : null}
          <Pill testID="voice-apply" title={useful ? VOICE.apply : VOICE.ok} grow onPress={() => {
            if (useful) { const after = applyProposal(items, pickup, talk.p); onApply(after.items, after.pickup); }
            done();
          }} />
        </Row>
      </View>
    );
  } else {
    const line = rec.recording ? VOICE.listening : talk.kind === "sending" ? VOICE.working : VOICE.say;
    box = (
      <>
        <Row>
          <IconBtn testID="mic" big glyph={rec.recording ? "stop" : "mic"} label={rec.recording ? VOICE.stop : VOICE.start} onPress={tap} />
          {talk.kind === "sending" ? <ActivityIndicator color={col.accent} /> : null}
          <Muted>{line}</Muted>
        </Row>
        {talk.kind === "error" ? <ErrorText>{talk.text}</ErrorText> : null}
      </>
    );
  }
  // the live region is always there, so a screen reader reads each new line and the proposal
  return <View accessibilityLiveRegion="polite" aria-live="polite" style={{ marginTop: space.gap }}>{box}</View>;
}
