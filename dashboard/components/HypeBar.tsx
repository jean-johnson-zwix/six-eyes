interface Props {
  score: number; // 0–1
  tier: string;
}

const TIER_COLOR: Record<string, string> = {
  hype:   '#e8513a',
  likely: '#f5c842',
  low:    '#3a8a82',
};

export default function HypeBar({ score, tier }: Props) {
  const pct = Math.round(score * 100);
  const color = TIER_COLOR[tier] ?? TIER_COLOR.low;
  const filled = Math.round(score * 12); // 12-char bar

  return (
    <span className="inline-flex items-center gap-2 text-xs font-mono">
      <span style={{ color }} aria-hidden>
        {'█'.repeat(filled)}
        {'░'.repeat(12 - filled)}
      </span>
      <span style={{ color }} className="font-bold tabular-nums w-10 text-right">
        {pct}%
      </span>
    </span>
  );
}
