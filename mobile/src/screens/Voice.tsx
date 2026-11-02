// The chat bar of the review, written new for V3RDE (NEW_WORK_UI.md, UI 17; X48s, the approved "V3RDE Chat Bar
// Mockup"; mobile/docs/style_guide.md "Chat bar"). Tap the microphone and talk, or type and tap Send. The voice
// service turns the clip into text (Muse Voice Transcribe) and the text into changes (Muse Spark). The changes go
// into the list at once; the chat under the list keeps what was said or typed and what changed, with Undo on the
// last change only. Nothing posts until Post. The review clears the chat after a post.
import { useEffect, useRef, useState } from "react";
import { ActivityIndicator, View } from "react-native";
import * as RN from "react-native";
import { ApiError, voice } from "../api";
import { deviceLocale, type ChatLine, type ItemForm } from "../logic";
import { canUndo, chatTurn, earlierToSend, keepOne, liveMarks, nextEarlier, proposalOf, turnsWithFound, undoTo, voiceError, voiceItems, type Earlier, type How, type Turn } from "../voice";
import { earlierWithFound } from "../earlierfound";
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
  /** X48u: the chat lines for the label report. */
  log: ChatLine[];
  /** I4 review: found items join the turns (Undo keeps them). */
  addFound: (added: ItemForm[]) => void;
};

/** I4: the chat line of one find: the number found, or the error text. */
function findText(name: string, n: number | string): string {
  if (typeof n === "string") return n || VOICE.findFailed(name);
  return n > 0 ? VOICE.foundOne(name, n) : VOICE.notFound(name);
}

/** I4 review: why a find failed, in the chat's words ("" = the general "Could not look for …"). */
function findError(err: unknown): string {
  if (!(err instanceof ApiError)) return "";
  const code = typeof err.body?.code === "string" ? err.body.code : "";
  if (err.status === 404) return VOICE.findGone; // the scan's photo is not kept: trying again does not help
  if (err.status === 429 || err.status === 503) return voiceError(err.status, code, VOICE);
  return "";
}

/** X49q: how long each hint of the chat box shows. */
export const HINT_SWAP_MS = 4000;

export function useChat({ items, pickup, onApply, onOpen, onFind, current }: {
  items: ItemForm[]; pickup: string;
  /** X48u: how says what made the change (a voice line, a typed line, Undo), for the label report. */
  onApply: (items: ItemForm[], pickup: string, how: "voice" | "text" | "undo") => void;
  /** True while a line is out: Post waits for it. */
  onOpen: (open: boolean) => void;
  /** I4: looks for a missed item by its name in the scan photo; the number of items found and added (an error = -1). */
  onFind?: (name: string, how: "voice" | "text") => Promise<number>;
  /** I4 review: the list as it is now (the review's ref, set at once), so a line that ends right after a find sees it. */
  current?: () => { items: ItemForm[]; pickup: string };
}): Chat {
  const col = useColors();
  const { ended } = useSession();
  const [turns, setTurns] = useState<Turn[]>([]);
  // I4: the answer of each find, by turn and name (none yet = still looking; -1 = it failed)
  const [finds, setFinds] = useState<Record<string, number | string>>({}); // the number found, or the error text
  const turnCount = useRef(0);
  const pending = useRef(0); // I4: finds still out (Post waits)
  const [waiting, setWaiting] = useState(0);
  const [sending, setSending] = useState(false);
  const [error, setError] = useState("");
  const [text, setText] = useState("");
  // X49q (T9 2026-10-31): the hint in the box changes between "Talk or type a change" and two examples, every
  // HINT_SWAP_MS, while the chat is empty (the examples line under the box is gone)
  const [tip, setTip] = useState(0);
  const empty = text === ""; // X49q review: no turns while the user types (the hint is not on view)
  useEffect(() => {
    if (turns.length > 0 || !empty) return;
    const id = setInterval(() => setTip((t) => (t + 1) % VOICE.hints.length), HINT_SWAP_MS);
    return () => clearInterval(id);
  }, [turns.length, empty]);
  const [since, setSince] = useState(0); // when the clip started (for the time and the bar)
  const [starting, setStarting] = useState(false); // the microphone is opening (a permission prompt may wait)
  const [now, setNow] = useState(0);
  const sent = useRef(false); // one clip goes one time (the automatic stop and the Stop tap give the same clip)
  const out = useRef(false); // X48s review: one line at a time (a fast double Send sends one line)
  const live = useRef(true); // false after Start over or the end of the flow: a late answer changes nothing
  const earlier = useRef<Earlier | null>(null); // X59: the last turn, when it ended in a question
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
      const before = earlierToSend(earlier.current, at, place); // X59 review: none after a hand edit or an Undo
      const answer = proposalOf(await voice(said, voiceItems(at, VOICE.unnamed), place, deviceLocale(), before));
      if (!live.current) return;
      // X48s review: the answer goes onto the list as it is NOW (a hand edit while the line was out stays)
      const { items: nowItems, pickup: nowPickup } = current ? current() : list.current;
      const p = keepOne(nowItems, answer, VOICE.notAll);
      const t = chatTurn(nowItems, nowPickup, p, how, how === "typed" ? (said as { text: string }).text : p.transcript, VOICE,
        { title: VOICE.markTitle, servings: VOICE.markServings, mark: VOICE.mark });
      if (t.count > 0) onApply(t.after.items, t.after.pickup, how === "said" ? "voice" : "text");
      earlier.current = nextEarlier(before, how === "typed" ? (said as { text: string }).text : answer.transcript, answer, t.after);
      const idx = turnCount.current++;
      setTurns((all) => [...all, t]);
      if (t.find && onFind) { // I4: one find for each name, at the same time
        pending.current += t.find.length;
        setWaiting(pending.current);
        for (const name of t.find) {
          const put = (n: number | string) => {
            if (!live.current) return;
            setFinds((f) => ({ ...f, [idx + ":" + name]: n }));
            if (RN.Platform.OS === "ios") RN.AccessibilityInfo.announceForAccessibility(findText(name, n)); // I4 review
          };
          onFind(name, how === "said" ? "voice" : "text").then(put, (err) => put(findError(err)))
            .finally(() => { pending.current--; if (live.current) setWaiting(pending.current); });
        }
      }
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
  useEffect(() => { if (live.current) onOpen(starting || rec.recording || sending || waiting > 0); }, [starting, rec.recording, sending, waiting]); // eslint-disable-line react-hooks/exhaustive-deps -- onOpen is a new function each render
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
    onApply(back.items, back.pickup, "undo");
    earlier.current = null; // X59 review: the undone words never go back to the model
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
            {(t.lines.length ? t.lines : t.find && onFind ? [] : [VOICE.none]).map((l, j) => <Body key={j}>{"• " + l}</Body>)}
            {t.find && onFind ? t.find.map((name) => {
              const n = finds[k + ":" + name];
              if (n === undefined) return <Row key={name} tight><ActivityIndicator color={col.accent} /><View style={{ flex: 1, minWidth: 0 }}><Body>{VOICE.looking(name)}</Body></View></Row>;
              return <Body key={name}>{findText(name, n)}</Body>;
            }) : null}
            {t.undone ? <Muted>{VOICE.undone}</Muted> : null}
            {k === turns.length - 1 && undoable ? <TextLink testID="chat-undo" onPress={undo}>{VOICE.undo}</TextLink> : null}
          </Msg>
        </View>
      ))}
      {error ? <ErrorText>{error}</ErrorText> : null}
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
        <Field testID="chat-text" accessibilityLabel={VOICE.typeName} placeholder={turns.length === 0 ? VOICE.hints[tip] : VOICE.hint} value={text} maxLength={500}
          returnKeyType="send" onSubmitEditing={typed} onChangeText={setText}
          right={text.trim() ? <FieldButton title={VOICE.send} onPress={typed} /> : undefined} />
      </View>
    </Row>
  );
  // X48u review: a line that set the pickup may hold an address: its words are not sent
  const log: ChatLine[] = turns.map((t) => ({ how: t.how === "said" ? "voice" : "text", text: t.pickupSaid ? "" : t.text, changes: t.count,
    ...(t.undone ? { undone: true } : {}), ...(t.pickupSaid ? { pickup: true } : {}) }));
  // IM review: a found item joins the earlier turn's list too, so the question asked before the find still goes with the next line
  const addFound = (added: ItemForm[]) => {
    if (!live.current || !added.length) return;
    setTurns((all) => turnsWithFound(all, added));
    earlier.current = earlierWithFound(earlier.current, added);
  };
  return { thread, bar, marks, pickupMarked, log, addFound, clear: () => { setTurns([]); setFinds({}); turnCount.current = 0; setError(""); setText(""); earlier.current = null; } };
}
