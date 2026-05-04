import Link from 'next/link';

export default function NotFound() {
  return (
    <div className="max-w-3xl mx-auto px-4 py-24 text-center">
      <p className="text-dim text-xs uppercase tracking-widest mb-4">404</p>
      <p className="text-text font-bold text-xl mb-2">Paper not found</p>
      <p className="text-dim text-sm mb-8">
        This arXiv ID isn&apos;t in the database yet — try again after the next ingest.
      </p>
      <Link
        href="/"
        className="text-xs px-4 py-2 rounded border border-border text-mid hover:border-mid hover:text-text transition-colors"
      >
        ← Back to feed
      </Link>
    </div>
  );
}
