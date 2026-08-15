(function () {
  const keyword = document.getElementById("keyword");
  const status = document.getElementById("status");
  const results = document.getElementById("results");
  const empty = document.getElementById("empty");
  const toastEl = document.getElementById("toast");
  const PAGE_SIZE = 20;

  let role = "seller";
  let lastHits = [];
  let page = 1;
  let lastName = "";
  let lastUrl = "";
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

  function flagFor(code) {
    const c = String(code || "").toUpperCase();
    if (!c || c.length !== 2) return "🏳️";
    return String.fromCodePoint(127397 + c.charCodeAt(0), 127397 + c.charCodeAt(1));
  }

  function showKeywordError(msg) {
    const el = document.getElementById("keyword-error");
    if (!el) return;
    el.hidden = !msg;
    el.textContent = msg || "";
  }

  function fillYears() {
    const el = document.getElementById("year");
    if (!el) return;
    const now = new Date().getUTCFullYear();
    const opts = ['<option value="0">不限</option>'];
    for (let y = now + 1; y >= now - 1; y--) {
      opts.push('<option value="' + y + '">' + y + "</option>");
    }
    el.innerHTML = opts.join("");
  }

  function fillCountrySelect(list) {
    const html = (list || []).map(function (c) {
      return '<option value="' + escapeAttr(c.code || "") + '">' + escapeHtml(c.label || c.code || "不限") + "</option>";
    }).join("");
    const el = document.getElementById("country");
    if (el) el.innerHTML = html;
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
    const head = document.getElementById("exh-thead");
    if (head) {
      head.innerHTML = role === "buyer"
        ? "<tr><th style=\"width:36px\"></th><th>展会名称</th><th style=\"width:160px\">举办时间</th><th style=\"width:180px\">国家/城市</th><th style=\"width:120px\">来源</th></tr>"
        : "<tr><th style=\"width:36px\"></th><th>公司名称</th><th>展会名称</th><th style=\"width:120px\">展位号</th><th style=\"width:140px\">国家/地区</th></tr>";
    }
  }

  function selectedCountry() {
    return (document.getElementById("country") || {}).value || "";
  }

  function selectedYear() {
    return parseInt((document.getElementById("year") || {}).value || "0", 10) || 0;
  }

  function searchField() {
    return (document.getElementById("search-field") || {}).value || "product";
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

  function renderHits(hits, note) {
    lastHits = hits || [];
    if (!lastHits.length) {
      empty.classList.remove("hidden");
      empty.textContent = note || (role === "buyer" ? "没有公开展会" : "没有公开参展商名单");
      results.innerHTML = "";
      updatePager();
      return;
    }
    empty.classList.add("hidden");
    const wantFairs = role === "buyer";
    results.innerHTML = pagedHits().map(function (h, i) {
      const geo = h.country_label || h.country || extra(h, "country") || "—";
      const city = extra(h, "city");
      const where = [geo, city].filter(Boolean).join(" · ");
      const when = [extra(h, "start"), extra(h, "end")].filter(Boolean).join(" – ") || "—";
      if (wantFairs) {
        return (
          "<tr data-i=\"" + i + "\" data-kind=\"fair\">" +
            '<td class="col-check"><input type="checkbox" class="mkt-pick" data-i="' + i + '" data-name="' + escapeAttr(h.name || h.title || "") + '"></td>' +
            "<td><div class=\"cus-co\">" +
              '<span class="cus-flag">' + flagFor(h.country) + "</span>" +
              "<div><b>" + escapeHtml(h.name || h.title || "") + "</b><small>" + escapeHtml(h.snippet || "") + "</small></div>" +
            "</div></td>" +
            "<td>" + escapeHtml(when) + "</td>" +
            "<td>" + escapeHtml(where) + "</td>" +
            "<td>" + escapeHtml(extra(h, "via") || h.source || "—") + "</td>" +
          "</tr>"
        );
      }
      return (
        "<tr data-i=\"" + i + "\" data-kind=\"exhibitor\">" +
          '<td class="col-check"><input type="checkbox" class="mkt-pick" data-i="' + i + '" data-name="' + escapeAttr(h.name || "") + '"></td>' +
          "<td><div class=\"cus-co\">" +
            '<span class="cus-flag">' + flagFor(h.country) + "</span>" +
            "<div><b>" + escapeHtml(h.name || "") + "</b></div>" +
          "</div></td>" +
          "<td>" + escapeHtml(extra(h, "fair") || "—") + "</td>" +
          "<td>" + escapeHtml(extra(h, "booth") || "—") + "</td>" +
          "<td>" + escapeHtml(geo) + "</td>" +
        "</tr>"
      );
    }).join("");
    updatePager();
  }

  function setTab(id) {
    document.querySelectorAll("#modal-tabs button").forEach(function (b) {
      b.classList.toggle("is-on", b.getAttribute("data-tab") === id);
    });
    ["basic", "contacts", "fair", "list"].forEach(function (t) {
      const el = document.getElementById("tab-" + t);
      if (el) el.classList.toggle("hidden", t !== id);
    });
  }

  document.querySelectorAll("#modal-tabs button").forEach(function (btn) {
    btn.addEventListener("click", function () { setTab(btn.getAttribute("data-tab")); });
  });

  function openModal(h, kind) {
    lastName = h.name || h.title || "";
    lastUrl = h.homepage_url || "";
    const modal = document.getElementById("exh-modal");
    modal.classList.remove("hidden");
    document.getElementById("modal-name").textContent = lastName || "—";
    document.getElementById("modal-role").textContent = kind === "fair" ? "展会" : "参展商";
    document.getElementById("stat-fair").textContent = extra(h, "fair") || (kind === "fair" ? lastName : "—");
    document.getElementById("stat-booth").textContent = extra(h, "booth") || "—";
    document.getElementById("stat-country").textContent = h.country_label || h.country || extra(h, "city") || "—";
    document.getElementById("stat-when").textContent = [extra(h, "start"), extra(h, "end")].filter(Boolean).join(" – ") || "—";
    document.getElementById("info-name").textContent = lastName || "—";
    document.getElementById("info-city").textContent = extra(h, "city") || "—";
    document.getElementById("info-country").textContent = h.country_label || h.country || "—";
    document.getElementById("info-booth").textContent = extra(h, "booth") || "—";
    document.getElementById("info-via").textContent = extra(h, "via") || h.source || "—";
    document.getElementById("info-snip").textContent = h.snippet || "—";
    document.getElementById("tab-fair").innerHTML = kind === "fair"
      ? ("<p>" + escapeHtml(h.snippet || "公开目录中的展会场次。") + "</p>")
      : ("<table><thead><tr><th>展会</th><th>展位</th><th>国家</th></tr></thead><tbody><tr><td>" +
        escapeHtml(extra(h, "fair") || "—") + "</td><td>" + escapeHtml(extra(h, "booth") || "—") +
        "</td><td>" + escapeHtml(h.country_label || "—") + "</td></tr></tbody></table>");
    document.getElementById("list-status").textContent = kind === "fair" ? "正在拉取公开参展商名单…" : "参展商名单只在搜展会时按场次加载。";
    document.getElementById("list-body").innerHTML = "";
    setTab(kind === "fair" ? "list" : "basic");
    document.getElementById("modal-open").onclick = function () {
      if (lastUrl) window.open(lastUrl, "_blank", "noopener");
      else toast("没有可打开的详情页");
    };
    if (kind === "fair") loadExhibitors(lastName, lastUrl);
  }

  function loadExhibitors(name, pageUrl) {
    const st = document.getElementById("list-status");
    const box = document.getElementById("list-body");
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
        box.innerHTML = "<table><thead><tr><th>公司</th><th>展位</th><th>国家/展区</th></tr></thead><tbody>" +
          hits.map(function (h) {
            return "<tr><td><b>" + escapeHtml(h.name || "") + "</b></td>" +
              "<td>" + escapeHtml(extra(h, "booth") || "—") + "</td>" +
              "<td>" + escapeHtml(h.country_label || extra(h, "country") || "—") + "</td></tr>";
          }).join("") + "</tbody></table>";
      })
      .catch(function () {
        st.textContent = "名单加载失败，可打开官网自行查看。";
      });
  }

  results.addEventListener("click", function (ev) {
    if (ev.target.closest("input")) return;
    const tr = ev.target.closest("tr[data-i]");
    if (!tr) return;
    const offset = (page - 1) * PAGE_SIZE;
    const h = lastHits[offset + Number(tr.getAttribute("data-i"))];
    if (!h) return;
    openModal(h, tr.getAttribute("data-kind") || (role === "buyer" ? "fair" : "exhibitor"));
  });

  document.getElementById("modal-close").addEventListener("click", function () {
    document.getElementById("exh-modal").classList.add("hidden");
  });
  document.getElementById("exh-modal").addEventListener("click", function (ev) {
    if (ev.target.id === "exh-modal") ev.currentTarget.classList.add("hidden");
  });
  document.getElementById("modal-save").addEventListener("click", function () {
    if (!lastName) return;
    if (navigator.clipboard && navigator.clipboard.writeText) {
      navigator.clipboard.writeText(lastName).then(function () { toast("已复制名称，可自行录入客户"); });
    }
  });

  document.getElementById("select-all").addEventListener("click", function () {
    const boxes = document.querySelectorAll(".mkt-pick");
    const allOn = Array.prototype.every.call(boxes, function (b) { return b.checked; });
    boxes.forEach(function (b) { b.checked = !allOn; });
  });
  document.getElementById("market-btn").addEventListener("click", function () {
    toast("公开名录没有现成邮箱。请到智能引擎按公司名抽取公开联系方式。系统不代发。");
  });
  document.getElementById("book-btn").addEventListener("click", function () {
    const names = Array.prototype.map.call(document.querySelectorAll(".mkt-pick:checked"), function (el) {
      return el.getAttribute("data-name");
    }).filter(Boolean);
    if (!names.length) { toast("请先勾选"); return; }
    if (navigator.clipboard && navigator.clipboard.writeText) {
      navigator.clipboard.writeText(names.join("\n")).then(function () {
        toast("已复制 " + names.length + " 条名称");
      });
      return;
    }
    toast("请手动复制名称");
  });

  document.getElementById("page-prev").addEventListener("click", function () {
    if (page > 1) { page -= 1; renderHits(lastHits); }
  });
  document.getElementById("page-next").addEventListener("click", function () {
    if (page < pageCount()) { page += 1; renderHits(lastHits); }
  });
  document.getElementById("reset-btn").addEventListener("click", function () {
    const country = document.getElementById("country");
    const year = document.getElementById("year");
    if (country) country.selectedIndex = 0;
    if (year) year.selectedIndex = 0;
  });

  function doSearch(kw) {
    kw = normalizeKeyword(kw);
    if (kw) keyword.value = kw;
    const landingKw = document.getElementById("landing-keyword");
    if (landingKw && kw) landingKw.value = kw;
    if (!kw || kw.length < 2) {
      showKeywordError("请输入产品、展会或公司名");
      toast("请输入产品、展会或公司名");
      return;
    }
    showKeywordError("");
    showResults();
    status.textContent = role === "seller" ? "正在拉取公开参展商名单…" : "正在找公开展会…";
    results.innerHTML = "";
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
        year: selectedYear(),
        precise: !!(document.getElementById("precise") || {}).checked,
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
    const lf = document.getElementById("landing-field");
    const lp = document.getElementById("landing-precise");
    if (lk) keyword.value = lk.value;
    if (lf) {
      document.getElementById("search-field").value = lf.value;
      if (lf.value === "fair") setRole("buyer");
      else setRole("seller");
    }
    if (lp) document.getElementById("precise").checked = lp.checked;
    doSearch(keyword.value);
  });
  document.getElementById("exh-form").addEventListener("submit", function (ev) {
    ev.preventDefault();
    if (searchField() === "fair") setRole("buyer");
    doSearch(keyword.value);
  });

  fillYears();
  setRole("seller");
})();
