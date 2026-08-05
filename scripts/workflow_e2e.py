#!/usr/bin/env python3
"""Local workflow E2E: agent pipeline + deep full-volume speed/quality + concurrency."""
from __future__ import annotations

import json
import time
import urllib.error
import urllib.parse
import urllib.request

BASE = "http://127.0.0.1:18080"


def http(method, path, data=None, json_body=None, timeout=120):
    url = path if path.startswith("http") else BASE + path
    body = None
    headers = {"User-Agent": "workflow-e2e/1.0"}
    if json_body is not None:
        body = json.dumps(json_body).encode()
        headers["Content-Type"] = "application/json"
    elif data is not None:
        body = urllib.parse.urlencode(data).encode()
        headers["Content-Type"] = "application/x-www-form-urlencoded"
    req = urllib.request.Request(url, data=body, headers=headers, method=method)
    with urllib.request.urlopen(req, timeout=timeout) as r:
        raw = r.read()
        ctype = r.headers.get("Content-Type", "")
        code = r.status
    if "json" in ctype or path.startswith("/api/"):
        try:
            return code, json.loads(raw.decode("utf-8", "replace"))
        except Exception:
            return code, raw.decode("utf-8", "replace")
    return code, raw.decode("utf-8", "replace")


def places_count(job_id):
    try:
        code, data = http("GET", f"/api/v1/jobs/{job_id}/places/count", timeout=30)
        if code == 200 and isinstance(data, dict):
            return int(data.get("count") or data.get("total") or 0)
    except Exception:
        pass
    try:
        code, data = http("GET", f"/api/v1/jobs/{job_id}/places?lite=1", timeout=60)
        if isinstance(data, dict):
            for k in ("places", "results", "data"):
                if isinstance(data.get(k), list):
                    return len(data[k])
        if isinstance(data, list):
            return len(data)
    except Exception:
        pass
    return 0


def get_places(job_id):
    try:
        code, data = http("GET", f"/api/v1/jobs/{job_id}/places", timeout=90)
        if isinstance(data, dict):
            for k in ("places", "results", "data"):
                if isinstance(data.get(k), list):
                    return data[k]
        if isinstance(data, list):
            return data
    except Exception as e:
        print(f"  places fetch error: {e}")
    return []


def job_status(job_id):
    code, data = http("GET", f"/api/v1/jobs/{job_id}", timeout=30)
    if isinstance(data, dict):
        return data.get("status", "unknown"), data
    return "unknown", data


def coverage(places):
    n = len(places) or 1
    keys = ("phone", "emails", "website", "whatsapp", "address", "title", "name")
    stats = {k: 0 for k in keys}
    for p in places:
        if not isinstance(p, dict):
            continue
        for k in keys:
            v = p.get(k) or p.get(k.rstrip("s"))  # email vs emails
            if k == "name":
                v = p.get("title") or p.get("name")
            if v and str(v).strip() not in ("", "[]", "null", "None"):
                stats[k] += 1
    return {k: round(100.0 * v / n, 1) for k, v in stats.items()}, n


def section(title):
    print("\n" + "=" * 60)
    print(title)
    print("=" * 60, flush=True)


def main():
    report = {"base": BASE, "ts": time.strftime("%Y-%m-%dT%H:%M:%SZ", time.gmtime())}

    section("1) UI / health")
    code, _ = http("GET", "/")
    print(f"GET / => {code}")
    html = urllib.request.urlopen(BASE + "/", timeout=30).read().decode("utf-8", "replace")
    checks = {
        "intent_box": "intent_goal" in html,
        "no_quick_mode_card": 'data-mode="quick"' not in html or "mode-fixed-banner" in html,
        "lang_select": "ui-lang-select" in html,
        "radius_required": "radius_km" in html,
        "deep_full_banner": "mode-fixed-banner" in html or "深度全量" in html,
        "no_proxies_textarea": '<textarea' not in html.lower() or 'name="proxies"' not in html,
    }
    for k, v in checks.items():
        print(f"  {k}: {'OK' if v else 'FAIL'}")
    report["ui"] = checks

    section("2) IntentAgent understand")
    t0 = time.time()
    code, plan = http(
        "POST",
        "/api/v1/agent/understand",
        json_body={"goal": "在雅加达找咖啡馆和进口商，半径8公里", "ui_lang": "zh"},
    )
    ms = (time.time() - t0) * 1000
    print(f"status={code} in {ms:.0f}ms")
    print(json.dumps(plan, ensure_ascii=False, indent=2)[:1200])
    report["understand_ms"] = round(ms, 1)
    report["understand"] = plan if isinstance(plan, dict) else str(plan)

    section("3) Dispatcher create deep-full jobs (no scrape yet cancel?)")
    # We'll create via form scrape for one timed deep job; agent dispatch creates pending jobs too.
    t0 = time.time()
    code, disp = http(
        "POST",
        "/api/v1/agent/dispatch",
        json_body={"goal": "在雅加达找咖啡馆，半径3公里", "ui_lang": "zh"},
    )
    ms = (time.time() - t0) * 1000
    print(f"status={code} in {ms:.0f}ms => {disp}")
    report["dispatch_ms"] = round(ms, 1)
    report["dispatch"] = disp if isinstance(disp, dict) else str(disp)

    section("4) Deep full-volume scrape smoke (radius 2km, timed)")
    # Small radius for measurable speed without running hours
    form = {
        "name": "e2e-deep-cafe-jakarta",
        "keywords": "cafe",
        "locations": "Jakarta",
        "lang": "id",
        "country_code": "id",
        "country_name": "Indonesia",
        "ai_translate": "",
        "zoom": "15",
        "depth": "50",
        "maxtime": "15m",
        "latitude": "-6.2088",
        "longitude": "106.8456",
        "fastmode": "on",  # must be ignored
        "gridmode": "",
        "radius_km": "2",
        "radius": "2000",
        "maxresults": "20",  # must be ignored
        "email": "on",
        "ui_lang": "zh",
        "enable_intel": "",
    }
    before = set()
    try:
        _, jobs = http("GET", "/api/v1/jobs")
        if isinstance(jobs, list):
            before = {j.get("id") for j in jobs if isinstance(j, dict)}
    except Exception:
        pass

    t_submit = time.time()
    code, body = http("POST", "/scrape", data=form, timeout=60)
    submit_ms = (time.time() - t_submit) * 1000
    print(f"POST /scrape => {code} in {submit_ms:.0f}ms")

    job_id = None
    for _ in range(40):
        _, jobs = http("GET", "/api/v1/jobs")
        if isinstance(jobs, list):
            for j in jobs:
                if j.get("id") not in before:
                    job_id = j["id"]
                    print(f"job={job_id} status={j.get('status')} fast={j.get('data',{}).get('fast_mode')} "
                          f"grid={j.get('data',{}).get('grid_mode')} max={j.get('data',{}).get('max_results')}")
                    break
        if job_id:
            break
        time.sleep(0.5)
    if not job_id:
        print("FAIL: no job id")
        report["scrape"] = {"error": "no job"}
        print(json.dumps(report, ensure_ascii=False, indent=2))
        return 1

    # Verify forced deep-full on created job
    st, jdata = job_status(job_id)
    data = jdata.get("data") or jdata.get("Data") or {}
    if isinstance(jdata, dict) and "data" not in jdata and "Data" not in jdata:
        # API may flatten
        data = jdata
    # Re-fetch raw job list for fields
    _, jobs = http("GET", "/api/v1/jobs")
    job_obj = next((j for j in jobs if j.get("id") == job_id), {})
    jd = job_obj.get("data") or {}
    print(f"forced policy: fast={jd.get('fast_mode')} grid={jd.get('grid_mode')} "
          f"max_results={jd.get('max_results')} radius={jd.get('radius')} depth={jd.get('depth')}")

    # Poll for progress up to 180s
    limit_s = 180
    t0 = time.time()
    at30 = at60 = at120 = None
    final_status = st
    while time.time() - t0 < limit_s:
        final_status, _ = job_status(job_id)
        n = places_count(job_id)
        elapsed = time.time() - t0
        if at30 is None and elapsed >= 30:
            at30 = n
            print(f"  t=30s places={n} status={final_status}", flush=True)
        if at60 is None and elapsed >= 60:
            at60 = n
            print(f"  t=60s places={n} status={final_status}", flush=True)
        if at120 is None and elapsed >= 120:
            at120 = n
            print(f"  t=120s places={n} status={final_status}", flush=True)
        if final_status in ("ok", "failed", "canceled"):
            print(f"  done status={final_status} places={n} elapsed={elapsed:.1f}s", flush=True)
            break
        time.sleep(5)
    else:
        n = places_count(job_id)
        print(f"  timeout status={final_status} places={n}", flush=True)

    places = get_places(job_id)
    cov, nplaces = coverage(places)
    elapsed = time.time() - t0
    rate = (nplaces / elapsed * 60) if elapsed > 0 else 0
    print(f"quality coverage %: {cov}")
    print(f"speed: {nplaces} places in {elapsed:.1f}s => {rate:.1f}/min")

    report["scrape"] = {
        "job_id": job_id,
        "submit_ms": round(submit_ms, 1),
        "status": final_status,
        "elapsed_s": round(elapsed, 1),
        "places": nplaces,
        "places_at_30s": at30,
        "places_at_60s": at60,
        "places_at_120s": at120,
        "places_per_min": round(rate, 1),
        "coverage_pct": cov,
        "policy": {
            "fast_mode": jd.get("fast_mode"),
            "grid_mode": jd.get("grid_mode"),
            "max_results": jd.get("max_results"),
            "radius": jd.get("radius"),
            "depth": jd.get("depth"),
        },
        "sample": places[:3] if places else [],
    }

    section("5) Concurrency: fire 4 deep jobs, observe parallel working")
    ids = []
    t_conc = time.time()
    for i in range(4):
        f = dict(form)
        f["name"] = f"e2e-conc-{i}"
        f["radius_km"] = "1"
        f["radius"] = "1000"
        f["keywords"] = "kopi" if i % 2 == 0 else "cafe"
        f["latitude"] = str(-6.20 - i * 0.01)
        f["longitude"] = str(106.84 + i * 0.01)
        http("POST", "/scrape", data=f, timeout=60)
    time.sleep(8)
    _, jobs = http("GET", "/api/v1/jobs")
    working = [j for j in jobs if j.get("status") == "working"]
    pending = [j for j in jobs if j.get("status") == "pending"]
    print(f"after 8s: working={len(working)} pending={len(pending)} total={len(jobs)}")
    for j in working[:8]:
        print(f"  WORKING {j.get('id','')[:8]} {j.get('name')}")
    report["concurrency"] = {
        "fired": 4,
        "working_after_8s": len(working),
        "pending_after_8s": len(pending),
        "host_cap_env": 8,
        "note": "deep mode inner workers capped at 2; adaptive jobs from RAM",
    }

    # Cancel remaining to free resources
    for j in jobs:
        if j.get("status") in ("working", "pending"):
            try:
                http("POST", f"/api/v1/jobs/{j['id']}/cancel")
            except Exception:
                pass

    section("6) SUMMARY")
    print(json.dumps(report, ensure_ascii=False, indent=2))
    out = "/opt/cursor/artifacts/workflow-e2e-report.json"
    try:
        import os
        os.makedirs("/opt/cursor/artifacts", exist_ok=True)
        with open(out, "w") as f:
            json.dump(report, f, ensure_ascii=False, indent=2)
        print(f"wrote {out}")
    except Exception as e:
        print(f"artifact write: {e}")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
