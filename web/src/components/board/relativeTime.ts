// Short relative-time formatter used by the board card. Deliberately
// library-free — we only need "30s / 5m / 2h / 4d / 3w / 2mo / 1y"
// resolution for glanceable card timestamps, and the output is stable
// regardless of locale so snapshot tests stay green.
//
// Falls back to the raw ISO string if parsing fails so the card never
// renders "NaN".
export function relativeTime(iso: string, now: Date = new Date()): string {
  const t = Date.parse(iso);
  if (Number.isNaN(t)) return iso;
  const diff = Math.max(0, now.getTime() - t);
  const sec = Math.floor(diff / 1000);
  if (sec < 60) return `${sec}s`;
  const min = Math.floor(sec / 60);
  if (min < 60) return `${min}m`;
  const hr = Math.floor(min / 60);
  if (hr < 24) return `${hr}h`;
  const d = Math.floor(hr / 24);
  if (d < 7) return `${d}d`;
  const w = Math.floor(d / 7);
  if (w < 5) return `${w}w`;
  const mo = Math.floor(d / 30);
  if (mo < 12) return `${mo}mo`;
  const y = Math.floor(d / 365);
  return `${y}y`;
}
