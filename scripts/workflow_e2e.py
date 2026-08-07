#!/usr/bin/env python3
"""Local workflow E2E: agent pipeline + deep full-volume speed/quality + concurrency.

Jobs API returns capitalized fields (ID/Status/Data).
"""
from __future__ import annotations

import json
import os
import time
import urllib.parse
import urllib.request

BASE = os.environ.get("GMS_BASE", "http://127.0.0.1:18080")


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


def job_id(j):
    return j.get("ID") or j.get("id")


def job_status_of(j):
    return j.get("Status") or j.get("status")


def job_data(j):
    return j.get("Data") or j.get("data") or {}


def places_count(jid):
    try:
        code, data = http("GET", f"/api/v1/jobs/{jid}/places/count", timeout=30)
        if code == 200 and isinstance(data, dict):
            return int(data.get("count") or data.get("total") or 0)
    except Exception:
        pass
    return 0


def get_places(jid):
    try:
        _, data = http("GET", f"/api/v1/jobs/{jid}/places", timeout=90)
        if isinstance(data, dict):
            for k in ("places", "results", "data"):
                if isinstance(data.get(k), list):
                    return data[k]
        if isinstance(data, list):
            return data
    except Exception as e:
        print(f"  places fetch error: {e}")
    return []


def coverage(places):
    n = len(places) or 1
    keys = ("phone", "emails", "website", "whatsapp", "address", "title")
    stats = {k: 0 for k in keys}
    for p in places:
        if not isinstance(p, dict):
            continue
        for k in keys:
            v = p.get(k)
            if v and str(v).strip() not in ("", "[]", "null", "None"):
                stats[k] += 1
    return {k: round(100.0 * v / n, 1) for k, v in stats.items()}, len(places)


def section(title):
    print("\n" + "=" * 60)
    print(title)
    print("=" * 60, flush=True)


def main():
    report = {"base": BASE, "ts": time.strftime("%Y-%m-%dT%H:%M:%SZ", time.gmtime())}

    section("1) UI / health")
    code, html = http("GET", "/")
    print(f"GET / => {code}")
    checks = {
        "intent_box": "intent_goal" in html,
        "deep_full_banner": "mode-fixed-banner" in html or "深度全量" in html,
        "lang_select": "ui-lang-select" in html,
        "radius_field": "radius_km" in html,
        "no_proxy_form_field": 'name="proxies"' not in html,
    }
    for k, v in checks.items():
        print(f"  {k}: {'OK' if v else 'FAIL'}")
    report["ui"] = checks

    section("2) IntentAgent understand + dispatch")
    t0 = time.time()
    code, plan = http(
        "POST", "/api/v1/agent/understand",
        json_body={"goal": "在雅加达找咖啡馆和进口商，半径8公里", "ui_lang": "zh"},
    )
    print(f"understand {code} in {(time.time()-t0)*1000:.0f}ms tasks={len((plan or {}).get('tasks') or [])}")
    report["understand"] = plan

    t0 = time.time()
    code, disp = http(
        "POST", "/api/v1/agent/dispatch",
        json_body={"goal": "在雅加达找咖啡馆，半径2公里", "ui_lang": "zh"},
    )
    print(f"dispatch {code} in {(time.time()-t0)*1000:.0f}ms => {disp}")
    report["dispatch"] = disp

    section("3) Deep scrape smoke (2km)")
    form = {
        "name": "e2e-deep-cafe",
        "keywords": "cafe",
        "locations": "Jakarta",
        "lang": "id",
        "country_code": "id",
        "country_name": "Indonesia",
        "zoom": "15",
        "depth": "50",
        "maxtime": "12m",
        "latitude": "-6.2088",
        "longitude": "106.8456",
        "fastmode": "on",
        "gridmode": "",
        "radius_km": "2",
        "radius": "2000",
        "maxresults": "20",
        "email": "on",
        "ui_lang": "zh",
    }
    before = {job_id(j) for j in (http("GET", "/api/v1/jobs")[1] or [])}
    http("POST", "/scrape", data=form, timeout=60)
    jid = None
    for _ in range(40):
        _, jobs = http("GET", "/api/v1/jobs")
        for j in jobs or []:
            if job_id(j) not in before:
                jid = job_id(j)
                jd = job_data(j)
                print(
                    f"job={jid} status={job_status_of(j)} fast={jd.get('fast_mode')} "
                    f"grid={jd.get('grid_mode')} max={jd.get('max_results')} depth={jd.get('depth')}"
                )
                break
        if jid:
            break
        time.sleep(0.5)
    if not jid:
        print("FAIL no job")
        return 1

    t0 = time.time()
    status = "working"
    at30 = at60 = None
    n = 0
    while time.time() - t0 < 150:
        _, jobs = http("GET", "/api/v1/jobs")
        j = next((x for x in (jobs or []) if job_id(x) == jid), {})
        status = job_status_of(j)
        n = places_count(jid)
        el = time.time() - t0
        if at30 is None and el >= 30:
            at30 = n
            print(f"t=30s places={n} status={status}", flush=True)
        if at60 is None and el >= 60:
            at60 = n
            print(f"t=60s places={n} status={status}", flush=True)
        if status in ("ok", "failed", "canceled"):
            print(f"done status={status} places={n} elapsed={el:.1f}s", flush=True)
            break
        time.sleep(5)

    places = get_places(jid)
    cov, nplaces = coverage(places)
    elapsed = time.time() - t0
    rate = nplaces / elapsed * 60 if elapsed else 0
    print(f"coverage={cov} rate={rate:.1f}/min")
    report["scrape"] = {
        "job_id": jid,
        "status": status,
        "places": nplaces,
        "at30": at30,
        "at60": at60,
        "places_per_min": round(rate, 1),
        "coverage_pct": cov,
        "elapsed_s": round(elapsed, 1),
    }

    section("4) Concurrency fire 4")
    for i in range(4):
        f = dict(form)
        f["name"] = f"conc-{i}"
        f["radius_km"] = "1"
        f["radius"] = "1000"
        f["latitude"] = str(-6.20 - i * 0.01)
        f["longitude"] = str(106.84 + i * 0.01)
        http("POST", "/scrape", data=f)
    time.sleep(8)
    _, jobs = http("GET", "/api/v1/jobs")
    working = [j for j in (jobs or []) if job_status_of(j) == "working"]
    print(f"working={len(working)}")
    report["concurrency"] = {"fired": 4, "working_after_8s": len(working)}

    for j in jobs or []:
        if job_status_of(j) in ("working", "pending"):
            try:
                http("POST", f"/api/v1/jobs/{job_id(j)}/cancel")
            except Exception:
                pass

    section("SUMMARY")
    print(json.dumps(report, ensure_ascii=False, indent=2))
    os.makedirs("/opt/cursor/artifacts", exist_ok=True)
    with open("/opt/cursor/artifacts/workflow-e2e-live.json", "w") as f:
        json.dump(report, f, ensure_ascii=False, indent=2)
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
