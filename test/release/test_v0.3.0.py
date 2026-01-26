# Copyright (C) 2025 Aleutian AI (jinterlante@aleutian.ai)
# This program is free software: you can redistribute it and/or modify
# it under the terms of the GNU Affero General Public License as published by
# the Free Software Foundation, either version 3 of the License, or
# (at your option) any later version.
# See the LICENSE.txt file for the full license text.
#
# NOTE: This work is subject to additional terms under AGPL v3 Section 7.
# See the NOTICE.txt file for details regarding AI system attribution.

import requests
import sys

BASE_URL = "http://localhost:8080"  # Or whatever your exposed port is


def log(msg, status):
    print(f"[{status}] {msg}")


def check_health():
    try:
        r = requests.get(f"{BASE_URL}/health")
        if r.status_code == 200:
            log("System Health", "PASS")
        else:
            log(f"System Health ({r.status_code})", "FAIL")
            sys.exit(1)
    except Exception as e:
        log(f"Connection Failed: {e}", "FAIL")
        sys.exit(1)


def check_timeseries_models():
    # Simple sine wave data
    payload = {
        "history": [0.1, 0.4, 0.8, 0.9, 0.5, 0.1, -0.2],
        "horizon": 3
    }

    models = ["timesfm-1.0-200m", "chronos-t5-small"]  # Add others

    for m in models:
        payload["model"] = m
        try:
            # Assuming you have a unified endpoint
            r = requests.post(f"{BASE_URL}/v1/timeseries/forecast", json=payload)
            if r.status_code == 200 and "forecast" in r.json():
                log(f"Model Inference: {m}", "PASS")
            else:
                log(f"Model Inference: {m} - {r.text}", "FAIL")
        except Exception as e:
            log(f"Model Crash: {m}", "FAIL")


def check_vector_db():
    try:
        # Hit a known endpoint that queries Weaviate
        r = requests.get(f"{BASE_URL}/v1/rag/status")
        if r.status_code == 200:
            log("Weaviate Connection", "PASS")
        else:
            log("Weaviate Connection", "FAIL")
    except:
        log("Weaviate Unreachable", "FAIL")


if __name__ == "__main__":
    print("--- Aleutian v0.3.0 Release Candidate Check ---")
    check_health()
    check_vector_db()
    check_timeseries_models()
    print("--- Done ---")