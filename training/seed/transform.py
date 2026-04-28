"""
transform.py — filter ArXiv Kaggle dump + join PwC links → papers_seed.parquet

Inputs (set via env vars or edit paths below):
    ARXIV_JSON   path to arxiv-metadata-oai-snapshot.json  (~3.5 GB NDJSON)
    PWC_JSON     path to links-between-papers-and-code.json (PwC GitHub dump)

Output:
    papers_seed.parquet  (DVC-tracked pipeline artifact)

Run:
    python transform.py

Label strategy:
    Papers present in PwC links with is_official=true → has_code=True, hype_label=True
    Papers absent from PwC                            → has_code=False, hype_label=False
    This is a bootstrap proxy; cmd/label overwrites hype_label with github_stars_t60 ground truth.
"""

import os
import duckdb

ARXIV_JSON  = os.getenv("ARXIV_JSON",  "raw_data/arxiv-metadata-oai-snapshot.json")
PWC_JSON    = os.getenv("PWC_JSON",    "raw_data/links-between-paper-and-code.parquet")
OUT_PARQUET = os.getenv("OUT_PARQUET", "papers_seed.parquet")

SQL = f"""
COPY (
    WITH arxiv_raw AS (
        SELECT
            regexp_replace(id, 'v[0-9]+$', '')        AS arxiv_id,
            trim(title)                                 AS title,
            trim(abstract)                              AS abstract,
            string_split(trim(categories), ' ')         AS categories,
            -- versions is 1-indexed in DuckDB. versions[1] is the original submission.
            -- COALESCE handles the rare records that omit the weekday prefix.
            COALESCE(
                TRY_STRPTIME(versions[1].created, '%a, %d %b %Y %H:%M:%S %Z'),
                TRY_STRPTIME(versions[1].created, '%d %b %Y %H:%M:%S %Z')
            )                                           AS submitted_at,
            CAST(update_date AS TIMESTAMP)              AS updated_at_api,
            -- struct_pack produces {{"name": "First Last"}} objects that map
            -- directly to the papers.authors JSONB schema.
            COALESCE(
                list_transform(
                    authors_parsed,
                    x -> struct_pack(
                        name := CASE
                            WHEN trim(x[2]) = '' THEN trim(x[1])
                            ELSE trim(x[2]) || ' ' || trim(x[1])
                        END
                    )
                ),
                []
            )                                           AS authors
        FROM read_ndjson('{ARXIV_JSON}', ignore_errors := true)
    ),

    -- Filter to the four target categories and 2024+ only.
    -- Done in a second CTE so the WHERE clause can reference the submitted_at alias.
    arxiv AS (
        SELECT * FROM arxiv_raw
        WHERE submitted_at >= '2024-01-01'
          AND (
               list_contains(categories, 'cs.LG')
            OR list_contains(categories, 'cs.AI')
            OR list_contains(categories, 'cs.CV')
            OR list_contains(categories, 'cs.CL')
          )
    ),

    -- One row per arxiv paper from PwC (GROUP BY deduplicates papers with
    -- multiple linked repos). Only official repos count as has_code.
    -- paper_arxiv_id is a direct column — no URL parsing needed.
    pwc AS (
        SELECT
            paper_arxiv_id          AS arxiv_id,
            ANY_VALUE(repo_url)     AS hf_github_repo
        FROM read_parquet('{PWC_JSON}')
        WHERE is_official = true
          AND paper_arxiv_id IS NOT NULL
          AND paper_arxiv_id != ''
        GROUP BY 1
    )

    SELECT
        a.arxiv_id,
        a.title,
        a.abstract,
        a.categories,
        a.submitted_at,
        a.updated_at_api,
        a.authors,
        (pwc.arxiv_id IS NOT NULL) AS has_code,
        pwc.hf_github_repo,
        (pwc.arxiv_id IS NOT NULL) AS hype_label
    FROM arxiv a
    LEFT JOIN pwc ON a.arxiv_id = pwc.arxiv_id

) TO '{OUT_PARQUET}' (FORMAT PARQUET);
"""

print(f"Reading {ARXIV_JSON} ...")
print(f"Joining {PWC_JSON} ...")

con = duckdb.connect()
con.execute(SQL)

total, positive, negative = con.execute(f"""
    SELECT
        COUNT(*)                      AS total,
        SUM(hype_label::int)          AS positive,
        COUNT(*) - SUM(hype_label::int) AS negative
    FROM '{OUT_PARQUET}'
""").fetchone()

print(f"Done.")
print(f"  Output : {OUT_PARQUET}")
print(f"  Total  : {total:,} papers")
print(f"  Positive (hype) : {positive:,} ({positive/total:.1%})")
print(f"  Negative        : {negative:,} ({negative/total:.1%})")
