'use client';

import { useState, useMemo } from 'react';
import type { Paper } from '@/lib/api';
import PaperCard, { ACCENT_COLORS } from './PaperCard';

const PAGE_SIZE = 10;

interface Props {
  papers: Paper[];
}

export default function FeedControls({ papers }: Props) {
  const [query, setQuery] = useState('');
  const [page, setPage] = useState(1);

  const filtered = useMemo(() => {
    const q = query.trim().toLowerCase();
    if (!q) return papers;
    return papers.filter(p =>
      p.title.toLowerCase().includes(q) ||
      p.authors.some(a => a.toLowerCase().includes(q))
    );
  }, [papers, query]);

  const totalPages = Math.max(1, Math.ceil(filtered.length / PAGE_SIZE));
  const safePage = Math.min(page, totalPages);
  const slice = filtered.slice((safePage - 1) * PAGE_SIZE, safePage * PAGE_SIZE);

  function handleSearch(val: string) {
    setQuery(val);
    setPage(1);
  }

  return (
    <>
      {/* Search bar */}
      <div className="mb-5">
        <div className="rounded-lg p-[1.5px]" style={{
          background: 'linear-gradient(135deg,#e8513a,#f09030,#f5c842,#5a9e52,#3a8a82,#4a72c4,#9068c0,#d44878)',
        }}>
          <input
            type="text"
            placeholder="Search by title or author..."
            value={query}
            onChange={e => handleSearch(e.target.value)}
            className="w-full px-4 py-2.5 rounded-[6px] text-text text-sm placeholder:text-dim focus:outline-none font-mono"
            style={{ background: '#181818' }}
          />
        </div>
      </div>

      {/* Count */}
      <p className="text-dim text-xs mb-4 tabular-nums">
        {filtered.length} paper{filtered.length !== 1 ? 's' : ''}
        {query.trim() && (
          <span> matching <span className="text-mid">&ldquo;{query.trim()}&rdquo;</span></span>
        )}
      </p>

      {/* Paper list */}
      {filtered.length === 0 ? (
        <div
          className="border border-border rounded-lg p-8 text-center"
          style={{ background: '#181818' }}
        >
          <p className="text-dim text-sm">No papers match your search.</p>
        </div>
      ) : (
        <div className="flex flex-col gap-3">
          {slice.map((paper, i) => (
            <PaperCard
              key={paper.arxivId}
              paper={paper}
              accentColor={ACCENT_COLORS[((safePage - 1) * PAGE_SIZE + i) % ACCENT_COLORS.length]}
            />
          ))}
        </div>
      )}

      {/* Pagination */}
      {totalPages > 1 && (
        <div className="flex items-center justify-between mt-6 text-xs">
          <button
            onClick={() => setPage(p => Math.max(1, p - 1))}
            disabled={safePage === 1}
            className="px-3 py-1.5 rounded border border-border text-dim hover:border-mid hover:text-mid transition-colors disabled:opacity-30 disabled:cursor-not-allowed"
          >
            ← Prev
          </button>
          <span className="text-dim tabular-nums">
            {safePage} / {totalPages}
          </span>
          <button
            onClick={() => setPage(p => Math.min(totalPages, p + 1))}
            disabled={safePage === totalPages}
            className="px-3 py-1.5 rounded border border-border text-dim hover:border-mid hover:text-mid transition-colors disabled:opacity-30 disabled:cursor-not-allowed"
          >
            Next →
          </button>
        </div>
      )}
    </>
  );
}
