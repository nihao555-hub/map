#!/usr/bin/env python3
"""Single-user public speed probe against invite-gated Agent API."""
from __future__ import annotations

import json
import os
import ssl
import sys
import time
import http.cookiejar
import urllib.error
import urllib.parse
import urllib.request
from typing import Any

BASE = os.environ.get("GMS_BASE", "http://124.222.208.237:8080").rstrip("/")
INVITE = os.environ["INVITE_CODE"]
GOAL = os.environ.get(
    "GMS_GOAL",
    "在 Menteng, Jakarta 找 kedai kopi，半径3公里",
)
POLL_S = float(os.environ.get("GMS_POLL_S", "8"))
MAX_WAIT_S = int(os.environ.get("GMS_MAX_WAIT_S", "1500"))
ARTIFACT = os.environ.get(
    "GMS_ARTIFACT", "/opt/cursor/artifacts/public-speed-probe.json"
)


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

    def request(
        self,
        method: str,
        path: str,
        *,
        data: dict | None = None,
        json_body: dict | None = None,
        timeout: int = 60,
        allow_redirects: bool = True,
    ):
        url = BASE + path
        headers = {"User-Agent": "gms-speed-probe/1.0"}
        body = None
        if json_body is not None:
            body = json.dumps(json_body).encode()
            headers["Content-Type"] = "application/json"
        elif data is not None:
            body = urllib.parse.urlencode(data).encode()
            headers["Content-Type"] = "application/x-www-form-urlencoded"
        req = urllib.request.Request(url, data=body, headers=headers, method=method)
        opener = self.opener if allow_redirects else self.opener_noredirect
        try:
            with opener.open(req, timeout=timeout) as resp:
                raw = resp.read()
                code = resp.getcode()
        except urllib.error.HTTPError as e:
            raw = e.read()
            code = e.code
        text = raw.decode("utf-8", errors="replace")
        parsed: Any
        try:
            parsed = json.loads(text) if text.strip().startswith(("{", "[")) else text
        except Exception:
            parsed = text
        return code, parsed


def main() -> int:
    s = Session()
    out: dict[str, Any] = {
        "base": BASE,
        "goal": GOAL,
        "invite": INVITE,
        "t0": time.time(),
    }
    print(f"BASE={BASE}", flush=True)
    print(f"GOAL={GOAL}", flush=True)

    t = time.time()
    code, body = s.request(
        "POST",
        "/invite",
        data={"code": INVITE, "next": "/agent/"},
        timeout=60,
        allow_redirects=False,
    )
    out["redeem_ms"] = round((time.time() - t) * 1000, 1)
    if code not in (200, 302, 303):
        out["error"] = f"redeem http={code} body={str(body)[:200]}"
        print(out["error"], flush=True)
        return 1
    if not any(c.name == "gms_invite_session" for c in s.cj):
        out["error"] = "no invite cookie"
        print(out["error"], flush=True)
        return 1
    print(f"redeem_ms={out['redeem_ms']}", flush=True)

    t = time.time()
    code, body = s.request(
        "POST",
        "/api/v1/agent/dispatch",
        json_body={"goal": GOAL, "ui_lang": "zh"},
        timeout=180,
    )
    out["dispatch_ms"] = round((time.time() - t) * 1000, 1)
    if code not in (200, 201) or not isinstance(body, dict):
        out["error"] = f"dispatch http={code} body={body}"
        print(out["error"], flush=True)
        return 1
    jobs = [str(j) for j in (body.get("job_ids") or []) if j]
    out["dispatch_body"] = {
        k: body.get(k) for k in ("message", "job_ids", "tasks", "plan") if k in body
    }
    out["job_ids"] = jobs
    print(f"dispatch_ms={out['dispatch_ms']} jobs={jobs}", flush=True)
    if not jobs:
        out["error"] = "no jobs"
        return 1

    t0 = time.time()
    first_working = None
    first_place = None
    last_places = 0
    timeline = []
    final = {}

    while time.time() - t0 < MAX_WAIT_S:
        elapsed = round(time.time() - t0, 1)
        c, q = s.request(
            "POST",
            "/api/v1/agent/jobs/queue",
            json_body={"job_ids": jobs},
            timeout=60,
        )
        statuses = {}
        if isinstance(q, dict):
            for item in q.get("jobs") or q.get("items") or []:
                if isinstance(item, dict) and item.get("id"):
                    statuses[str(item["id"])] = str(item.get("status") or "")
            if not statuses and isinstance(q.get("statuses"), dict):
                statuses = {str(k): str(v) for k, v in q["statuses"].items()}
        # Fallback: per-job GET
        if not statuses:
            for jid in jobs:
                c2, j = s.request("GET", f"/api/v1/jobs/{jid}", timeout=60)
                if isinstance(j, dict):
                    statuses[jid] = str(j.get("status") or "")

        if first_working is None and any(st == "working" for st in statuses.values()):
            first_working = elapsed

        place_n = 0
        samples = []
        for jid in jobs:
            c3, body3 = s.request(
                "GET", f"/api/v1/jobs/{jid}/places?full=1", timeout=90
            )
            places = []
            if c3 == 200:
                if isinstance(body3, list):
                    places = body3
                elif isinstance(body3, dict):
                    for k in ("places", "results", "data"):
                        if isinstance(body3.get(k), list):
                            places = body3[k]
                            break
            place_n += len(places)
            for p in places[:3]:
                if isinstance(p, dict) and p.get("title"):
                    samples.append(str(p["title"]))

        if place_n > 0 and first_place is None:
            first_place = elapsed
        if place_n != last_places:
            print(
                f"t={elapsed}s status={statuses} places={place_n} sample={samples[:3]}",
                flush=True,
            )
            last_places = place_n
            timeline.append(
                {
                    "t": elapsed,
                    "statuses": dict(statuses),
                    "places": place_n,
                    "sample": samples[:5],
                }
            )
        else:
            print(f"t={elapsed}s status={statuses} places={place_n}", flush=True)

        final = statuses
        if statuses and all(st in ("ok", "failed", "canceled", "error") for st in statuses.values()):
            break
        time.sleep(POLL_S)

    # Final place tally + contact coverage
    all_places = []
    for jid in jobs:
        c3, body3 = s.request("GET", f"/api/v1/jobs/{jid}/places?full=1", timeout=90)
        if c3 == 200 and isinstance(body3, list):
            all_places.extend(body3)
        elif c3 == 200 and isinstance(body3, dict):
            for k in ("places", "results", "data"):
                if isinstance(body3.get(k), list):
                    all_places.extend(body3[k])
                    break

    n = max(len(all_places), 1)
    phone = sum(1 for p in all_places if str(p.get("phone") or "").strip())
    wa = sum(1 for p in all_places if str(p.get("whatsapp") or "").strip())
    email = 0
    for p in all_places:
        e = p.get("emails") or p.get("email") or ""
        if isinstance(e, list):
            if any(str(x).strip() for x in e):
                email += 1
        elif str(e).strip():
            email += 1

    out.update(
        {
            "elapsed_s": round(time.time() - t0, 1),
            "first_working_s": first_working,
            "first_place_s": first_place,
            "final_status": final,
            "places": len(all_places),
            "phone_pct": round(100 * phone / n, 1),
            "whatsapp_pct": round(100 * wa / n, 1),
            "email_pct": round(100 * email / n, 1),
            "sample_titles": [
                str(p.get("title") or "") for p in all_places[:8] if p.get("title")
            ],
            "timeline": timeline,
            "ok": bool(final)
            and all(st == "ok" for st in final.values())
            and len(all_places) > 0,
        }
    )
    os.makedirs(os.path.dirname(ARTIFACT), exist_ok=True)
    with open(ARTIFACT, "w", encoding="utf-8") as f:
        json.dump(out, f, ensure_ascii=False, indent=2)
    print(json.dumps({k: out[k] for k in out if k != "timeline"}, ensure_ascii=False, indent=2), flush=True)
    print(f"ARTIFACT={ARTIFACT}", flush=True)
    return 0 if out.get("ok") else 2


if __name__ == "__main__":
    sys.exit(main())
