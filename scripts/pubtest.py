#!/usr/bin/env python3
"""Public-site benchmark: fast vs deep mode speed + per-column coverage + intel quality."""
import csv
import io
import json
import sys
import time
import urllib.parse
import urllib.request

BASE = "http://124.222.208.237:8080"

DEEP_COLUMNS = [
    "address", "whatsapp", "emails", "phone", "website",
    "facebook", "instagram", "linkedin", "twitter", "tiktok", "youtube",
    "telegram", "pinterest", "category", "review_rating", "review_count",
    "reviews_per_rating", "open_hours", "popular_times", "price_range",
    "status", "descriptions", "about", "menu", "owner", "images",
    "thumbnail", "reviews_link", "user_reviews", "user_reviews_extended",
    "plus_code", "timezone", "complete_address", "credit_cards_accepted",
    "reservations", "order_online", "street_view_url", "data_id",
]


def http(method, path, data=None, timeout=120, raw=False):
    url = path if path.startswith("http") else BASE + path
    body = None
    headers = {"User-Agent": "pubtest/1.0"}
    if data is not None:
        body = urllib.parse.urlencode(data).encode()
        headers["Content-Type"] = "application/x-www-form-urlencoded"
    req = urllib.request.Request(url, data=body, headers=headers, method=method)
    with urllib.request.urlopen(req, timeout=timeout) as r:
        payload = r.read()
    return payload if raw else payload.decode("utf-8", "replace")


def submit(name, fast, radius_km, maxresults, intel, keywords="cafe",
           lat="-6.2088", lon="106.8456", country=("id", "Indonesia", "id"),
           locations="Jakarta"):
    cc, cn, lang = country
    form = {
        "name": name,
        "keywords": keywords,
        "locations": locations,
        "lang": lang,
        "country_code": cc,
        "country_name": cn,
        "ai_translate": "on",
        "zoom": "15",
        "depth": "10",
        "maxtime": "3600",
        "latitude": lat,
        "longitude": lon,
        "fastmode": "on" if fast else "",
        "gridmode": "",
        "radius_km": str(radius_km),
        "radius": str(radius_km * 1000),
        "columns": "" if fast else ",".join(DEEP_COLUMNS),
        "email": "on",
        "enable_intel": "on" if intel else "",
        "maxresults": str(maxresults),
    }
    before = {j["id"] for j in json.loads(http("GET", "/api/v1/jobs"))}
    t0 = time.time()
    http("POST", "/scrape", data=form)
    submit_ms = (time.time() - t0) * 1000
    for _ in range(30):
        jobs = json.loads(http("GET", "/api/v1/jobs"))
        new = [j for j in jobs if j["id"] not in before]
        if new:
            return new[0]["id"], t0, submit_ms
        time.sleep(1)
    raise RuntimeError("job id not found after submit")


def count_places(job_id):
    try:
        data = json.loads(http("GET", f"/api/v1/jobs/{job_id}/places", timeout=60))
    except Exception:
        return 0
    if isinstance(data, dict):
        for k in ("places", "results", "data"):
            if isinstance(data.get(k), list):
                return len(data[k])
        return 0
    return len(data) if isinstance(data, list) else 0


def wait_job(job_id, t0, limit_s):
    """Poll until job leaves working/pending. Records count at ~60s."""
    at60 = None
    status = "unknown"
    while time.time() - t0 < limit_s:
        try:
            job = json.loads(http("GET", f"/api/v1/jobs/{job_id}", timeout=60))
            status = job.get("status", "unknown")
        except Exception as e:
            print(f"   poll error: {e}", flush=True)
            time.sleep(5)
            continue
        el = time.time() - t0
        if at60 is None and el >= 60:
            at60 = count_places(job_id)
            print(f"   [60s] count={at60}", flush=True)
        if status not in ("working", "pending", "queued"):
            return status, time.time() - t0, at60
        time.sleep(5)
    return status, time.time() - t0, at60


def coverage(job_id):
    try:
        blob = http("GET", f"/api/v1/jobs/{job_id}/download", timeout=180, raw=True)
    except Exception as e:
        return None, 0, {}, str(e)
    text = blob.decode("utf-8", "replace")
    rows = list(csv.DictReader(io.StringIO(text)))
    if not rows:
        return text, 0, {}, "empty csv"
    headers = list(rows[0].keys())
    cov = {}
    for h in headers:
        n = sum(1 for r in rows if (r.get(h) or "").strip() not in ("", "[]", "{}", "0001-01-01", "null"))
        cov[h] = round(100.0 * n / len(rows), 1)
    return text, len(rows), cov, None


def run(label, **kw):
    print(f"\n{'='*70}\n### {label}\n{'='*70}", flush=True)
    job_id, t0, submit_ms = submit(label, **kw)
    print(f"job={job_id} submit_latency={submit_ms:.0f}ms", flush=True)
    status, elapsed, at60 = wait_job(job_id, t0, kw.pop("limit_s", 900))
    csv_text, n, cov, err = coverage(job_id)
    rate = (n / elapsed * 60) if elapsed > 0 else 0
    print(f"status={status} elapsed={elapsed:.0f}s rows={n} rate={rate:.1f}/min at60s={at60}", flush=True)
    if err:
        print(f"csv error: {err}", flush=True)
    return {
        "label": label, "job_id": job_id, "status": status,
        "submit_ms": round(submit_ms), "elapsed_s": round(elapsed),
        "rows": n, "rate_per_min": round(rate, 1), "count_at_60s": at60,
        "coverage": cov, "csv_error": err,
    }


if __name__ == "__main__":
    which = sys.argv[1] if len(sys.argv) > 1 else "all"
    out = {}
    if which in ("all", "fast"):
        out["fast"] = run("PUBTEST-FAST", fast=True, radius_km=10, maxresults=60,
                          intel=False, limit_s=900)
    if which in ("all", "deep"):
        out["deep"] = run("PUBTEST-DEEP", fast=False, radius_km=10, maxresults=50,
                          intel=True, limit_s=1200)
    with open(f"/tmp/pubtest_{which}.json", "w") as f:
        json.dump(out, f, indent=2, ensure_ascii=False)
    print(f"\nsaved /tmp/pubtest_{which}.json", flush=True)
