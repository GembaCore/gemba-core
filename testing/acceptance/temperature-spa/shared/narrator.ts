// shared/narrator.ts (gm-root.27.37) — structured event timeline for
// TTS / voiceover alignment with the demo-mode video capture.
//
// The acceptance run emits a `narration.json` alongside its MP4 so a
// downstream pipeline (ElevenLabs, OpenAI TTS, macOS `say`, manual
// voiceover) can produce audio aligned to the actual frame timing of
// the run. Phrases are hardcoded per emit-point — output is
// deterministic; we never call an LLM for narration text.
//
// Pacing hints:
//   short  ≈ 1s  ("Done", "M3 lands")
//   medium ≈ 2s  ("Mock agents claim M1 scaffolding work")
//   long   ≈ 3s  ("The operator configures a pool with the
//                acceptance-engineer persona")
//
// Renderers (see scripts/narrate-demo.sh) feed each phrase into TTS
// at `at_ms` from the video start. If the produced clip is longer
// than the gap to the next event, it ducks under the next utterance
// or trims — that's a renderer concern, not ours.

import { mkdirSync, writeFileSync } from 'node:fs';
import path from 'node:path';

export type DurationHint = 'short' | 'medium' | 'long';

export interface NarrationEvent {
  /** Milliseconds since the narrator was started. */
  at_ms: number;
  /** Phrase to be spoken. Plain English; no punctuation TTS engines stumble on. */
  phrase: string;
  /** Pacing hint for the renderer. */
  duration_hint: DurationHint;
}

export interface NarrationFile {
  /** Path of the video the JSON pairs with. Renderer fills this in if absent. */
  video_path?: string;
  /** Total run duration in ms (now - startedAt at write time). */
  duration_ms: number;
  /** Strictly time-ordered events. Step boundaries get their own emits. */
  events: NarrationEvent[];
}

/**
 * Narrator is a thin event recorder. The acceptance spec accepts an
 * optional Narrator on its opts; if absent (the common test path)
 * `emit()` is a no-op. Demo-mode runs construct one + write at end.
 */
export class Narrator {
  private readonly startedAt: number;
  private readonly events: NarrationEvent[] = [];

  constructor(now: () => number = () => Date.now()) {
    this.startedAt = now();
    this._now = now;
  }

  private readonly _now: () => number;

  /** Record an event at the current offset from start. */
  emit(phrase: string, duration_hint: DurationHint = 'medium'): void {
    this.events.push({
      at_ms: Math.max(0, this._now() - this.startedAt),
      phrase,
      duration_hint,
    });
  }

  /** Build the JSON envelope without writing it. Useful for tests. */
  build(opts: { video_path?: string } = {}): NarrationFile {
    return {
      video_path: opts.video_path,
      duration_ms: Math.max(0, this._now() - this.startedAt),
      events: [...this.events],
    };
  }

  /**
   * Persist as `narration.json` next to a target path. Creates the
   * parent directory if missing.
   */
  writeTo(filePath: string, opts: { video_path?: string } = {}): void {
    const dir = path.dirname(filePath);
    mkdirSync(dir, { recursive: true });
    writeFileSync(filePath, JSON.stringify(this.build(opts), null, 2) + '\n');
  }

  /** Snapshot for inspection in tests. */
  snapshot(): NarrationEvent[] {
    return [...this.events];
  }
}

/** No-op narrator for non-demo runs. */
export const noopNarrator: Pick<Narrator, 'emit'> = {
  emit() {
    /* drop */
  },
};
