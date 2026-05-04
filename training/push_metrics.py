"""
push_metrics.py — push Prometheus metrics to Grafana Cloud via remote-write.

Implements a minimal proto3 encoder for the Prometheus WriteRequest wire format
without requiring the `protobuf` package. Uses raw (block) snappy compression,
which is what Prometheus remote-write expects.

Usage
-----
    from push_metrics import push

    push([
        ({'__name__': 'six_eyes_mean_hype_score'}, 0.142),
        ({'__name__': 'six_eyes_drift_share'},      0.33),
        ({'__name__': 'six_eyes_feature_drift_score', 'feature': 'max_h_index'}, 0.07),
    ])

Env vars (or pass kwargs)
-------------------------
    GRAFANA_REMOTE_WRITE_URL
    GRAFANA_USERNAME
    GRAFANA_PASSWORD

Dependencies (add to monitor job pip install)
---------------------------------------------
    requests
    python-snappy
"""

from __future__ import annotations

import base64
import os
import struct
import time
from typing import Sequence

import requests


# ── Minimal proto3 wire-format encoder ────────────────────────────────────────
# Prometheus WriteRequest schema (prometheus/prompb/types.proto):
#
#   WriteRequest  { repeated TimeSeries timeseries = 1; }
#   TimeSeries    { repeated Label labels = 1; repeated Sample samples = 2; }
#   Label         { string name = 1; string value = 2; }
#   Sample        { double value = 1; int64 timestamp = 2; }
#
# Wire types used: 0 = varint, 1 = 64-bit, 2 = length-delimited


def _varint(n: int) -> bytes:
    if n == 0:
        return b'\x00'
    buf = []
    while n:
        bits = n & 0x7F
        n >>= 7
        buf.append(bits | 0x80 if n else bits)
    return bytes(buf)


def _ld(field: int, data: bytes) -> bytes:
    """Wire type 2: length-delimited field."""
    return _varint((field << 3) | 2) + _varint(len(data)) + data


def _f64(field: int, v: float) -> bytes:
    """Wire type 1: 64-bit little-endian double."""
    return _varint((field << 3) | 1) + struct.pack('<d', v)


def _vi(field: int, n: int) -> bytes:
    """Wire type 0: varint (positive int64 only — sufficient for timestamps)."""
    return _varint((field << 3) | 0) + _varint(n)


def _encode_label(name: str, value: str) -> bytes:
    """Label message wrapped as TimeSeries.labels (field 1) repeated entry."""
    body = _ld(1, name.encode()) + _ld(2, value.encode())
    return _ld(1, body)


def _encode_sample(value: float, ts_ms: int) -> bytes:
    """Sample message wrapped as TimeSeries.samples (field 2) repeated entry."""
    body = _f64(1, value) + _vi(2, ts_ms)
    return _ld(2, body)


def _encode_timeseries(labels: dict[str, str], value: float, ts_ms: int) -> bytes:
    """TimeSeries message wrapped as WriteRequest.timeseries (field 1)."""
    # Prometheus requires labels sorted: __name__ first, then lexicographic
    sorted_labels = sorted(labels.items(), key=lambda kv: (kv[0] != '__name__', kv[0]))
    body = b''.join(_encode_label(k, v) for k, v in sorted_labels)
    body += _encode_sample(value, ts_ms)
    return _ld(1, body)


# ── Public API ─────────────────────────────────────────────────────────────────


def push(
    metrics: Sequence[tuple[dict[str, str], float]],
    *,
    url: str | None = None,
    username: str | None = None,
    password: str | None = None,
) -> None:
    """Push (labels_dict, value) pairs to Grafana Cloud.

    Every labels dict must contain '__name__'. All metrics share the
    current timestamp (suitable for a batch push at the end of a job).

    Raises requests.HTTPError on non-2xx responses.
    """
    url      = url      or os.environ["GRAFANA_REMOTE_WRITE_URL"]
    username = username or os.environ["GRAFANA_USERNAME"]
    password = password or os.environ["GRAFANA_PASSWORD"]

    try:
        import snappy
    except ImportError as e:
        raise ImportError("pip install python-snappy") from e

    ts_ms   = int(time.time() * 1000)
    payload = b''.join(_encode_timeseries(lbs, val, ts_ms) for lbs, val in metrics)

    # Prometheus remote-write requires raw (block) snappy, not the framing format.
    # python-snappy >= 0.6 exposes raw_compress(); older versions compress() is raw.
    compress_fn = getattr(snappy, 'raw_compress', snappy.compress)
    compressed  = compress_fn(payload)

    creds = base64.b64encode(f"{username}:{password}".encode()).decode()
    resp  = requests.post(
        url,
        data=compressed,
        headers={
            "Content-Type":                      "application/x-protobuf",
            "Content-Encoding":                  "snappy",
            "X-Prometheus-Remote-Write-Version":  "0.1.0",
            "Authorization":                     f"Basic {creds}",
        },
        timeout=30,
    )
    resp.raise_for_status()
    print(f"  Grafana push OK: {len(metrics)} series → HTTP {resp.status_code}")
