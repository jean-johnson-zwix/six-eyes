## Table `alembic_version`

### Columns

| Name | Type | Constraints |
|------|------|-------------|
| `version_num` | `varchar` | Primary |

## Table `datasets`

### Columns

| Name | Type | Constraints |
|------|------|-------------|
| `dataset_uuid` | `varchar` |  |
| `experiment_id` | `int4` | Primary |
| `name` | `varchar` | Primary |
| `digest` | `varchar` | Primary |
| `dataset_source_type` | `varchar` |  |
| `dataset_source` | `text` |  |
| `dataset_schema` | `text` |  Nullable |
| `dataset_profile` | `text` |  Nullable |

## Table `experiment_tags`

### Columns

| Name | Type | Constraints |
|------|------|-------------|
| `key` | `varchar` | Primary |
| `value` | `varchar` |  Nullable |
| `experiment_id` | `int4` | Primary |

## Table `experiments`

### Columns

| Name | Type | Constraints |
|------|------|-------------|
| `experiment_id` | `int4` | Primary |
| `name` | `varchar` |  Unique |
| `artifact_location` | `varchar` |  Nullable |
| `lifecycle_stage` | `varchar` |  Nullable |
| `creation_time` | `int8` |  Nullable |
| `last_update_time` | `int8` |  Nullable |

## Table `input_tags`

### Columns

| Name | Type | Constraints |
|------|------|-------------|
| `input_uuid` | `varchar` | Primary |
| `name` | `varchar` | Primary |
| `value` | `varchar` |  |

## Table `inputs`

### Columns

| Name | Type | Constraints |
|------|------|-------------|
| `input_uuid` | `varchar` |  |
| `source_type` | `varchar` | Primary |
| `source_id` | `varchar` | Primary |
| `destination_type` | `varchar` | Primary |
| `destination_id` | `varchar` | Primary |

## Table `latest_metrics`

### Columns

| Name | Type | Constraints |
|------|------|-------------|
| `key` | `varchar` | Primary |
| `value` | `float8` |  |
| `timestamp` | `int8` |  Nullable |
| `step` | `int8` |  |
| `is_nan` | `bool` |  |
| `run_uuid` | `varchar` | Primary |

## Table `metrics`

### Columns

| Name | Type | Constraints |
|------|------|-------------|
| `key` | `varchar` | Primary |
| `value` | `float8` | Primary |
| `timestamp` | `int8` | Primary |
| `run_uuid` | `varchar` | Primary |
| `step` | `int8` | Primary |
| `is_nan` | `bool` | Primary |

## Table `model_version_tags`

### Columns

| Name | Type | Constraints |
|------|------|-------------|
| `key` | `varchar` | Primary |
| `value` | `varchar` |  Nullable |
| `name` | `varchar` | Primary |
| `version` | `int4` | Primary |

## Table `model_versions`

### Columns

| Name | Type | Constraints |
|------|------|-------------|
| `name` | `varchar` | Primary |
| `version` | `int4` | Primary |
| `creation_time` | `int8` |  Nullable |
| `last_updated_time` | `int8` |  Nullable |
| `description` | `varchar` |  Nullable |
| `user_id` | `varchar` |  Nullable |
| `current_stage` | `varchar` |  Nullable |
| `source` | `varchar` |  Nullable |
| `run_id` | `varchar` |  Nullable |
| `status` | `varchar` |  Nullable |
| `status_message` | `varchar` |  Nullable |
| `run_link` | `varchar` |  Nullable |
| `storage_location` | `varchar` |  Nullable |

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

## Table `params`

### Columns

| Name | Type | Constraints |
|------|------|-------------|
| `key` | `varchar` | Primary |
| `value` | `varchar` |  |
| `run_uuid` | `varchar` | Primary |

## Table `registered_model_aliases`

### Columns

| Name | Type | Constraints |
|------|------|-------------|
| `alias` | `varchar` | Primary |
| `version` | `int4` |  |
| `name` | `varchar` | Primary |

## Table `registered_model_tags`

### Columns

| Name | Type | Constraints |
|------|------|-------------|
| `key` | `varchar` | Primary |
| `value` | `varchar` |  Nullable |
| `name` | `varchar` | Primary |

## Table `registered_models`

### Columns

| Name | Type | Constraints |
|------|------|-------------|
| `name` | `varchar` | Primary |
| `creation_time` | `int8` |  Nullable |
| `last_updated_time` | `int8` |  Nullable |
| `description` | `varchar` |  Nullable |

## Table `runs`

### Columns

| Name | Type | Constraints |
|------|------|-------------|
| `run_uuid` | `varchar` | Primary |
| `name` | `varchar` |  Nullable |
| `source_type` | `varchar` |  Nullable |
| `source_name` | `varchar` |  Nullable |
| `entry_point_name` | `varchar` |  Nullable |
| `user_id` | `varchar` |  Nullable |
| `status` | `varchar` |  Nullable |
| `start_time` | `int8` |  Nullable |
| `end_time` | `int8` |  Nullable |
| `source_version` | `varchar` |  Nullable |
| `lifecycle_stage` | `varchar` |  Nullable |
| `artifact_uri` | `varchar` |  Nullable |
| `experiment_id` | `int4` |  Nullable |
| `deleted_time` | `int8` |  Nullable |

## Table `tags`

### Columns

| Name | Type | Constraints |
|------|------|-------------|
| `key` | `varchar` | Primary |
| `value` | `varchar` |  Nullable |
| `run_uuid` | `varchar` | Primary |

## Table `trace_info`

### Columns

| Name | Type | Constraints |
|------|------|-------------|
| `request_id` | `varchar` | Primary |
| `experiment_id` | `int4` |  |
| `timestamp_ms` | `int8` |  |
| `execution_time_ms` | `int8` |  Nullable |
| `status` | `varchar` |  |

## Table `trace_request_metadata`

### Columns

| Name | Type | Constraints |
|------|------|-------------|
| `key` | `varchar` | Primary |
| `value` | `varchar` |  Nullable |
| `request_id` | `varchar` | Primary |

## Table `trace_tags`

### Columns

| Name | Type | Constraints |
|------|------|-------------|
| `key` | `varchar` | Primary |
| `value` | `varchar` |  Nullable |
| `request_id` | `varchar` | Primary |

