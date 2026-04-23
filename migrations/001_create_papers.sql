CREATE EXTENSION IF NOT EXISTS "pgcrypto";

CREATE TABLE IF NOT EXISTS papers (
    id                  UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    arxiv_id            TEXT        UNIQUE NOT NULL,
    title               TEXT,
    abstract            TEXT,
    categories          TEXT[],
    authors             JSONB,
    submitted_at        TIMESTAMPTZ,
    updated_at_api      TIMESTAMPTZ,

    -- Semantic Scholar enrichment
    ss_paper_id         TEXT,
    citation_count      INTEGER,
    max_h_index         INTEGER,
    total_prior_papers  INTEGER,

    -- HuggingFace enrichment
    has_code            BOOLEAN     DEFAULT FALSE,
    hf_paper_id         TEXT,
    hf_upvotes          INTEGER,
    hf_github_repo      TEXT,

    -- Target labels (populated by T+60 label job)
    github_stars_t60    INTEGER,
    hype_label          BOOLEAN,

    -- Metadata
    ingested_at         TIMESTAMPTZ DEFAULT NOW(),
    enriched_at         TIMESTAMPTZ
);

CREATE INDEX IF NOT EXISTS idx_papers_submitted_at
    ON papers (submitted_at DESC);

-- Partial index for Phase 2 slow-track: papers not yet hydrated by HuggingFace.
CREATE INDEX IF NOT EXISTS idx_papers_hf_paper_id
    ON papers (arxiv_id)
    WHERE hf_paper_id IS NOT NULL;

-- Partial index for the re-enrichment job: only rows needing enrichment.
CREATE INDEX IF NOT EXISTS idx_papers_needs_enrichment
    ON papers (ingested_at)
    WHERE enriched_at IS NULL;
