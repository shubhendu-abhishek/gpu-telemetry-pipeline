#!/usr/bin/env python3
"""Validate the assignment's bundled DCGM CSV fixture using only the stdlib."""

from __future__ import annotations

import argparse
import csv
import math
import sys
from collections import Counter
from datetime import datetime
from pathlib import Path

EXPECTED_HEADER = (
    "timestamp",
    "metric_name",
    "gpu_id",
    "device",
    "uuid",
    "modelName",
    "Hostname",
    "container",
    "pod",
    "namespace",
    "value",
    "labels_raw",
)
EXPECTED_METRICS = {
    "DCGM_FI_DEV_GPU_UTIL",
    "DCGM_FI_DEV_MEM_COPY_UTIL",
    "DCGM_FI_DEV_POWER_USAGE",
    "DCGM_FI_DEV_GPU_TEMP",
    "DCGM_FI_DEV_FB_USED",
    "DCGM_FI_DEV_FB_FREE",
    "DCGM_FI_DEV_DEC_UTIL",
    "DCGM_FI_DEV_ENC_UTIL",
    "DCGM_FI_DEV_SM_CLOCK",
    "DCGM_FI_DEV_MEM_CLOCK",
}
EXPECTED_ROWS = 2470
EXPECTED_GPUS = 247
EXPECTED_HOSTS = 31
EXPECTED_ROWS_PER_METRIC = 247
REQUIRED_FIELDS = (
    "timestamp", "metric_name", "gpu_id", "device",
    "uuid", "modelName", "Hostname", "value",
)


def fail(message: str) -> None:
    raise ValueError(message)


def parse_timestamp(value: str, line: int) -> None:
    try:
        parsed = datetime.fromisoformat(value.replace("Z", "+00:00"))
    except ValueError as exc:
        fail(f"line {line}: invalid timestamp {value!r}: {exc}")
    if parsed.tzinfo is None:
        fail(f"line {line}: timestamp must include a timezone: {value!r}")


def validate(path: Path) -> dict[str, object]:
    if not path.is_file():
        fail(f"CSV file not found: {path}")

    metric_counts: Counter[str] = Counter()
    gpu_uuids: set[str] = set()
    hostnames: set[str] = set()
    gpu_ids: set[str] = set()
    unique_keys: set[tuple[str, str, str]] = set()
    models: set[str] = set()
    row_count = 0

    with path.open("r", encoding="utf-8-sig", newline="") as csv_file:
        reader = csv.reader(csv_file, strict=True)
        try:
            header = next(reader)
        except StopIteration:
            fail("CSV is empty")
        if tuple(header) != EXPECTED_HEADER:
            fail(f"unexpected CSV header: {header!r}")

        for line, values in enumerate(reader, start=2):
            if not values:
                fail(f"line {line}: blank CSV record")
            if len(values) != len(EXPECTED_HEADER):
                fail(
                    f"line {line}: expected {len(EXPECTED_HEADER)} fields, "
                    f"found {len(values)}"
                )
            row = dict(zip(EXPECTED_HEADER, values, strict=True))
            for field in REQUIRED_FIELDS:
                if not row[field].strip():
                    fail(f"line {line}: required field {field!r} is empty")

            parse_timestamp(row["timestamp"], line)
            try:
                metric_value = float(row["value"])
            except ValueError:
                fail(f"line {line}: value is not numeric: {row['value']!r}")
            if not math.isfinite(metric_value):
                fail(f"line {line}: value must be finite")

            key = (row["timestamp"], row["metric_name"], row["uuid"])
            if key in unique_keys:
                fail(f"line {line}: duplicate (timestamp, metric_name, uuid) key: {key!r}")
            unique_keys.add(key)
            metric_counts[row["metric_name"]] += 1
            gpu_uuids.add(row["uuid"])
            hostnames.add(row["Hostname"])
            gpu_ids.add(row["gpu_id"])
            models.add(row["modelName"])
            row_count += 1

    if row_count != EXPECTED_ROWS:
        fail(f"expected {EXPECTED_ROWS} data rows, found {row_count}")
    if set(metric_counts) != EXPECTED_METRICS:
        missing = sorted(EXPECTED_METRICS - set(metric_counts))
        extra = sorted(set(metric_counts) - EXPECTED_METRICS)
        fail(f"metric set mismatch: missing={missing}, extra={extra}")
    wrong_counts = {
        name: count
        for name, count in metric_counts.items()
        if count != EXPECTED_ROWS_PER_METRIC
    }
    if wrong_counts:
        fail(
            f"expected {EXPECTED_ROWS_PER_METRIC} rows per metric; "
            f"found {wrong_counts}"
        )
    if len(gpu_uuids) != EXPECTED_GPUS:
        fail(f"expected {EXPECTED_GPUS} unique GPU UUIDs, found {len(gpu_uuids)}")
    if len(hostnames) != EXPECTED_HOSTS:
        fail(f"expected {EXPECTED_HOSTS} unique hosts, found {len(hostnames)}")
    if gpu_ids != {str(value) for value in range(8)}:
        fail(f"expected GPU indices 0 through 7, found {sorted(gpu_ids)}")
    if len(models) != 1 or "NVIDIA H100 80GB HBM3" not in models:
        fail(f"unexpected GPU model set: {sorted(models)}")

    return {
        "rows": row_count,
        "metrics": len(metric_counts),
        "gpus": len(gpu_uuids),
        "hosts": len(hostnames),
        "gpu_indices": ",".join(sorted(gpu_ids, key=int)),
        "model": next(iter(models)),
    }


def main() -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument(
        "--csv",
        type=Path,
        default=Path("data/dcgm_metrics_20250718_134233.csv"),
        help="CSV fixture to validate",
    )
    args = parser.parse_args()
    try:
        result = validate(args.csv)
    except (OSError, UnicodeError, csv.Error, ValueError) as exc:
        print(f"DATASET VALIDATION FAILED: {exc}", file=sys.stderr)
        return 1

    print("DATASET VALIDATION PASSED")
    for key, value in result.items():
        print(f"{key}: {value}")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
