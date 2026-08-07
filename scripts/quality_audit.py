#!/usr/bin/env python3
"""Independent quality audit for scrape results and place intel.

Deliberately does NOT reuse the production relevance filter or the person-name
gate, so a pass here is not the code grading its own homework:

  * relevance   -> LLM judge (GRSAI) scores each sampled row against the brief
  * intel facts -> checked against verifiable evidence (domains, source URLs)

Usage: quality_audit.py <base_url> <job_id> [sample_n] [intel_n]
"""
import json
import os
import random
import re
import sys
import time
import urllib.parse
import urllib.request
from collections import Counter

GRSAI_HOST = os.environ.get("GRSAI_API_HOST", "https://grsaiapi.com").rstrip("/")
GRSAI_KEY = os.environ.get("GRSAI_API_KEY", "")
GRSAI_MODEL = os.environ.get("GRSAI_MODEL", "gemini-3.1-flash-lite")


def http_json(url, timeout=120):
    req = urllib.request.Request(url, headers={"User-Agent": "quality-audit/1.0"})
    with urllib.request.urlopen(req, timeout=timeout) as r:
        return json.loads(r.read().decode("utf-8", "replace"))


def llm(prompt, timeout=120):
    if not GRSAI_KEY:
        return ""
    body = json.dumps({
        "model": GRSAI_MODEL,
        "messages": [{"role": "user", "content": prompt}],
        "temperature": 0,
    }).encode()
    req = urllib.request.Request(
        GRSAI_HOST + "/v1/chat/completions",
        data=body,
        headers={"Content-Type": "application/json", "Authorization": "Bearer " + GRSAI_KEY},
    )
    with urllib.request.urlopen(req, timeout=timeout) as r:
        data = json.loads(r.read().decode("utf-8", "replace"))
    return data["choices"][0]["message"]["content"]


def json_from(text):
    m = re.search(r"\[.*\]|\{.*\}", text or "", re.S)
    if not m:
        return None
    try:
        return json.loads(m.group(0))
    except Exception:
        return None


def pct(n, d):
    return round(100.0 * n / d, 1) if d else 0.0


def registrable(host):
    host = re.sub(r"^https?://", "", (host or "").strip().lower()).split("/")[0]
    host = host.split(":")[0]
    if host.startswith("www."):
        host = host[4:]
    parts = host.split(".")
    if len(parts) <= 2:
        return host
    if parts[-2] in ("co", "com", "or", "net", "go", "ac", "sch", "my", "web"):
        return ".".join(parts[-3:])
    return ".".join(parts[-2:])


def audit_places(base, job_id, sample_n):
    rows = http_json(f"{base}/api/v1/jobs/{job_id}/places?full=1", timeout=300)
    if isinstance(rows, dict):
        rows = rows.get("places") or []
    total = len(rows)
    out = {"total": total}
    if not total:
        return out, rows

    def has(row, *keys):
        return any((str(row.get(k) or "").strip()) for k in keys)

    out["coverage_pct"] = {
        "phone": pct(sum(1 for r in rows if has(r, "phone", "Phone")), total),
        "whatsapp": pct(sum(1 for r in rows if has(r, "whatsapp", "WhatsApp")), total),
        "website": pct(sum(1 for r in rows if has(r, "website", "Website")), total),
        "email": pct(sum(1 for r in rows if has(r, "emails", "Emails")), total),
        "address": pct(sum(1 for r in rows if has(r, "address", "Address")), total),
        "any_contact": pct(sum(1 for r in rows if has(r, "phone", "Phone", "whatsapp", "WhatsApp", "emails", "Emails")), total),
    }
    cats = Counter((str(r.get("category") or r.get("Category") or "?")).strip() for r in rows)
    out["top_categories"] = cats.most_common(12)

    # duplicate detection (independent of the scraper's own dedup keys)
    names = Counter(
        re.sub(r"\s+", " ", str(r.get("title") or r.get("Title") or "").strip().lower())
        for r in rows
    )
    out["duplicate_titles"] = sum(c - 1 for c in names.values() if c > 1)

    sample = random.sample(rows, min(sample_n, total))
    judged = []
    batch = 20
    for i in range(0, len(sample), batch):
        chunk = sample[i:i + batch]
        listing = "\n".join(
            f"{j+1}. 名称={r.get('title') or r.get('Title')} | 类别={r.get('category') or r.get('Category')} | 地址={(str(r.get('address') or r.get('Address') or ''))[:60]}"
            for j, r in enumerate(chunk)
        )
        prompt = (
            "你是外贸获客数据质检员。客户需求：在雅加达找配电柜/配电箱/电气成套设备的经销商、"
            "供应商或电气工程商（印尼语 panel listrik / alat listrik / instalasi listrik）。\n"
            "给下列每条商家打分：2=直接对口（卖配电柜/电气器材/电气工程）；"
            "1=沾边（泛电气或工程建材，可能有需求但不专营）；0=完全不相关（餐饮/警局/商场/手机店等）。\n"
            "只输出 JSON 数组，元素形如 {\"i\":1,\"score\":2,\"why\":\"简短\"}。\n\n" + listing
        )
        try:
            res = json_from(llm(prompt)) or []
        except Exception as e:
            print(f"  judge error: {e}", file=sys.stderr)
            res = []
        for item in res:
            try:
                idx = int(item["i"]) - 1
                if 0 <= idx < len(chunk):
                    judged.append((chunk[idx], int(item["score"]), item.get("why", "")))
            except Exception:
                continue
        time.sleep(0.5)

    if judged:
        s2 = sum(1 for _, s, _ in judged if s == 2)
        s1 = sum(1 for _, s, _ in judged if s == 1)
        s0 = sum(1 for _, s, _ in judged if s == 0)
        out["llm_relevance"] = {
            "judged": len(judged),
            "on_target_pct": pct(s2, len(judged)),
            "adjacent_pct": pct(s1, len(judged)),
            "off_target_pct": pct(s0, len(judged)),
            "off_target_examples": [
                f"{(r.get('title') or r.get('Title'))} [{r.get('category') or r.get('Category')}] :: {why}"
                for r, s, why in judged if s == 0
            ][:12],
        }
    return out, rows


def audit_intel(base, job_id, rows, intel_n):
    pool = [r for r in rows if (r.get("place_id") or r.get("PlaceID"))]
    with_site = [r for r in pool if str(r.get("website") or r.get("Website") or "").strip()]
    without = [r for r in pool if r not in with_site]
    picks = random.sample(with_site, min(intel_n * 2 // 3, len(with_site)))
    picks += random.sample(without, min(intel_n - len(picks), len(without)))

    results = []
    for r in picks:
        pid = r.get("place_id") or r.get("PlaceID")
        try:
            data = http_json(
                f"{base}/api/v1/jobs/{job_id}/places/{urllib.parse.quote(pid)}/intel", timeout=300
            )
        except Exception as e:
            results.append({"title": r.get("title"), "error": str(e)})
            continue
        results.append({"row": r, "intel": data})
        time.sleep(0.2)

    ready = [x for x in results if x.get("intel") and (
        x["intel"].get("summary") or x["intel"].get("status") in ("ready", "skipped"))]
    running = [x for x in results if x.get("intel") and x["intel"].get("status") in ("running", "pending")]
    failed = [x for x in results if x.get("error") or (x.get("intel") or {}).get("status") == "failed"]

    email_checked = email_matched = 0
    people_total = people_evidence_ok = 0
    generic_only = 0
    summary_cases = []
    for x in ready:
        intel = x["intel"]
        row = x["row"]
        site = str(row.get("website") or row.get("Website") or "")
        site_dom = registrable(site) if site else ""
        emails = intel.get("extra_emails") or []
        if site_dom and emails:
            for em in emails:
                email_checked += 1
                if registrable(em.split("@")[-1]) == site_dom:
                    email_matched += 1
        dms = intel.get("decision_makers") or []
        named = [d for d in dms if (d.get("name") or "").strip()]
        if dms and not named:
            generic_only += 1
        title = str(row.get("title") or row.get("Title") or "")
        brand_tokens = [t for t in re.split(r"[^A-Za-z0-9]+", title.lower()) if len(t) > 3]
        for d in named:
            people_total += 1
            ev = " ".join(str(d.get(k) or "") for k in ("evidence", "source", "linkedin", "headline", "email")).lower()
            if any(t in ev for t in brand_tokens) or (site_dom and site_dom in ev):
                people_evidence_ok += 1
        if intel.get("summary"):
            summary_cases.append({
                "title": title,
                "category": row.get("category") or row.get("Category"),
                "website": site,
                "summary": intel["summary"][:400],
                "sources": (intel.get("sources") or [])[:5],
            })

    out = {
        "sampled": len(results),
        "ready": len(ready),
        "running": len(running),
        "failed": len(failed),
        "email_domain_match_pct": pct(email_matched, email_checked),
        "emails_checked": email_checked,
        "named_people": people_total,
        "named_people_with_company_evidence_pct": pct(people_evidence_ok, people_total),
        "channel_only_cases": generic_only,
    }

    if summary_cases:
        chunk = summary_cases[:20]
        listing = "\n\n".join(
            f"{i+1}. 商家={c['title']} | 类别={c['category']} | 官网={c['website']}\n摘要={c['summary']}"
            for i, c in enumerate(chunk)
        )
        prompt = (
            "你在核查商家背调摘要是否与该商家本身一致（不是别的公司、不是模板营销话术）。\n"
            "对每条打分：2=贴合该商家且有具体信息；1=正确但太空泛；0=张冠李戴或明显编造。\n"
            "只输出 JSON 数组，元素形如 {\"i\":1,\"score\":2,\"why\":\"简短\"}。\n\n" + listing
        )
        try:
            res = json_from(llm(prompt)) or []
            scores = [int(i["score"]) for i in res if "score" in i]
            if scores:
                out["summary_judge"] = {
                    "judged": len(scores),
                    "faithful_pct": pct(sum(1 for s in scores if s == 2), len(scores)),
                    "vague_pct": pct(sum(1 for s in scores if s == 1), len(scores)),
                    "wrong_pct": pct(sum(1 for s in scores if s == 0), len(scores)),
                    "wrong_examples": [
                        chunk[int(i["i"]) - 1]["title"] + " :: " + i.get("why", "")
                        for i in res
                        if str(i.get("score")) == "0" and 0 < int(i["i"]) <= len(chunk)
                    ][:8],
                }
        except Exception as e:
            out["summary_judge_error"] = str(e)
    return out


if __name__ == "__main__":
    base = sys.argv[1].rstrip("/")
    job_id = sys.argv[2]
    sample_n = int(sys.argv[3]) if len(sys.argv) > 3 else 80
    intel_n = int(sys.argv[4]) if len(sys.argv) > 4 else 24
    random.seed(7)

    places, rows = audit_places(base, job_id, sample_n)
    print(json.dumps({"places": places}, ensure_ascii=False, indent=2), flush=True)

    intel = audit_intel(base, job_id, rows, intel_n) if rows else {}
    report = {"job_id": job_id, "places": places, "intel": intel}
    out_path = os.environ.get("AUDIT_OUT", "/tmp/quality-audit.json")
    with open(out_path, "w") as f:
        json.dump(report, f, ensure_ascii=False, indent=2)
    print(json.dumps({"intel": intel}, ensure_ascii=False, indent=2))
    print(f"\nsaved {out_path}")
