import { notFound } from 'next/navigation';
import Link from 'next/link';
import { fetchPaper } from '@/lib/api';
import TierBadge from '@/components/TierBadge';
import HypeBar from '@/components/HypeBar';

interface Props {
  params: Promise<{ arxivId: string }>;
}

function formatDate(iso: string) {
  return new Date(iso).toLocaleDateString('en-US', {
    month: 'long', day: 'numeric', year: 'numeric',
  });
}

export default async function PaperPage({ params }: Props) {
  const { arxivId } = await params;
  const id = decodeURIComponent(arxivId);

  let paper;
  try {
    paper = await fetchPaper(id);
  } catch {
    paper = null;
  }

  if (!paper) notFound();

  const {
    title, authors, categories, abstract, submittedAt,
    hasCode, hypeScore, hypeTier, maxHIndex, totalPriorPapers, hfUpvotes,
  } = paper;

  const arxivUrl = `https://arxiv.org/abs/${id}`;

  return (
    <div className="max-w-3xl mx-auto px-4 py-8">
      {/* Back */}
      <Link
        href="/"
        className="inline-flex items-center gap-1.5 text-dim text-sm hover:text-mid transition-colors mb-8"
      >
        <span aria-hidden>←</span> Feed
      </Link>

      {/* Tier + categories */}
      <div className="flex items-center gap-2 mb-4 flex-wrap">
        <TierBadge tier={hypeTier} />
        <span className="text-dim text-xs">{categories.join(' · ')}</span>
        <span className="ml-auto text-dim text-xs tabular-nums">
          {formatDate(submittedAt)}
        </span>
      </div>

      {/* Title */}
      <h1
        className="text-text font-bold mb-4 leading-snug"
        style={{ fontSize: '1.4rem', letterSpacing: '-0.5px' }}
      >
        {title}
      </h1>

      {/* Authors */}
      <p className="text-mid text-sm mb-6">{authors.join(', ')}</p>

      {/* Hype score card */}
      <div
        className="border border-border rounded-lg p-5 mb-6"
        style={{ background: '#181818' }}
      >
        <p className="text-dim text-xs mb-3 uppercase tracking-widest">Hype Prediction</p>
        <div className="flex items-center gap-4 flex-wrap">
          <HypeBar score={hypeScore} tier={hypeTier} />
          <span className="text-dim text-xs">
            {Math.round(hypeScore * 100)}% predicted to reach 100+ GitHub stars at T+60d
          </span>
        </div>

        {/* Signal metadata */}
        <div className="flex gap-6 mt-4 flex-wrap">
          {maxHIndex != null && (
            <div>
              <p className="text-dim text-xs uppercase tracking-wider mb-0.5">Top h-index</p>
              <p className="text-mid font-bold">{maxHIndex}</p>
            </div>
          )}
          {totalPriorPapers != null && (
            <div>
              <p className="text-dim text-xs uppercase tracking-wider mb-0.5">Prior papers</p>
              <p className="text-mid font-bold">{totalPriorPapers}</p>
            </div>
          )}
          {hfUpvotes != null && (
            <div>
              <p className="text-dim text-xs uppercase tracking-wider mb-0.5">HF upvotes</p>
              <p className="text-mid font-bold">{hfUpvotes}</p>
            </div>
          )}
          <div>
            <p className="text-dim text-xs uppercase tracking-wider mb-0.5">Code</p>
            <p className="text-mid font-bold">{hasCode ? 'yes' : 'no'}</p>
          </div>
        </div>
      </div>

      {/* Abstract */}
      <div className="mb-8">
        <p className="text-dim text-xs uppercase tracking-widest mb-3">Abstract</p>
        <p className="text-mid text-sm leading-relaxed" style={{ whiteSpace: 'pre-wrap' }}>
          {abstract}
        </p>
      </div>

      {/* Actions */}
      <div className="flex gap-3">
        <a
          href={arxivUrl}
          target="_blank"
          rel="noopener noreferrer"
          className="text-xs px-4 py-2 rounded border border-border text-mid hover:border-mid hover:text-text transition-colors"
        >
          View on arXiv →
        </a>
      </div>
    </div>
  );
}
