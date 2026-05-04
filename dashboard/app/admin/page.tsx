import type { Metadata } from 'next';

export const metadata: Metadata = { title: 'Admin — six-eyes' };

interface ServiceCard {
  name: string;
  category: string;
  description: string;
  url: string | null;
  status: 'live' | 'pending' | 'scheduled';
  accent: string;
}

function buildCards(renderUrl: string | null, grafanaUrl: string | null): ServiceCard[] {
  return [
    // ── Source & CI/CD ──────────────────────────────────────────────
    {
      name: 'GitHub Repo',
      category: 'Source',
      description: 'Ingestion service (Go), GraphQL API (Go), training pipeline (Python), Next.js dashboard.',
      url: 'https://github.com/jean-johnson-zwix/six-eyes',
      status: 'live',
      accent: '#f5c842',
    },
    {
      name: 'GitHub Actions',
      category: 'CI/CD',
      description: 'Daily ingest (0 7 * * *) and weekly Evidently drift report (0 9 * * 1).',
      url: 'https://github.com/jean-johnson-zwix/six-eyes/actions',
      status: 'live',
      accent: '#f5c842',
    },
    {
      name: 'GitHub Pages',
      category: 'Monitoring',
      description: 'Weekly Evidently drift report — DataDriftPreset + DataQualityPreset vs training distribution.',
      url: 'https://jean-johnson-zwix.github.io/six-eyes',
      status: 'live',
      accent: '#5a9e52',
    },
    // ── ML Platform ──────────────────────────────────────────────────
    {
      name: 'DagShub MLflow',
      category: 'Experiment Tracking',
      description: 'All training runs, Optuna trials, and model registry. Champion model: six-eyes-xgb v4.',
      url: 'https://dagshub.com/jajoh151/six-eyes.mlflow',
      status: 'live',
      accent: '#f09030',
    },
    {
      name: 'DagShub Repo',
      category: 'Experiment Tracking',
      description: 'DagShub-mirrored repo used as MLflow artifact store backend.',
      url: 'https://dagshub.com/jajoh151/six-eyes',
      status: 'live',
      accent: '#f09030',
    },
    {
      name: 'HuggingFace Hub',
      category: 'Model Artifacts',
      description: 'Exported XGBoost JSON + model_meta.json fetched by Go API at startup.',
      url: 'https://huggingface.co/jeanjohnson/six-eyes-model',
      status: 'live',
      accent: '#f09030',
    },
    // ── Orchestration ────────────────────────────────────────────────
    {
      name: 'Prefect Cloud',
      category: 'Orchestration',
      description: 'Monthly retrain flow (0 8 1 * *) — deployment: six-eyes-train, pool: six-eyes-work-pool.',
      url: 'https://app.prefect.cloud',
      status: 'live',
      accent: '#4a72c4',
    },
    // ── Infrastructure ───────────────────────────────────────────────
    {
      name: 'Supabase',
      category: 'Database',
      description: 'PostgreSQL — papers table (live ingestion feed). Free tier: ~10K rows.',
      url: 'https://app.supabase.com/project/mtfcirmxlipzyydzgghk',
      status: 'live',
      accent: '#3a8a82',
    },
    {
      name: 'Render (Go API)',
      category: 'Serving',
      description: 'GraphQL API — papers(days, limit, tier) + paper(arxivId). Sleeps after 15 min inactivity.',
      url: renderUrl,
      status: renderUrl ? 'live' : 'pending',
      accent: '#3a8a82',
    },
    // ── Monitoring ───────────────────────────────────────────────────
    {
      name: 'Grafana Cloud',
      category: 'Monitoring',
      description: 'six-eyes MLOps dashboard: mean hype score/week, feature drift by category, paper volume (req 6.2–6.4). Metrics pushed weekly by monitor.py.',
      url: grafanaUrl ? `${grafanaUrl}/d/six-eyes-mlops-v1` : 'https://grafana.com',
      status: grafanaUrl ? 'live' : 'pending',
      accent: '#9068c0',
    },
  ];
}

const STATUS_CONFIG = {
  live:      { label: 'Live',      color: '#5a9e52', bg: 'rgba(90,158,82,0.1)' },
  pending:   { label: 'Pending',   color: '#7a6a45', bg: 'rgba(122,106,69,0.1)' },
  scheduled: { label: 'Scheduled', color: '#4a72c4', bg: 'rgba(74,114,196,0.1)' },
};

function StatusBadge({ status }: { status: ServiceCard['status'] }) {
  const { label, color, bg } = STATUS_CONFIG[status];
  return (
    <span
      className="text-xs px-2 py-0.5 rounded font-mono"
      style={{ color, background: bg, border: `1px solid ${color}33` }}
    >
      {label}
    </span>
  );
}

function Card({ card }: { card: ServiceCard }) {
  const inner = (
    <div
      className="paper-card rounded-lg p-5 h-full flex flex-col gap-3"
      style={{ '--accent': card.accent, background: '#181818' } as React.CSSProperties}
    >
      {/* Top accent strip */}
      <div className="paper-card__strip" />

      <div className="flex items-start justify-between gap-2 mt-1">
        <div>
          <p className="text-[0.65rem] font-mono uppercase tracking-widest mb-1" style={{ color: card.accent }}>
            {card.category}
          </p>
          <h2 className="text-text font-bold text-sm">{card.name}</h2>
        </div>
        <StatusBadge status={card.status} />
      </div>

      <p className="text-dim text-xs leading-relaxed flex-1">{card.description}</p>

      {card.url ? (
        <a
          href={card.url}
          target="_blank"
          rel="noopener noreferrer"
          className="inline-flex items-center gap-1.5 text-xs font-mono transition-colors"
          style={{ color: card.accent }}
        >
          Open →
        </a>
      ) : (
        <p className="text-xs font-mono text-dim">Set API_URL in .env.local</p>
      )}
    </div>
  );

  return inner;
}

export default function AdminPage() {
  // Read service URLs from env at request time (server component)
  const apiUrl     = process.env.API_URL      ?? null;
  const renderUrl  = apiUrl ? apiUrl.replace('/graphql', '') : null;
  const grafanaUrl = process.env.GRAFANA_URL  ?? null;
  const cards = buildCards(renderUrl, grafanaUrl);

  return (
    <div className="max-w-5xl mx-auto px-4 py-8">
      <div className="mb-8">
        <h1
          className="text-text font-bold mb-1"
          style={{ fontSize: '1.5rem', letterSpacing: '-0.5px' }}
        >
          Admin
        </h1>
        <p className="text-dim text-sm">All external services for the six-eyes MLOps pipeline.</p>
      </div>

      <div className="grid gap-4" style={{ gridTemplateColumns: 'repeat(auto-fill, minmax(280px, 1fr))' }}>
        {cards.map(card => (
          <Card key={card.name} card={card} />
        ))}
      </div>

      {/* Module map reference */}
      <div className="mt-10 border border-border rounded-lg p-5" style={{ background: '#181818' }}>
        <p className="text-dim text-xs uppercase tracking-widest mb-4 font-mono">MLOps Module Map</p>
        <div className="grid gap-2" style={{ gridTemplateColumns: 'repeat(auto-fill, minmax(220px, 1fr))' }}>
          {[
            { module: '01 — ML Pipeline',          component: 'training/',                          done: true },
            { module: '02 — Experiment Tracking',   component: 'DagShub MLflow',                     done: true },
            { module: '03 — Orchestration',         component: 'Prefect Cloud + GitHub Actions',     done: true },
            { module: '04 — Serving',               component: 'Render Go API + Next.js dashboard', done: true },
            { module: '05 — Monitoring',            component: 'Evidently → GH Pages · Grafana',    done: false },
            { module: '06 — Best Practices',        component: 'Makefile · CI · pre-commit · tests', done: false },
          ].map(({ module, component, done }) => (
            <div key={module} className="flex items-start gap-2">
              <span className="text-xs mt-0.5 flex-shrink-0" style={{ color: done ? '#5a9e52' : '#7a6a45' }}>
                {done ? '✓' : '○'}
              </span>
              <div>
                <p className="text-xs font-mono" style={{ color: done ? '#c8b78a' : '#7a6a45' }}>{module}</p>
                <p className="text-[0.65rem] text-dim">{component}</p>
              </div>
            </div>
          ))}
        </div>
      </div>
    </div>
  );
}
