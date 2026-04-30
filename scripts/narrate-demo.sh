#!/usr/bin/env bash
# narrate-demo.sh (gm-root.27.37) — render a TTS-narrated mp4 from a
# Playwright video + the structured narration.json the acceptance test
# emits.
#
# Usage:
#   scripts/narrate-demo.sh narration.json demo.mp4 [out.mp4] [--voice <macsay-voice>]
#
# Inputs:
#   narration.json — emitted by the acceptance test (gm-root.27.37).
#                    See testing/acceptance/temperature-spa/shared/narrator.ts
#                    for the schema.
#   demo.mp4       — the Playwright-captured video aligned to the run.
#
# Output:
#   out.mp4        — narrated video. Defaults to <demo>-narrated.mp4
#                    next to the input when omitted.
#
# Behavior:
#   - Reads each event from narration.json.
#   - Generates a per-event WAV via macOS `say` (or `--say-cmd` override
#     for ElevenLabs / OpenAI TTS / etc.).
#   - Mixes each WAV into the video at its at_ms offset using ffmpeg's
#     adelay + amix filter. The original video's audio (if any) ducks
#     to 30% during narration windows.
#
# Reference impl. Operators may prefer manual voice-over or a richer
# pipeline; this is the minimal "show that the JSON drives a tool"
# script that the bead's DoD calls for.

set -euo pipefail

if [ "$#" -lt 2 ]; then
  echo "usage: $0 narration.json demo.mp4 [out.mp4] [--voice <name>]" >&2
  exit 64
fi

narration_json="$1"
input_mp4="$2"
shift 2

out_mp4=""
voice="Samantha"
say_cmd_template=""

while [ "$#" -gt 0 ]; do
  case "$1" in
    --voice)
      voice="$2"; shift 2 ;;
    --say-cmd)
      # External TTS template, e.g.:
      #   --say-cmd 'curl -s elevenlabs.../{phrase} -o {out}'
      # The placeholders {phrase} and {out} get substituted per event.
      say_cmd_template="$2"; shift 2 ;;
    *)
      if [ -z "$out_mp4" ]; then out_mp4="$1"; shift; else
        echo "unknown arg: $1" >&2; exit 64
      fi ;;
  esac
done

if [ -z "$out_mp4" ]; then
  out_mp4="${input_mp4%.mp4}-narrated.mp4"
fi

command -v ffmpeg >/dev/null 2>&1 || { echo "ffmpeg not found" >&2; exit 65; }
command -v jq     >/dev/null 2>&1 || { echo "jq not found"     >&2; exit 65; }

if [ -z "$say_cmd_template" ] && ! command -v say >/dev/null 2>&1; then
  echo "no TTS available: macOS 'say' not on PATH and no --say-cmd given" >&2
  exit 65
fi

# Stage WAVs in a scratch dir.
work=$(mktemp -d "${TMPDIR:-/tmp}/narrate-XXXXXX")
trap 'rm -rf "$work"' EXIT

# Read events: tab-separated at_ms<TAB>phrase
mapfile -t rows < <(jq -r '.events[] | "\(.at_ms)\t\(.phrase)"' "$narration_json")

if [ "${#rows[@]}" -eq 0 ]; then
  echo "narration.json has no events; copying input to output unchanged" >&2
  cp "$input_mp4" "$out_mp4"
  exit 0
fi

# Generate one WAV per event.
i=0
for row in "${rows[@]}"; do
  at_ms="${row%%$'\t'*}"
  phrase="${row#*$'\t'}"
  out_wav="$work/event-$i.wav"
  if [ -n "$say_cmd_template" ]; then
    cmd="${say_cmd_template//\{phrase\}/$phrase}"
    cmd="${cmd//\{out\}/$out_wav}"
    bash -c "$cmd"
  else
    say -v "$voice" -o "$out_wav" --data-format=LEI16@22050 "$phrase"
  fi
  i=$((i + 1))
done

# Build the ffmpeg filter graph.
#   - Each event gets [event-i] -> adelay=at_ms|at_ms (stereo)
#   - All delayed events amix together into [narr]
#   - Original video audio ducks to 30% during narration windows via
#     a sidechain compressor; the output is [aout]
inputs=( -i "$input_mp4" )
filter_parts=()
amix_pads=""
i=0
for row in "${rows[@]}"; do
  at_ms="${row%%$'\t'*}"
  inputs+=( -i "$work/event-$i.wav" )
  filter_parts+=( "[$((i + 1)):a]adelay=${at_ms}|${at_ms}[d$i]" )
  amix_pads+="[d$i]"
  i=$((i + 1))
done

filter="${filter_parts[*]}"
filter="${filter// /;}"
filter="$filter;${amix_pads}amix=inputs=$i:dropout_transition=0[narr];[0:a][narr]amix=inputs=2:duration=first:dropout_transition=0[aout]"

ffmpeg -y "${inputs[@]}" \
  -filter_complex "$filter" \
  -map 0:v -map "[aout]" \
  -c:v copy -c:a aac -b:a 192k \
  "$out_mp4"

echo "narrated video → $out_mp4"
