#!/usr/bin/env python3
"""Simulate 10 invite-isolated Agent users against a public GMS deployment.

Agent jobs are hidden from GET /api/v1/jobs (map UI). This script polls:
  POST /api/v1/agent/jobs/queue  and  GET /api/v1/jobs/{id}
  GET /api/v1/jobs/{id}/places?full=1

Usage:
  INVITE_CODES='GMS-...,GMS-...' python3 scripts/agent_10user_load.py
"""
from __future__ import annotations

import concurrent.futures
import http.cookiejar
import json
import math
import os
import ssl
import time
import urllib.error
import urllib.parse
import urllib.request
from dataclasses import asdict, dataclass, field
from typing import Any

BASE = os.environ.get("GMS_BASE", "http://124.222.208.237:8080").rstrip("/")
TIMEOUT = int(os.environ.get("GMS_TIMEOUT", "120"))
POLL_S = float(os.environ.get("GMS_POLL_S", "10"))
# full=1 → city-scale 50km deep+grid (product max); small radius otherwise.
FULL = os.environ.get("GMS_FULL", "").strip() in ("1", "true", "yes", "full")
RADIUS_KM = int(os.environ.get("GMS_RADIUS_KM", "50" if FULL else "2"))
MAX_WAIT_S = int(os.environ.get("GMS_MAX_WAIT_S", "43200" if FULL else "2100"))
ARTIFACT = os.environ.get(
    "GMS_ARTIFACT",
    "/opt/cursor/artifacts/agent-10user-full-load.json"
    if FULL
    else "/opt/cursor/artifacts/agent-10user-load.json",
)

_CITIES = [
    ("menteng", "Menteng, Jakarta"),
    ("surabaya", "Surabaya"),
    ("bandung", "Bandung"),
    ("medan", "Medan"),
    ("bekasi", "Bekasi"),
    ("tangerang", "Tangerang"),
    ("yogyakarta", "Yogyakarta"),
    ("semarang", "Semarang"),
    ("depok", "Depok"),
    ("bogor", "Bogor"),
]


def _default_goals() -> list[str]:
    if FULL:
        # Product "最全": project-max radius, one deep+grid+unlimited job per user.
        return [
            f"在{label}找咖啡馆，半径{RADIUS_KM}公里，只创建一个任务不要拆分，深挖不限数量，尽量拿全电话和邮箱"
            for _, label in _CITIES
        ]
    return [
        f"在 {label} 找咖啡馆，半径{RADIUS_KM}公里" for _, label in _CITIES
    ]


DEFAULT_GOALS = _default_goals()

CITY_CENTERS = {
    "menteng": (-6.1944, 106.8294),
    "jakarta": (-6.2088, 106.8456),
    "surabaya": (-7.2575, 112.7521),
    "bandung": (-6.9175, 107.6191),
    "medan": (3.5952, 98.6722),
    "bekasi": (-6.2383, 106.9756),
    "tangerang": (-6.1783, 106.6319),
    "yogyakarta": (-7.7956, 110.3695),
    "semarang": (-6.9667, 110.4167),
    "depok": (-6.4025, 106.7942),
    "bogor": (-6.5971, 106.8060),
}


def haversine_km(lat1, lon1, lat2, lon2) -> float:
    r = 6371.0
    p1, p2 = math.radians(lat1), math.radians(lat2)
    dphi = math.radians(lat2 - lat1)
    dlmb = math.radians(lon2 - lon1)
    a = math.sin(dphi / 2) ** 2 + math.cos(p1) * math.cos(p2) * math.sin(dlmb / 2) ** 2
    return 2 * r * math.asin(math.sqrt(a))


def guess_center(goal: str):
    g = goal.lower()
    for key, pt in CITY_CENTERS.items():
        if key in g:
            return key, pt
    return "", (None, None)


@dataclass
class UserResult:
    user: int
    invite: str
    goal: str
    ok: bool = False
    error: str = ""
    redeem_ms: float = 0
    dispatch_ms: float = 0
    job_ids: list[str] = field(default_factory=list)
    first_working_s: float | None = None
    completed_s: float | None = None
    final_status: dict[str, str] = field(default_factory=dict)
    places: int = 0
    unique_titles: int = 0
    phone_pct: float = 0
    email_pct: float = 0
    website_pct: float = 0
    within_radius_pct: float | None = None
    sample_titles: list[str] = field(default_factory=list)
    admit_samples: list[dict] = field(default_factory=list)


class NoRedirect(urllib.request.HTTPRedirectHandler):
    def redirect_request(self, req, fp, code, msg, headers, newurl):
        return None


class Session:
    def __init__(self):
        self.cj = http.cookiejar.CookieJar()
        ctx = ssl.create_default_context()
        self.opener = urllib.request.build_opener(
            urllib.request.HTTPCookieProcessor(self.cj),
            urllib.request.HTTPSHandler(context=ctx),
        )
        self.opener_noredirect = urllib.request.build_opener(
            urllib.request.HTTPCookieProcessor(self.cj),
            NoRedirect,
            urllib.request.HTTPSHandler(context=ctx),
        )

    def request(self, method, path, data=None, json_body=None, timeout=TIMEOUT, allow_redirects=True):
        url = path if path.startswith("http") else BASE + path
        body = None
        headers = {"User-Agent": "agent-10user-load/1.1"}
        if json_body is not None:
            body = json.dumps(json_body).encode()
            headers["Content-Type"] = "application/json"
        elif data is not None:
            body = urllib.parse.urlencode(data).encode()
            headers["Content-Type"] = "application/x-www-form-urlencoded"
        req = urllib.request.Request(url, data=body, headers=headers, method=method)
        opener = self.opener if allow_redirects else self.opener_noredirect
        try:
            with opener.open(req, timeout=timeout) as r:
                raw = r.read()
                code = r.status
                ctype = r.headers.get("Content-Type", "")
        except urllib.error.HTTPError as e:
            raw = e.read() if e.fp else b""
            code = e.code
            ctype = e.headers.get("Content-Type", "") if e.headers else ""
            if not allow_redirects and code in (301, 302, 303, 307, 308):
                return code, {"Location": e.headers.get("Location", "")}
        text = raw.decode("utf-8", "replace")
        if "json" in ctype or path.startswith("/api/"):
            try:
                return code, json.loads(text) if text else {}
            except Exception:
                return code, text
        return code, text


def redeem(session: Session, code: str) -> float:
    t0 = time.time()
    http_code, body = session.request(
        "POST",
        "/invite",
        data={"code": code, "next": "/agent/"},
        timeout=60,
        allow_redirects=False,
    )
    if http_code not in (200, 302, 303):
        raise RuntimeError(f"redeem http={http_code} body={str(body)[:160]}")
    cookies = [c for c in session.cj if c.name == "gms_invite_session"]
    if not cookies:
        raise RuntimeError("redeem did not set gms_invite_session cookie")
    return (time.time() - t0) * 1000


def dispatch(session: Session, goal: str):
    t0 = time.time()
    c, body = session.request(
        "POST",
        "/api/v1/agent/dispatch",
        json_body={"goal": goal, "ui_lang": "zh"},
        timeout=180,
    )
    ms = (time.time() - t0) * 1000
    if c not in (200, 201) or not isinstance(body, dict):
        raise RuntimeError(f"dispatch http={c} body={body}")
    jobs = body.get("job_ids") or []
    jobs = [str(j) for j in jobs if j]
    if not jobs:
        raise RuntimeError(f"dispatch returned no jobs: {body}")
    return ms, jobs, body


def queue_status(session: Session, job_ids: list[str]) -> dict[str, Any]:
    c, body = session.request(
        "POST",
        "/api/v1/agent/jobs/queue",
        json_body={"job_ids": job_ids},
        timeout=60,
    )
    if c != 200 or not isinstance(body, dict):
        return {}
    return body


def get_job(session: Session, jid: str) -> dict:
    c, body = session.request("GET", f"/api/v1/jobs/{jid}", timeout=60)
    if c != 200 or not isinstance(body, dict):
        return {}
    return body


def places_for(session: Session, jid: str) -> list[dict]:
    c, body = session.request("GET", f"/api/v1/jobs/{jid}/places?full=1", timeout=90)
    if c != 200:
        return []
    if isinstance(body, list):
        return [p for p in body if isinstance(p, dict)]
    if isinstance(body, dict):
        for k in ("places", "results", "data"):
            if isinstance(body.get(k), list):
                return [p for p in body[k] if isinstance(p, dict)]
    return []


def coverage(places: list[dict]):
    n = max(len(places), 1)
    phone = email = website = 0
    titles = []
    for p in places:
        titles.append(str(p.get("title") or p.get("name") or "").strip())
        if str(p.get("phone") or "").strip():
            phone += 1
        emails = p.get("emails") or p.get("email") or []
        if isinstance(emails, list):
            if any(str(x).strip() for x in emails):
                email += 1
        elif str(emails).strip():
            email += 1
        if str(p.get("website") or "").strip():
            website += 1
    uniq = len({t.lower() for t in titles if t})
    return {
        "n": len(places),
        "unique": uniq,
        "phone_pct": round(100 * phone / n, 1),
        "email_pct": round(100 * email / n, 1),
        "website_pct": round(100 * website / n, 1),
        "sample": [t for t in titles if t][:5],
    }


def radius_compliance(places: list[dict], center, radius_km: float):
    clat, clon = center
    if clat is None:
        return None
    ok = known = 0
    for p in places:
        try:
            lat = float(p.get("latitude") or p.get("lat") or 0)
            lon = float(p.get("longitude") or p.get("lon") or 0)
        except Exception:
            continue
        if lat == 0 and lon == 0:
            continue
        known += 1
        if haversine_km(clat, clon, lat, lon) <= radius_km * 1.35:
            ok += 1
    if known == 0:
        return None
    return round(100 * ok / known, 1)


def run_user(idx: int, invite: str, goal: str) -> UserResult:
    res = UserResult(user=idx, invite=invite, goal=goal)
    s = Session()
    city, center = guess_center(goal)
    try:
        res.redeem_ms = redeem(s, invite)
        res.dispatch_ms, res.job_ids, _ = dispatch(s, goal)
    except Exception as e:
        res.error = f"setup: {e}"
        print(f"[u{idx}] SETUP FAIL {e}", flush=True)
        return res

    print(
        f"[u{idx}] dispatched {city} jobs={len(res.job_ids)} in {res.dispatch_ms:.0f}ms",
        flush=True,
    )

    t0 = time.time()
    while time.time() - t0 < MAX_WAIT_S:
        q = queue_status(s, res.job_ids)
        jobs = q.get("jobs") or []
        by_id = {j.get("ID") or j.get("id"): j for j in jobs if isinstance(j, dict)}
        if q:
            res.admit_samples.append(
                {
                    "t": round(time.time() - t0, 1),
                    "active_jobs": q.get("active_jobs"),
                    "admit_slots": q.get("admit_slots"),
                }
            )

        # Fallback: per-job GET if queue batch empty
        if len(by_id) < len(res.job_ids):
            for jid in res.job_ids:
                if jid in by_id:
                    continue
                j = get_job(s, jid)
                if j:
                    by_id[jid] = {
                        "id": jid,
                        "Status": j.get("Status") or j.get("status"),
                        "status": j.get("Status") or j.get("status"),
                    }

        working_any = False
        all_terminal = True
        for jid in res.job_ids:
            j = by_id.get(jid) or {}
            st = (j.get("Status") or j.get("status") or "").lower()
            if not st:
                all_terminal = False
                continue
            res.final_status[jid] = st
            if st == "working":
                working_any = True
            if st in ("pending", "working"):
                all_terminal = False
        if working_any and res.first_working_s is None:
            res.first_working_s = round(time.time() - t0, 1)
            print(f"[u{idx}] first working at {res.first_working_s}s", flush=True)
        if all_terminal and len(res.final_status) == len(res.job_ids):
            res.completed_s = round(time.time() - t0, 1)
            break
        time.sleep(POLL_S)

    all_places: list[dict] = []
    for jid in res.job_ids:
        all_places.extend(places_for(s, jid))
    cov = coverage(all_places)
    res.places = cov["n"]
    res.unique_titles = cov["unique"]
    res.phone_pct = cov["phone_pct"]
    res.email_pct = cov["email_pct"]
    res.website_pct = cov["website_pct"]
    res.sample_titles = cov["sample"]
    res.within_radius_pct = radius_compliance(all_places, center, RADIUS_KM)
    statuses = set(res.final_status.values())
    res.ok = bool(res.job_ids) and statuses.issubset({"ok"}) and res.places > 0
    if not res.ok and not res.error:
        res.error = f"status={res.final_status} places={res.places}"
    print(
        f"[u{idx}] {city or '?'} ok={res.ok} wait={res.first_working_s}s "
        f"done={res.completed_s}s jobs={len(res.job_ids)} places={res.places} "
        f"phone={res.phone_pct}% email={res.email_pct}% geo={res.within_radius_pct}% "
        f"err={res.error}",
        flush=True,
    )
    return res


def load_invites() -> list[str]:
    raw = os.environ.get("INVITE_CODES", "").strip()
    if raw:
        return [c.strip() for c in raw.split(",") if c.strip()]
    path = os.environ.get("INVITE_FILE", "")
    if path and os.path.exists(path):
        return [ln.strip() for ln in open(path) if ln.strip() and not ln.startswith("#")]
    raise SystemExit("Set INVITE_CODES or INVITE_FILE with >=10 unused invite codes")


def main():
    invites = load_invites()
    goals = DEFAULT_GOALS
    n = min(10, len(invites), len(goals))
    invites, goals = invites[:n], goals[:n]
    print(
        f"BASE={BASE} users={n} full={FULL} radius={RADIUS_KM}km "
        f"max_wait={MAX_WAIT_S}s",
        flush=True,
    )

    t0 = time.time()
    results: list[UserResult] = []
    with concurrent.futures.ThreadPoolExecutor(max_workers=n) as ex:
        futs = [ex.submit(run_user, i + 1, invites[i], goals[i]) for i in range(n)]
        for f in concurrent.futures.as_completed(futs):
            results.append(f.result())
    results.sort(key=lambda r: r.user)
    elapsed = round(time.time() - t0, 1)

    ok_n = sum(1 for r in results if r.ok)
    waits = [r.first_working_s for r in results if r.first_working_s is not None]
    dones = [r.completed_s for r in results if r.completed_s is not None]
    report: dict[str, Any] = {
        "base": BASE,
        "ts": time.strftime("%Y-%m-%dT%H:%M:%SZ", time.gmtime()),
        "users": n,
        "full": FULL,
        "radius_km": RADIUS_KM,
        "wall_s": elapsed,
        "ok": ok_n,
        "jobs_total": sum(len(r.job_ids) for r in results),
        "queue_wait_s": {
            "min": min(waits) if waits else None,
            "max": max(waits) if waits else None,
            "avg": round(sum(waits) / len(waits), 1) if waits else None,
        },
        "completion_s": {
            "min": min(dones) if dones else None,
            "max": max(dones) if dones else None,
            "avg": round(sum(dones) / len(dones), 1) if dones else None,
        },
        "places_total": sum(r.places for r in results),
        "avg_phone_pct": round(sum(r.phone_pct for r in results) / n, 1),
        "avg_email_pct": round(sum(r.email_pct for r in results) / n, 1),
        "avg_geo_pct": round(
            sum(r.within_radius_pct or 0 for r in results if r.within_radius_pct is not None)
            / max(1, sum(1 for r in results if r.within_radius_pct is not None)),
            1,
        ),
        "users_detail": [asdict(r) for r in results],
    }
    os.makedirs(os.path.dirname(ARTIFACT) or ".", exist_ok=True)
    with open(ARTIFACT, "w") as f:
        json.dump(report, f, ensure_ascii=False, indent=2)
    print("\n=== SUMMARY ===", flush=True)
    print(
        json.dumps(
            {k: v for k, v in report.items() if k != "users_detail"},
            ensure_ascii=False,
            indent=2,
        )
    )
    print(f"wrote {ARTIFACT}", flush=True)
    return 0 if ok_n >= max(1, n // 2) else 1


if __name__ == "__main__":
    raise SystemExit(main())
