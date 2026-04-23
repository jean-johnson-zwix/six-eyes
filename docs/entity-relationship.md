## Table `papers`

### Columns

| Name | Type | Constraints |
|------|------|-------------|
| `id` | `uuid` | Primary |
| `arxiv_id` | `text` |  Unique |
| `title` | `text` |  Nullable |
| `abstract` | `text` |  Nullable |
| `categories` | `_text` |  Nullable |
| `authors` | `jsonb` |  Nullable |
| `submitted_at` | `timestamptz` |  Nullable |
| `updated_at_api` | `timestamptz` |  Nullable |
| `ss_paper_id` | `text` |  Nullable |
| `citation_count` | `int4` |  Nullable |
| `max_h_index` | `int4` |  Nullable |
| `total_prior_papers` | `int4` |  Nullable |
| `has_code` | `bool` |  Nullable |
| `github_stars_t60` | `int4` |  Nullable |
| `hype_label` | `bool` |  Nullable |
| `ingested_at` | `timestamptz` |  Nullable |
| `enriched_at` | `timestamptz` |  Nullable |
| `hf_paper_id` | `text` |  Nullable |
| `hf_upvotes` | `int4` |  Nullable |
| `hf_github_repo` | `text` |  Nullable |

