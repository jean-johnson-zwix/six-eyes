import Link from 'next/link';
import type { Paper } from '@/lib/api';
import TierBadge from './TierBadge';
import HypeBar from './HypeBar';

function formatDate(iso: string) {
  return new Date(iso).toLocaleDateString('en-US', {
    month: 'short', day: 'numeric', year: 'numeric',
  });
}

function authorLine(authors: string[]) {
  if (authors.length === 0) return '';
  if (authors.length <= 3) return authors.join(' · ');
  return `${authors.slice(0, 3).join(' · ')} +${authors.length - 3}`;
}

export const ACCENT_COLORS = [
  '#e8513a', // coral
  '#f09030', // orange
  '#f5c842', // yellow
  '#5a9e52', // sage
  '#3a8a82', // teal
  '#4a72c4', // blue
  '#9068c0', // lavender
  '#d44878', // pink
];

interface Props {
  paper: Paper;
  accentColor: string;
}

export default function PaperCard({ paper, accentColor }: Props) {
  const {
    arxivId, title, authors, categories, submittedAt,
    hasCode, hypeScore, hypeTier, maxHIndex,
  } = paper;

  return (
    <Link
      href={`/paper/${encodeURIComponent(arxivId)}`}
      className="block group"
    >
      <article
        className="paper-card rounded-lg p-5"
        style={{ '--accent': accentColor, background: '#181818' } as React.CSSProperties}
      >
        {/* Accent strip */}
        <div className="paper-card__strip" />
        {/* Top row: tier badge + categories + date */}
        <div className="flex items-center gap-2 mb-3 flex-wrap">
          <TierBadge tier={hypeTier} />
          <span className="text-dim text-xs">
            {categories.slice(0, 3).join(' · ')}
          </span>
          <span className="ml-auto text-dim text-xs tabular-nums flex-shrink-0">
            {formatDate(submittedAt)}
          </span>
        </div>

        {/* Title */}
        <h2
          className="text-text font-bold mb-2 leading-snug group-hover:text-mid transition-colors"
          style={{ fontSize: '0.95rem', letterSpacing: '-0.3px' }}
        >
          {title}
        </h2>

        {/* Authors */}
        <p className="text-dim text-xs mb-3 truncate">
          {authorLine(authors)}
        </p>

        {/* Bottom row: hype bar + metadata chips */}
        <div className="flex items-center gap-4 flex-wrap">
          <HypeBar score={hypeScore} tier={hypeTier} />
          {maxHIndex != null && (
            <span className="text-dim text-xs">
              h-idx <span className="text-mid">{maxHIndex}</span>
            </span>
          )}
          {hasCode && (
            <span className="text-xs text-dim border border-border rounded px-1.5 py-0.5">
              code
            </span>
          )}
        </div>
      </article>
    </Link>
  );
}
