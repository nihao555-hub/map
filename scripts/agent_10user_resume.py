#!/usr/bin/env python3
"""Resume polling an in-flight 10-user full load without re-dispatching.

Input CSV lines: owner_code,job_id,name,status
"""
from __future__ import annotations

import csv
import io
import json
import os
import time

from agent_10user_load import (  # type: ignore
    BASE,
    RADIUS_KM,
    Session,
    UserResult,
    coverage,
    get_job,
    guess_center,
    places_for,
    queue_status,
    radius_compliance,
    redeem,
)

ARTIFACT = os.environ.get(
    "GMS_ARTIFACT", "/opt/cursor/artifacts/agent-10user-full-load.json"
)
POLL_S = float(os.environ.get("GMS_POLL_S", "30"))
MAX_WAIT_S = int(os.environ.get("GMS_MAX_WAIT_S", "43200"))  # 12h
JOBS_CSV = os.environ.get("GMS_JOBS_CSV", "/tmp/full_jobs.csv")
T0_EPOCH = float(os.environ.get("GMS_T0_EPOCH", "0") or "0")


def load_rows(path: str) -> list[dict]:
    text = open(path, encoding="utf-8").read()
    return list(csv.DictReader(io.StringIO(text)))


def main() -> int:
    rows = load_rows(JOBS_CSV)
    if not rows:
        raise SystemExit(f"no jobs in {JOBS_CSV}")
    wall0 = T0_EPOCH if T0_EPOCH > 0 else time.time()
    print(
        f"RESUME base={BASE} jobs={len(rows)} max_wait={MAX_WAIT_S}s "
        f"artifact={ARTIFACT}",
        flush=True,
    )

    # Prepare sessions / results
    results: list[UserResult] = []
    sessions: dict[str, Session] = {}
    for i, row in enumerate(rows, 1):
        invite = row["owner_code"].strip()
        jid = row["id"].strip()
        name = row.get("name") or ""
        goal = name
        city, center = guess_center(name + " " + goal)
        res = UserResult(user=i, invite=invite, goal=goal, job_ids=[jid])
        s = Session()
        try:
            res.redeem_ms = redeem(s, invite)
        except Exception as e:
            # Already-used invites still restore session cookie on redeem attempt;
            # if that fails, keep going — Get job may still work with cookie.
            res.error = f"redeem: {e}"
        sessions[invite] = s
        st0 = (row.get("status") or "").lower()
        if st0 == "ok":
            res.first_working_s = 0.0
            res.completed_s = round(time.time() - wall0, 1)
            res.final_status[jid] = "ok"
        results.append(res)
        print(f"[u{i}] track {city or name[:24]} job={jid[:8]} status={st0}", flush=True)

    t_poll0 = time.time()
    while time.time() - t_poll0 < MAX_WAIT_S:
        all_done = True
        for res in results:
            if res.completed_s is not None:
                continue
            all_done = False
            s = sessions[res.invite]
            jid = res.job_ids[0]
            q = queue_status(s, [jid])
            st = ""
            jobs = q.get("jobs") or []
            if jobs:
                st = (jobs[0].get("Status") or jobs[0].get("status") or "").lower()
            if not st:
                j = get_job(s, jid)
                st = (j.get("Status") or j.get("status") or "").lower()
            if not st:
                continue
            res.final_status[jid] = st
            if st == "working" and res.first_working_s is None:
                res.first_working_s = round(time.time() - wall0, 1)
                print(f"[u{res.user}] first working at {res.first_working_s}s", flush=True)
            if st in ("ok", "failed", "canceled", "cancelled"):
                res.completed_s = round(time.time() - wall0, 1)
                # gather places now
                city, center = guess_center(res.goal)
                pls = places_for(s, jid)
                cov = coverage(pls)
                res.places = cov["n"]
                res.unique_titles = cov["unique"]
                res.phone_pct = cov["phone_pct"]
                res.email_pct = cov["email_pct"]
                res.website_pct = cov["website_pct"]
                res.sample_titles = cov["sample"]
                res.within_radius_pct = radius_compliance(pls, center, RADIUS_KM)
                res.ok = st == "ok" and res.places > 0
                if not res.ok and not res.error:
                    res.error = f"status={st} places={res.places}"
                print(
                    f"[u{res.user}] {city or '?'} ok={res.ok} wait={res.first_working_s}s "
                    f"done={res.completed_s}s places={res.places} phone={res.phone_pct}% "
                    f"email={res.email_pct}% geo={res.within_radius_pct}% err={res.error}",
                    flush=True,
                )
        if all_done:
            break
        time.sleep(POLL_S)

    # Final harvest for any still open
    for res in results:
        if res.completed_s is not None:
            continue
        s = sessions[res.invite]
        jid = res.job_ids[0]
        j = get_job(s, jid)
        st = (j.get("Status") or j.get("status") or "timeout").lower()
        res.final_status[jid] = st
        res.completed_s = round(time.time() - wall0, 1)
        pls = places_for(s, jid)
        cov = coverage(pls)
        res.places = cov["n"]
        res.unique_titles = cov["unique"]
        res.phone_pct = cov["phone_pct"]
        res.email_pct = cov["email_pct"]
        res.website_pct = cov["website_pct"]
        res.sample_titles = cov["sample"]
        city, center = guess_center(res.goal)
        res.within_radius_pct = radius_compliance(pls, center, RADIUS_KM)
        res.ok = st == "ok" and res.places > 0
        res.error = res.error or f"status={st} places={res.places}"
        print(
            f"[u{res.user}] FINAL {city or '?'} ok={res.ok} done={res.completed_s}s "
            f"places={res.places} err={res.error}",
            flush=True,
        )

    # Fill places for pre-completed ok rows if missing
    for res in results:
        if res.places > 0:
            continue
        s = sessions[res.invite]
        jid = res.job_ids[0]
        pls = places_for(s, jid)
        cov = coverage(pls)
        res.places = cov["n"]
        res.unique_titles = cov["unique"]
        res.phone_pct = cov["phone_pct"]
        res.email_pct = cov["email_pct"]
        res.website_pct = cov["website_pct"]
        res.sample_titles = cov["sample"]
        city, center = guess_center(res.goal)
        res.within_radius_pct = radius_compliance(pls, center, RADIUS_KM)
        st = res.final_status.get(jid, "")
        res.ok = st == "ok" and res.places > 0

    elapsed = round(time.time() - wall0, 1)
    n = len(results)
    ok_n = sum(1 for r in results if r.ok)
    waits = [r.first_working_s for r in results if r.first_working_s is not None]
    dones = [r.completed_s for r in results if r.completed_s is not None]
    report = {
        "base": BASE,
        "ts": time.strftime("%Y-%m-%dT%H:%M:%SZ", time.gmtime()),
        "users": n,
        "full": True,
        "radius_km": RADIUS_KM,
        "wall_s": elapsed,
        "ok": ok_n,
        "jobs_total": n,
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
        "avg_phone_pct": round(sum(r.phone_pct for r in results) / max(n, 1), 1),
        "avg_email_pct": round(sum(r.email_pct for r in results) / max(n, 1), 1),
        "avg_geo_pct": round(
            sum(r.within_radius_pct or 0 for r in results if r.within_radius_pct is not None)
            / max(1, sum(1 for r in results if r.within_radius_pct is not None)),
            1,
        ),
        "users_detail": [r.__dict__ for r in results],
    }
    os.makedirs(os.path.dirname(ARTIFACT) or ".", exist_ok=True)
    with open(ARTIFACT, "w", encoding="utf-8") as f:
        json.dump(report, f, ensure_ascii=False, indent=2, default=list)
    print("\n=== SUMMARY ===", flush=True)
    print(json.dumps({k: v for k, v in report.items() if k != "users_detail"}, ensure_ascii=False, indent=2))
    print(f"wrote {ARTIFACT}", flush=True)
    return 0 if ok_n >= max(1, n // 2) else 1


if __name__ == "__main__":
    raise SystemExit(main())
