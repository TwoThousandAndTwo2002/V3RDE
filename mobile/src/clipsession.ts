// X36a: one voice clip at a time, with the automatic stop before the
// server's limit (VOICE_MAX_MS). Review finding 7 (X35e): the timer called
// stop() and threw the clip away, so a 60 s clip was lost and the Stop tap
// after it failed. Here the stop is one promise: the timer starts it, and the
// Stop tap (or onAutoStop) gets the same clip. Plain TypeScript (no React),
// so tests/clipsession.test.mts runs it under node.
//
// States: idle -> starting (begin runs) -> recording -> finishing (finish
// runs) -> stopped (the clip waits for stop()) -> idle. A cancel always wins
// (the third review): it moves a new generation, so a begin that ends after
// it closes the microphone at once, and an automatic stop that ends after it
// does not call onAutoStop.

import { MIC } from "./words";

export class ClipBusy extends Error {
  constructor() {
    super(MIC.busy);
  }
}

export class ClipCancelled extends Error {
  constructor() {
    super(MIC.cancelled);
  }
}

export type ClipParts<C> = {
  begin: () => Promise<void>; // open the microphone and start the clip
  finish: () => Promise<C>; // close it and return the clip
  abort: () => void; // close it and drop the clip
};

type State = "idle" | "starting" | "recording" | "finishing" | "stopped";

export class ClipSession<C> {
  private state: State = "idle";
  private gen = 0;
  private parts: ClipParts<C> | null = null;
  private timer: ReturnType<typeof setTimeout> | null = null;
  private begun: Promise<void> | null = null;
  private done: Promise<C> | null = null;
  private readonly maxMs: number;
  private readonly onChange: (recording: boolean) => void;
  private readonly onAutoStop: (clip: C) => void;

  // plain fields, not parameter properties: node --experimental-strip-types
  // runs the tests, and it cannot strip those.
  constructor(maxMs: number, onChange?: (recording: boolean) => void, onAutoStop?: (clip: C) => void) {
    this.maxMs = maxMs;
    this.onChange = onChange ?? (() => undefined);
    this.onAutoStop = onAutoStop ?? (() => undefined);
  }

  // recording is true from start() until the clip is finished or cancelled.
  get recording(): boolean {
    return this.state === "starting" || this.state === "recording";
  }

  // start opens one clip. While a clip starts, records or finishes, a second
  // start throws ClipBusy (no second microphone over the first). A stopped
  // clip that nobody picked up is dropped.
  async start(parts: ClipParts<C>): Promise<void> {
    if (this.state === "starting" || this.state === "recording" || this.state === "finishing") throw new ClipBusy();
    const gen = ++this.gen;
    this.state = "starting";
    this.parts = parts;
    this.done = null;
    this.onChange(true);
    const begun = parts.begin();
    this.begun = begun;
    try {
      await begun;
    } catch (e) {
      if (gen === this.gen) {
        this.reset();
        this.onChange(false);
      }
      throw e;
    } finally {
      if (this.begun === begun) this.begun = null;
    }
    if (gen !== this.gen) {
      // cancelled while the microphone opened: close it now
      try { parts.abort(); } catch { /* already closed */ }
      throw new ClipCancelled();
    }
    this.state = "recording";
    this.timer = setTimeout(() => {
      this.stop().then((c) => { if (gen === this.gen) this.onAutoStop(c); }, () => undefined);
    }, this.maxMs);
  }

  // stop ends the clip and returns it. During start it waits for the
  // microphone, then stops. A second call (the Stop tap after the automatic
  // stop) returns the same clip; with no clip it rejects.
  stop(): Promise<C> {
    if (this.state === "starting" && this.begun) {
      const gen = this.gen;
      return this.begun.then(() => {
        if (gen !== this.gen) throw new ClipCancelled();
        return this.stop();
      });
    }
    if (this.state === "finishing" || this.state === "stopped") {
      const d = this.done!;
      if (this.state === "stopped") this.reset();
      return d;
    }
    if (this.state !== "recording" || !this.parts) return Promise.reject(new Error(MIC.notRecording));
    this.clearTimer();
    const gen = this.gen;
    const d = this.parts.finish();
    this.state = "finishing";
    this.done = d;
    this.onChange(false);
    d.then(
      () => { if (gen === this.gen && this.state === "finishing") this.state = "stopped"; },
      () => { if (gen === this.gen) this.reset(); },
    );
    return d;
  }

  // cancel drops the clip (also on unmount). A clip that is finishing ends,
  // but nobody gets it (no onAutoStop).
  cancel(): void {
    const p = this.parts;
    const wasRecording = this.state === "recording";
    this.gen++;
    this.reset();
    if (p && wasRecording) {
      try { p.abort(); } catch { /* already closed */ }
    }
    this.onChange(false);
  }

  private reset() {
    this.clearTimer();
    this.state = "idle";
    this.parts = null;
    this.done = null;
  }

  private clearTimer() {
    if (this.timer) clearTimeout(this.timer);
    this.timer = null;
  }
}
