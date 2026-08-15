(function () {
  const keyword = document.getElementById("keyword");
  const status = document.getElementById("status");
  const results = document.getElementById("results");
  const empty = document.getElementById("empty");
  const fairBoard = document.getElementById("fair-board");
  const toastEl = document.getElementById("toast");
  const PAGE_SIZE = 20;

  let role = "seller";
  let lastHits = [];
  let page = 1;
  let searchGen = 0;

  function toast(msg) {
    toastEl.textContent = msg;
    toastEl.classList.remove("hidden");
    clearTimeout(toast.t);
    toast.t = setTimeout(function () { toastEl.classList.add("hidden"); }, 2400);
  }

  function escapeHtml(s) {
    return String(s || "").replace(/[&<>"']/g, function (c) {
      return ({ "&": "&amp;", "<": "&lt;", ">": "&gt;", '"': "&quot;", "'": "&#39;" })[c];
    });
  }
  function escapeAttr(s) { return escapeHtml(s).replace(/`/g, ""); }
  function normalizeKeyword(s) { return String(s || "").replace(/\s+/g, " ").trim(); }

  function extra(h, key) { return (h && h.extra && h.extra[key]) || ""; }

  function showKeywordError(msg) {
    const el = document.getElementById("keyword-error");
    if (!el) return;
    el.hidden = !msg;
    el.textContent = msg || "";
  }

  function fillCountrySelect(list) {
    const html = (list || []).map(function (c) {
      return '<option value="' + escapeAttr(c.code || "") + '">' + escapeHtml(c.label || c.code || "不限") + "</option>";
    }).join("");
    ["country", "landing-country"].forEach(function (id) {
      const el = document.getElementById(id);
      if (el) el.innerHTML = html;
    });
  }

  fetch("/api/v1/discover/countries")
    .then(function (r) { return r.json(); })
    .then(function (j) { if (j && j.countries) fillCountrySelect(j.countries); })
    .catch(function () {});

  function showResults() {
    const landing = document.getElementById("landing-view");
    const view = document.getElementById("results-view");
    if (landing) landing.hidden = true;
    if (view) view.hidden = false;
    document.body.classList.remove("is-landing");
  }

  function setRole(next) {
    role = next === "buyer" ? "buyer" : "seller";
    document.querySelectorAll("[data-role]").forEach(function (el) {
      el.classList.toggle("is-on", el.getAttribute("data-role") === role);
    });
  }

  function selectedCountry() {
    const a = document.getElementById("country");
    const b = document.getElementById("landing-country");
    return (a && a.value) || (b && b.value) || "";
  }

  function syncCountry(from) {
    const a = document.getElementById("country");
    const b = document.getElementById("landing-country");
    if (from && a && from !== a) a.value = from.value;
    if (from && b && from !== b) b.value = from.value;
  }

  document.querySelectorAll("[data-role]").forEach(function (btn) {
    btn.addEventListener("click", function () {
      const prev = role;
      setRole(btn.getAttribute("data-role"));
      if (prev !== role && keyword.value) doSearch(keyword.value);
    });
  });

  function pageCount() { return Math.max(1, Math.ceil(lastHits.length / PAGE_SIZE)); }
  function pagedHits() {
    if (page > pageCount()) page = pageCount();
    if (page < 1) page = 1;
    return lastHits.slice((page - 1) * PAGE_SIZE, page * PAGE_SIZE);
  }

  function updatePager() {
    const pager = document.getElementById("pager");
    if (!pager) return;
    pager.hidden = !lastHits.length;
    const countEl = document.getElementById("hit-count");
    if (countEl) countEl.textContent = "共 " + lastHits.length + " 条";
    const cur = document.getElementById("page-cur");
    const tot = document.getElementById("page-total");
    if (cur) cur.textContent = String(page);
    if (tot) tot.textContent = String(pageCount());
    const prev = document.getElementById("page-prev");
    const next = document.getElementById("page-next");
    if (prev) prev.disabled = page <= 1;
    if (next) next.disabled = page >= pageCount();
  }

  function renderExhibitorTable(hits, target) {
    if (!hits.length) {
      target.innerHTML = "";
      return;
    }
    target.innerHTML = hits.map(function (h) {
      return "<tr>" +
        "<td><b>" + escapeHtml(h.name || "") + "</b></td>" +
        "<td>" + escapeHtml(extra(h, "fair") || "") + "</td>" +
        "<td>" + escapeHtml(extra(h, "booth") || "—") + "</td>" +
        "<td>" + escapeHtml(h.country_label || extra(h, "country") || "—") + "</td>" +
      "</tr>";
    }).join("");
  }

  function renderFairCards(hits, target) {
    target.innerHTML = hits.map(function (h, i) {
      const when = [extra(h, "start"), extra(h, "end")].filter(Boolean).join(" – ") || "日期未公开";
      const where = [h.country_label || h.country || "", extra(h, "city")].filter(Boolean).join(" · ");
      return (
        '<article class="exh-card" data-i="' + i + '">' +
          '<span class="exh-when">' + escapeHtml(when) + "</span>" +
          "<h3>" + escapeHtml(h.name || h.title || "未命名") + "</h3>" +
          '<p class="exh-where">' + escapeHtml(where || "地点未公开") + "</p>" +
          '<p class="exh-snip">' + escapeHtml(h.snippet || "") + "</p>" +
          '<span class="exh-card-foot">查看参展商名单</span>' +
        "</article>"
      );
    }).join("");
  }

  function renderHits(hits, note) {
    lastHits = hits || [];
    const table = document.getElementById("exh-table");
    const wantFairs = role === "buyer";
    if (table) table.classList.toggle("hidden", wantFairs || !lastHits.length);
    if (fairBoard) fairBoard.classList.toggle("hidden", !wantFairs || !lastHits.length);
    if (!lastHits.length) {
      empty.classList.remove("hidden");
      empty.textContent = note || (wantFairs ? "没有公开展会" : "没有公开参展商名单");
      results.innerHTML = "";
      if (fairBoard) fairBoard.innerHTML = "";
      updatePager();
      return;
    }
    empty.classList.add("hidden");
    if (wantFairs) {
      results.innerHTML = "";
      renderFairCards(pagedHits(), fairBoard);
    } else {
      if (fairBoard) fairBoard.innerHTML = "";
      renderExhibitorTable(pagedHits(), results);
    }
    updatePager();
  }

  function loadExhibitors(name, pageUrl) {
    const box = document.getElementById("drawer-exhibitors");
    const st = document.getElementById("drawer-exh-status");
    st.textContent = "正在拉取公开参展商名单…";
    box.innerHTML = "";
    const qs = "/api/v1/discover/exhibition/exhibitors?name=" + encodeURIComponent(name || "") +
      (pageUrl ? "&url=" + encodeURIComponent(pageUrl) : "") + "&limit=200";
    fetch(qs)
      .then(function (r) { return r.json().then(function (j) { return { ok: r.ok, j: j }; }); })
      .then(function (out) {
        const hits = (out.ok && out.j && out.j.hits) || [];
        st.textContent = hits.length
          ? ("公开名单 " + hits.length + " 家（不是全量库）")
          : ((out.j && out.j.note) || "没有公开名单");
        if (!hits.length) {
          box.innerHTML = "";
          return;
        }
        box.innerHTML =
          '<div class="exh-table-wrap"><table class="exh-table"><thead><tr>' +
            "<th>公司</th><th>展位</th><th>国家/展区</th>" +
          "</tr></thead><tbody>" +
          hits.map(function (h) {
            return "<tr><td><b>" + escapeHtml(h.name || "") + "</b></td>" +
              "<td>" + escapeHtml(extra(h, "booth") || "—") + "</td>" +
              "<td>" + escapeHtml(h.country_label || extra(h, "country") || "—") + "</td></tr>";
          }).join("") +
          "</tbody></table></div>";
      })
      .catch(function () {
        st.textContent = "名单加载失败，可打开官网自行查看。";
      });
  }

  if (fairBoard) {
    fairBoard.addEventListener("click", function (ev) {
      const card = ev.target.closest(".exh-card");
      if (!card) return;
      const offset = (page - 1) * PAGE_SIZE;
      const h = lastHits[offset + Number(card.getAttribute("data-i"))];
      if (!h) return;
      const drawer = document.getElementById("exh-drawer");
      drawer.classList.remove("hidden");
      document.getElementById("drawer-kicker").textContent = "展会";
      document.getElementById("drawer-name").textContent = h.name || h.title || "—";
      document.getElementById("drawer-where").textContent = [h.country_label || h.country || "", extra(h, "city")].filter(Boolean).join(" · ");
      document.getElementById("drawer-when").textContent = [extra(h, "start"), extra(h, "end")].filter(Boolean).join(" – ");
      document.getElementById("drawer-snip").textContent = h.snippet || "";
      const link = document.getElementById("drawer-link");
      if (h.homepage_url) {
        link.href = h.homepage_url;
        link.style.display = "";
      } else {
        link.removeAttribute("href");
        link.style.display = "none";
      }
      loadExhibitors(h.name || h.title || "", h.homepage_url || "");
    });
  }

  document.getElementById("drawer-close").addEventListener("click", function () {
    document.getElementById("exh-drawer").classList.add("hidden");
  });

  document.getElementById("page-prev").addEventListener("click", function () {
    if (page > 1) { page -= 1; renderHits(lastHits); }
  });
  document.getElementById("page-next").addEventListener("click", function () {
    if (page < pageCount()) { page += 1; renderHits(lastHits); }
  });

  document.getElementById("reset-btn").addEventListener("click", function () {
    const country = document.getElementById("country");
    if (country) country.value = "";
    syncCountry(country);
  });

  ["country", "landing-country"].forEach(function (id) {
    const el = document.getElementById(id);
    if (!el) return;
    el.addEventListener("change", function () { syncCountry(el); });
  });

  function doSearch(kw) {
    kw = normalizeKeyword(kw);
    if (kw) keyword.value = kw;
    const landingKw = document.getElementById("landing-keyword");
    if (landingKw && kw) landingKw.value = kw;
    if (!kw || kw.length < 2) {
      showKeywordError("请输入行业或展会名");
      toast("请输入行业或展会名");
      return;
    }
    showKeywordError("");
    showResults();
    status.textContent = role === "seller" ? "正在拉取公开参展商名单…" : "正在找公开展会…";
    results.innerHTML = "";
    if (fairBoard) fairBoard.innerHTML = "";
    empty.classList.add("hidden");
    page = 1;
    lastHits = [];
    const btn = document.getElementById("search-btn");
    if (btn) { btn.disabled = true; btn.textContent = "搜索中"; }
    const gen = ++searchGen;
    fetchExhibition(kw, 0, gen, btn);
  }

  function fetchExhibition(kw, attempt, gen, btn) {
    fetch("/api/v1/discover/search", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({
        keyword: kw,
        kind: "exhibition",
        role: role,
        country: selectedCountry(),
        limit: 0,
      }),
    })
      .then(function (r) { return r.json().then(function (j) { return { ok: r.ok, j: j }; }); })
      .then(function (out) {
        if (gen !== searchGen) return;
        if (!out.ok) {
          if (btn) { btn.disabled = false; btn.textContent = "搜索"; }
          status.textContent = (out.j && out.j.message) || "查找失败";
          renderHits([], status.textContent);
          return;
        }
        const hits = out.j.hits || [];
        renderHits(hits, out.j.note);
        if (out.j.refreshing && attempt < 16) {
          status.textContent = hits.length
            ? ("已找到 " + hits.length + (role === "seller" ? " 家客户" : " 场展会") + "，正在补全最新数据…")
            : (role === "seller" ? "正在拉取公开参展商名单…" : "正在找公开展会…");
          if (btn && attempt === 0) { btn.disabled = false; btn.textContent = "搜索"; }
          setTimeout(function () { fetchExhibition(kw, attempt + 1, gen, btn); }, 850);
          return;
        }
        if (btn) { btn.disabled = false; btn.textContent = "搜索"; }
        status.textContent = hits.length
          ? ("已找到 " + hits.length + (role === "seller" ? " 家客户" : " 场展会") + (out.j.cached ? "（即时）" : ""))
          : (out.j.note || "没有命中");
      })
      .catch(function () {
        if (gen !== searchGen) return;
        if (btn) { btn.disabled = false; btn.textContent = "搜索"; }
        status.textContent = "搜索繁忙，请稍后再试。";
      });
  }

  document.getElementById("landing-form").addEventListener("submit", function (ev) {
    ev.preventDefault();
    const lk = document.getElementById("landing-keyword");
    doSearch(lk ? lk.value : keyword.value);
  });
  document.getElementById("exh-form").addEventListener("submit", function (ev) {
    ev.preventDefault();
    doSearch(keyword.value);
  });
})();
