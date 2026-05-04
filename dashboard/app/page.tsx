import Link from 'next/link';
import { fetchPapers } from '@/lib/api';
import FeedControls from '@/components/FeedControls';

const TIER_LABELS: Record<string, string> = {
  '':       'All',
  'hype':   'Hype',
  'likely': 'Likely',
  'low':    'Low',
};

interface Props {
  searchParams: Promise<{ tier?: string; days?: string }>;
}

export default async function FeedPage({ searchParams }: Props) {
  const params = await searchParams;
  const tier = params.tier && TIER_LABELS[params.tier] ? params.tier : '';
  const days = Math.min(Number(params.days) || 30, 90);

  let papers: Awaited<ReturnType<typeof fetchPapers>> = [];
  let error: string | null = null;

  try {
    papers = await fetchPapers(days, 100, tier || undefined);
  } catch (e) {
    error = e instanceof Error ? e.message : 'Failed to load papers';
  }

  return (
    <div className="max-w-3xl mx-auto px-4 py-8">
      {/* Header */}
      <div className="mb-8">
        <h1
          className="text-text font-bold mb-1"
          style={{ fontSize: '1.5rem', letterSpacing: '-0.5px' }}
        >
          Paper Feed
        </h1>
        <p className="text-dim text-sm">
          ML papers from the last {days} days, ranked by predicted hype.
        </p>
      </div>

      {/* Tier filter */}
      <div className="flex gap-2 mb-6 flex-wrap">
        {Object.entries(TIER_LABELS).map(([value, label]) => {
          const active = tier === value;
          const href = value ? `/?tier=${value}` : '/';
          return (
            <Link
              key={value}
              href={href}
              className={`text-xs px-3 py-1.5 rounded border transition-colors ${
                active
                  ? 'border-mid text-text bg-card'
                  : 'border-border text-dim hover:border-dim hover:text-mid'
              }`}
            >
              {label}
            </Link>
          );
        })}

        {/* Days filter */}
        <div className="ml-auto flex gap-2">
          {[7, 14, 30].map(d => (
            <Link
              key={d}
              href={`?${new URLSearchParams({ ...(tier && { tier }), days: String(d) })}`}
              className={`text-xs px-3 py-1.5 rounded border transition-colors ${
                days === d
                  ? 'border-mid text-text bg-card'
                  : 'border-border text-dim hover:border-dim hover:text-mid'
              }`}
            >
              {d}d
            </Link>
          ))}
        </div>
      </div>

      {/* Content */}
      {error ? (
        <div className="border border-border rounded-lg p-8 text-center" style={{ background: '#181818' }}>
          <p className="text-dim text-sm mb-2">Could not load papers</p>
          <p className="text-xs" style={{ color: '#e8513a' }}>{error}</p>
          <p className="text-dim text-xs mt-4">
            The API may be cold-starting (Render free tier). Try refreshing in 30s.
          </p>
        </div>
      ) : papers.length === 0 ? (
        <div className="border border-border rounded-lg p-8 text-center" style={{ background: '#181818' }}>
          <p className="text-dim text-sm">No papers found for this filter.</p>
        </div>
      ) : (
        <FeedControls papers={papers} />
      )}
    </div>
  );
}
