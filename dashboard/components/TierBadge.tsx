interface Props {
  tier: string;
  className?: string;
}

const TIER_CONFIG: Record<string, { label: string; color: string; bg: string }> = {
  hype:   { label: 'HYPE',   color: '#e8513a', bg: 'rgba(232,81,58,0.12)' },
  likely: { label: 'LIKELY', color: '#f5c842', bg: 'rgba(245,200,66,0.12)' },
  low:    { label: 'LOW',    color: '#3a8a82', bg: 'rgba(58,138,130,0.12)' },
};

export default function TierBadge({ tier, className = '' }: Props) {
  const cfg = TIER_CONFIG[tier] ?? TIER_CONFIG.low;
  return (
    <span
      className={`inline-block text-xs font-bold tracking-widest px-2 py-0.5 rounded ${className}`}
      style={{ color: cfg.color, background: cfg.bg, border: `1px solid ${cfg.color}33` }}
    >
      {cfg.label}
    </span>
  );
}
