// The chat bar of the review, written new for V3RDE (NEW_WORK_UI.md, UI 17; X48s, the approved "V3RDE Chat Bar
// Mockup"; mobile/docs/style_guide.md "Chat bar"). Tap the microphone and talk, or type and tap Send. The voice
// service turns the clip into text (Muse Voice Transcribe) and the text into changes (Muse Spark). The changes go
// into the list at once; the chat under the list keeps what was said or typed and what changed, with Undo on the
// last change only. Nothing posts until Post. The review clears the chat after a post.
import { useEffect, useRef, useState } from "react";
import { ActivityIndicator, View } from "react-native";
import * as RN from "react-native";
import { ApiError, voice } from "../api";
import { deviceLocale, type ItemForm } from "../logic";
import { canUndo, chatTurn, keepOne, liveMarks, proposalOf, undoTo, voiceError, voiceItems, type How, type Turn } from "../voice";
import { VoiceError, useVoiceRecorder, type VoiceClip } from "../voicerec";
import { ClipBusy } from "../clipsession";
import { VOICE_MAX_MS } from "../wav";
import { radius, size, space, useColors } from "../theme";
import { Body, ErrorText, Field, FieldButton, IconBtn, Muted, Progress, Row, TextLink } from "../ui";
import { useSession } from "../session";
import { VOICE } from "../words";

/** One message: the user's on the icon-button fill, the app's on a card with a line. */
function Msg({ you, children }: { you: boolean; children: React.ReactNode }) {
  const col = useColors();
  return (
    <View style={{ alignSelf: you ? "flex-end" : "flex-start", maxWidth: "88%", padding: space.item, gap: space.hair, borderRadius: radius.card,
      backgroundColor: you ? col.iconFill : col.card, borderWidth: you ? 0 : size.border, borderColor: col.line }}>{children}</View>
  );
}

export type Chat = {
  thread: React.ReactNode; bar: React.ReactNode;
  /** The card marks ("Changed in the chat: …") of the last change, while the list is as it left it. */
  marks: Record<number, string>; pickupMarked: boolean;
  /** Clears the chat (after a post). */
  clear: () => void;
};

export function useChat({ items, pickup, onApply, onOpen }: {
  items: ItemForm[]; pickup: string; onApply: (items: ItemForm[], pickup: string) => void;
  /** True while a line is out: Post waits for it. */
  onOpen: (open: boolean) => void;
}): Chat {
  const col = useColors();
  const { ended } = useSession();
  const [turns, setTurns] = useState<Turn[]>([]);
  const [sending, setSending] = useState(false);
  const [error, setError] = useState("");
  const [text, setText] = useState("");
  const [since, setSince] = useState(0); // when the clip started (for the time and the bar)
  const [starting, setStarting] = useState(false); // the microphone is opening (a permission prompt may wait)
  const [now, setNow] = useState(0);
  const sent = useRef(false); // one clip goes one time (the automatic stop and the Stop tap give the same clip)
  const out = useRef(false); // X48s review: one line at a time (a fast double Send sends one line)
  const live = useRef(true); // false after Start over or the end of the flow: a late answer changes nothing
  const list = useRef({ items, pickup });
  list.current = { items, pickup };
  useEffect(() => () => { live.current = false; onOpen(false); }, []); // eslint-disable-line react-hooks/exhaustive-deps -- at the end
  const busy = (b: boolean) => { if (live.current) setSending(b); };

  const ask = async (how: How, said: VoiceClip | { text: string }) => {
    if (out.current) return;
    out.current = true;
    setError("");
    busy(true);
    const { items: at, pickup: place } = list.current;
    try {
      const answer = proposalOf(await voice(said, voiceItems(at, VOICE.unnamed), place, deviceLocale()));
      if (!live.current) return;
      // X48s review: the answer goes onto the list as it is NOW (a hand edit while the line was out stays)
      const { items: nowItems, pickup: nowPickup } = list.current;
      const p = keepOne(nowItems, answer, VOICE.notAll);
      const t = chatTurn(nowItems, nowPickup, p, how, how === "typed" ? (said as { text: string }).text : p.transcript, VOICE,
        { title: VOICE.markTitle, servings: VOICE.markServings, mark: VOICE.mark });
      if (t.count > 0) onApply(t.after.items, t.after.pickup);
      setTurns((all) => [...all, t]);
      if (RN.Platform.OS === "ios") RN.AccessibilityInfo.announceForAccessibility([VOICE.made(t.count), ...t.lines].join(". "));
      if (how === "typed") setText("");
    } catch (err) {
      if (!live.current) return;
      if (err instanceof ApiError && err.status === 401) return ended();
      const code = err instanceof ApiError && typeof err.body?.code === "string" ? err.body.code : "";
      setError(voiceError(err instanceof ApiError ? err.status : 0, code, VOICE));
    } finally {
      out.current = false;
      busy(false);
    }
  };
  const send = async (clip: VoiceClip) => {
    if (sent.current) return;
    sent.current = true;
    await ask("said", clip);
  };
  const rec = useVoiceRecorder({ onAutoStop: send });
  // X48s review: Post waits while the microphone is on or a line is out
  useEffect(() => { if (live.current) onOpen(starting || rec.recording || sending); }, [starting, rec.recording, sending]); // eslint-disable-line react-hooks/exhaustive-deps -- onOpen is a new function each render
  useEffect(function tick() { // the time and the bar move only while a clip runs
    if (!rec.recording) return;
    setNow(Date.now());
    const id = setInterval(() => setNow(Date.now()), 250);
    return () => clearInterval(id);
  }, [rec.recording]);
  const tap = async () => {
    if (out.current) return;
    if (rec.recording) {
      try { await send(await rec.stop()); } catch { setError(VOICE.failed); }
      return;
    }
    sent.current = false;
    setError("");
    setStarting(true); // X48s review: Post waits while the microphone opens, too
    try {
      setSince(Date.now()); // X48s review: before the start, so the first frame shows 0:00
      await rec.start();
    } catch (err) {
      if (err instanceof ClipBusy) return; // the clip of the automatic stop is still on its way
      setError(err instanceof VoiceError && err.code === "denied" ? VOICE.micOff : VOICE.failed);
    } finally {
      if (live.current) setStarting(false);
    }
  };
  const typed = () => {
    const t = text.trim();
    if (!t || out.current || rec.recording) return;
    ask("typed", { text: t });
  };
  const undo = () => {
    const back = undoTo(turns);
    if (!back) return;
    onApply(back.items, back.pickup);
    setTurns((all) => all.map((t, k) => (k === all.length - 1 ? { ...t, undone: true } : t)));
  };

  const undoable = canUndo(turns, items, pickup);
  const { marks, pickup: pickupMarked } = liveMarks(turns, items, pickup);
  const thread = (
    <View testID="chat" accessibilityLiveRegion="polite" aria-live="polite" style={{ marginTop: space.gap, gap: space.gap }}>
      {turns.map((t, k) => (
        <View key={k} style={{ gap: space.gap }}>
          <Msg you><Muted>{t.how === "said" ? VOICE.said : VOICE.typed}</Muted><Body>{"“" + t.text + "”"}</Body></Msg>
          <Msg you={false}>
            {t.count > 0 ? <Body>{VOICE.made(t.count)}</Body> : null}
            {(t.lines.length ? t.lines : [VOICE.none]).map((l, j) => <Body key={j}>{"• " + l}</Body>)}
            {t.undone ? <Muted>{VOICE.undone}</Muted> : null}
            {k === turns.length - 1 && undoable ? <TextLink testID="chat-undo" onPress={undo}>{VOICE.undo}</TextLink> : null}
          </Msg>
        </View>
      ))}
      {error ? <ErrorText>{error}</ErrorText> : null}
      {turns.length === 0 ? <Muted lines={2}>{VOICE.examples}</Muted> : null}
    </View>
  );
  const limit = VOICE_MAX_MS - 500;
  const secs = Math.max(0, Math.floor((now - since) / 1000));
  const bar = rec.recording ? (
    <View testID="chat-listening" style={{ marginTop: space.gap, gap: space.gap, padding: space.item, borderRadius: radius.card,
      borderWidth: size.border, borderColor: col.accent, backgroundColor: col.card }}>
      <Row>
        <IconBtn testID="mic" big glyph="stop" label={VOICE.stop} onPress={tap} />
        <View style={{ flex: 1, minWidth: 0 }}><Body>{VOICE.listeningFor(secs)}</Body><Muted lines={2}>{VOICE.tapStop}</Muted></View>
      </Row>
      <Progress share={Math.min(1, (now - since) / limit)} />
    </View>
  ) : sending ? (
    <Row style={{ marginTop: space.gap, padding: space.item }}>
      <ActivityIndicator color={col.accent} /><View style={{ flex: 1, minWidth: 0 }}><Body>{VOICE.reading}</Body></View></Row>
  ) : (
    <Row style={{ marginTop: space.gap }}>
      <IconBtn testID="mic" big glyph="mic" label={VOICE.start} onPress={tap} />
      <View style={{ flex: 1, minWidth: 0 }}>
        <Field testID="chat-text" accessibilityLabel={VOICE.typeName} placeholder={VOICE.hint} value={text} maxLength={500}
          returnKeyType="send" onSubmitEditing={typed} onChangeText={setText}
          right={text.trim() ? <FieldButton title={VOICE.send} onPress={typed} /> : undefined} />
      </View>
    </Row>
  );
  return { thread, bar, marks, pickupMarked, clear: () => { setTurns([]); setError(""); setText(""); } };
}
