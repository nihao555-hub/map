(function () {
  const keyword = document.getElementById("keyword");
  const status = document.getElementById("status");
  const results = document.getElementById("results");
  const empty = document.getElementById("empty");
  const toastEl = document.getElementById("toast");
  const PAGE_SIZE = 20;

  let role = "buyer";
  let lastHits = [];
  let page = 1;
  let lastName = "";

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

  function validateKeyword(s) {
    s = normalizeKeyword(s);
    if (!s) return "请输入产品、企业或 HS 编码";
    if (s.length < 2) return "关键词至少 2 个字";
    if (s.length > 64) return "关键词过长";
    return "";
  }

  function showKeywordError(msg) {
    const el = document.getElementById("keyword-error");
    if (!el) return;
    el.hidden = !msg;
    el.textContent = msg || "";
  }

  function flagFor(code) {
    const c = String(code || "").toUpperCase();
    if (!c || c.length !== 2) return "🏳️";
    return String.fromCodePoint(127397 + c.charCodeAt(0), 127397 + c.charCodeAt(1));
  }

  function isLogistics(name) {
    return /logistics|freight|shipping|forwarder|courier|物流|货运|船务|报关/i.test(name || "");
  }

  function selectedYear() {
    const el = document.getElementById("year");
    return parseInt((el && el.value) || "0", 10) || 0;
  }

  function fillYears() {
    const el = document.getElementById("year");
    if (!el) return;
    const now = new Date().getUTCFullYear();
    const opts = [];
    for (let y = now; y >= now - 5; y--) {
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

  function loadCountries() {
    return fetch("/api/v1/discover/countries")
      .then(function (r) { return r.json(); })
      .then(function (j) { if (j && j.countries) fillCountrySelect(j.countries); })
      .catch(function () {});
  }

  function showResults() {
    const landing = document.getElementById("landing-view");
    const view = document.getElementById("results-view");
    if (landing) landing.hidden = true;
    if (view) view.hidden = false;
    document.body.classList.remove("is-landing");
  }

  function setRole(next) {
    role = next === "seller" ? "seller" : "buyer";
    document.querySelectorAll("[data-role]").forEach(function (el) {
      el.classList.toggle("is-on", el.getAttribute("data-role") === role);
    });
  }

  document.querySelectorAll("[data-role]").forEach(function (btn) {
    btn.addEventListener("click", function () {
      const prev = role;
      setRole(btn.getAttribute("data-role"));
      if (prev !== role && keyword.value) doSearch(keyword.value);
    });
  });

  function extra(h, key) {
    return (h && h.extra && h.extra[key]) || "";
  }

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
      empty.textContent = note || (role === "seller"
        ? "没有命中公开发货人。可换产品词，或把国家改成不限。"
        : "没有命中公开进口商。逐票买家目前来自美国海关公开提单。");
      results.innerHTML = "";
      updatePager();
      return;
    }
    empty.classList.add("hidden");
    results.innerHTML = pagedHits().map(function (h, i) {
      const match = extra(h, "matching") || extra(h, "shipments") || "—";
      const total = extra(h, "shipments") || "—";
      const focus = extra(h, "focus");
      const product = extra(h, "product");
      const hs = extra(h, "hs") || "—";
      const when = extra(h, "last_date") || extra(h, "year") || "—";
      const weight = extra(h, "weight_kg");
      const geo = h.country_label || h.country || "";
      const sub = [geo, product].filter(Boolean).join(" · ");
      return (
        "<tr data-name=\"" + escapeAttr(h.name) + "\">" +
          '<td class="col-check"><input type="checkbox" class="mkt-pick" data-i="' + i + '" data-name="' + escapeAttr(h.name) + '"></td>' +
          "<td><div class=\"cus-co\">" +
            '<span class="cus-flag">' + flagFor(h.country) + "</span>" +
            "<div><b>" + escapeHtml(h.name) + "</b><small>" + escapeHtml(sub) + "</small></div>" +
          "</div></td>" +
          "<td>" + escapeHtml(hs) + "</td>" +
          "<td>" + escapeHtml(match) + (focus ? "<small style=\"display:block;color:#8c8c8c\">专注 " + escapeHtml(focus) + "%</small>" : "") +
            (total !== "—" && total !== match ? "<small style=\"display:block;color:#8c8c8c\">全部 " + escapeHtml(total) + "</small>" : "") + "</td>" +
          "<td class=\"cus-amt-miss\" title=\"公开提单未提供金额\">—" +
            (weight ? "<small style=\"display:block\">重量 " + escapeHtml(weight) + " kg</small>" : "") + "</td>" +
          "<td>" + escapeHtml(when) + "</td>" +
        "</tr>"
      );
    }).join("");
    updatePager();
  }

  function searchTerm() {
    const hs = normalizeKeyword((document.getElementById("hs") || {}).value);
    const field = (document.getElementById("search-field") || {}).value;
    const kw = normalizeKeyword(keyword.value);
    if (hs) return hs;
    if (field === "hs") return kw;
    return kw;
  }

  function doSearch(kw) {
    if (kw) keyword.value = kw;
    const term = searchTerm();
    const err = validateKeyword(term);
    if (err) {
      showKeywordError(err);
      toast(err);
      return;
    }
    showKeywordError("");
    showResults();
    status.textContent = "正在查询公开提单…";
    results.innerHTML = "";
    empty.classList.add("hidden");
    page = 1;
    lastHits = [];
    const btn = document.getElementById("search-btn");
    if (btn) { btn.disabled = true; btn.textContent = "搜索中"; }

    fetch("/api/v1/discover/search", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({
        keyword: term,
        kind: "customs",
        role: role,
        country: (document.getElementById("country") || {}).value || "",
        year: selectedYear(),
        precise: !!(document.getElementById("precise") || {}).checked,
        limit: 0,
      }),
    })
      .then(function (r) { return r.json().then(function (j) { return { ok: r.ok, j: j }; }); })
      .then(function (out) {
        if (btn) { btn.disabled = false; btn.textContent = "搜索"; }
        if (!out.ok) {
          status.textContent = (out.j && out.j.message) || "查询失败";
          empty.classList.remove("hidden");
          empty.textContent = status.textContent;
          return;
        }
        let hits = out.j.hits || [];
        if ((document.getElementById("exclude-logistics") || {}).checked) {
          hits = hits.filter(function (h) { return !isLogistics(h.name); });
        }
        lastHits = hits;
        status.textContent = hits.length
          ? ("已找到 " + hits.length + " 家企业")
          : (out.j.note || "没有命中");
        renderHits(hits, out.j.note);
      })
      .catch(function () {
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
    if (lf) document.getElementById("search-field").value = lf.value;
    if (lp) document.getElementById("precise").checked = lp.checked;
    doSearch(keyword.value);
  });

  document.getElementById("customs-form").addEventListener("submit", function (ev) {
    ev.preventDefault();
    doSearch(keyword.value);
  });

  document.getElementById("reset-btn").addEventListener("click", function () {
    document.getElementById("country").selectedIndex = 0;
    document.getElementById("year").selectedIndex = 0;
    document.getElementById("hs").value = "";
    document.getElementById("exclude-logistics").checked = true;
  });

  document.getElementById("page-prev").addEventListener("click", function () {
    if (page > 1) { page -= 1; renderHits(lastHits); }
  });
  document.getElementById("page-next").addEventListener("click", function () {
    if (page < pageCount()) { page += 1; renderHits(lastHits); }
  });
  document.getElementById("select-all").addEventListener("click", function () {
    const boxes = document.querySelectorAll(".mkt-pick");
    const allOn = Array.prototype.every.call(boxes, function (b) { return b.checked; });
    boxes.forEach(function (b) { b.checked = !allOn; });
  });
  document.getElementById("market-btn").addEventListener("click", function () {
    toast("公开提单没有现成邮箱。请到智能引擎按公司名抽取公开联系方式。系统不代发。");
  });
  document.getElementById("book-btn").addEventListener("click", function () {
    const names = Array.prototype.map.call(document.querySelectorAll(".mkt-pick:checked"), function (el) {
      return el.getAttribute("data-name");
    }).filter(Boolean);
    if (!names.length) { toast("请先勾选企业"); return; }
    if (navigator.clipboard && navigator.clipboard.writeText) {
      navigator.clipboard.writeText(names.join("\n")).then(function () {
        toast("已复制 " + names.length + " 家企业名称");
      });
      return;
    }
    toast("请手动复制企业名称");
  });

  results.addEventListener("click", function (ev) {
    if (ev.target.closest("input")) return;
    const tr = ev.target.closest("tr[data-name]");
    if (!tr) return;
    openProfile(tr.getAttribute("data-name"));
  });

  function setTab(id) {
    document.querySelectorAll("#modal-tabs button").forEach(function (b) {
      b.classList.toggle("is-on", b.getAttribute("data-tab") === id);
    });
    ["basic", "contacts", "records", "suppliers", "ship", "chain"].forEach(function (t) {
      const el = document.getElementById("tab-" + t);
      if (el) el.classList.toggle("hidden", t !== id);
    });
  }

  document.querySelectorAll("#modal-tabs button").forEach(function (btn) {
    btn.addEventListener("click", function () { setTab(btn.getAttribute("data-tab")); });
  });

  function openProfile(name) {
    lastName = name;
    const modal = document.getElementById("cus-modal");
    modal.classList.remove("hidden");
    document.getElementById("modal-name").textContent = name;
    document.getElementById("modal-role").textContent = role === "seller" ? "供应商" : "采购商";
    setTab("basic");
    fetch("/api/v1/discover/customs/profile?name=" + encodeURIComponent(name) + "&year=" + selectedYear())
      .then(function (r) { return r.json().then(function (j) { return { ok: r.ok, j: j }; }); })
      .then(function (out) {
        const p = out.ok ? out.j : {};
        document.getElementById("stat-supplier").textContent = (p.suppliers && p.suppliers[0] && p.suppliers[0].name) || "—";
        document.getElementById("stat-count").textContent = p.total_shipments || "—";
        document.getElementById("stat-time").textContent = (p.shipments && p.shipments[0] && p.shipments[0].date) || extraYear();
        document.getElementById("info-country").textContent = p.country || "美国";
        document.getElementById("info-address").textContent = p.address || "—";
        document.getElementById("info-shipments").textContent = p.total_shipments || "—";
        document.getElementById("info-year").textContent = (p.year_from || "") + (p.year_to ? " – " + p.year_to : "");
        const prod = firstProduct(p);
        document.getElementById("info-product").textContent = prod.product || "—";
        document.getElementById("info-hs").textContent = prod.hs || "—";
        document.getElementById("tab-suppliers").innerHTML = renderPartners(p.suppliers, "供应商");
        document.getElementById("tab-ship").innerHTML = renderShipTab(p);
        document.getElementById("tab-records").innerHTML = renderRecords(p);
        document.getElementById("modal-open").onclick = function () {
          if (p.homepage_url) window.open(p.homepage_url, "_blank", "noopener");
          else toast("没有可打开的详情页");
        };
      })
      .catch(function () { toast("详情加载失败"); });
  }

  function extraYear() {
    return String(selectedYear() || "—");
  }

  function firstProduct(p) {
    const list = (p && p.products) || [];
    let hs = "";
    let product = "";
    list.forEach(function (item) {
      const code = (item && item.code) || "";
      if (!hs && /^\d/.test(code)) hs = code;
      if (!product && code && !/^\d/.test(code)) product = code;
    });
    if (!product && list.length) product = list[0].code || "";
    if (!product && p && p.shipments && p.shipments[0]) product = p.shipments[0].product || "";
    if (!hs && p && p.shipments && p.shipments[0]) hs = p.shipments[0].hs_code || "";
    return { product: product, hs: hs };
  }

  function renderPartners(list, title) {
    if (!list || !list.length) return "暂无" + title + "汇总。";
    return "<table><thead><tr><th>名称</th><th>国家</th><th>提单</th></tr></thead><tbody>" +
      list.map(function (p) {
        return "<tr><td>" + escapeHtml(p.name) + "</td><td>" + escapeHtml(p.country || "—") +
          "</td><td>" + escapeHtml(String(p.shipments || "—")) + "</td></tr>";
      }).join("") + "</tbody></table>";
  }

  function renderProducts(list) {
    if (!list || !list.length) return "";
    return "<h4>Top 产品 / HS</h4><table><thead><tr><th>编码或描述</th><th>提单</th></tr></thead><tbody>" +
      list.map(function (p) {
        return "<tr><td>" + escapeHtml(p.code || "—") + "</td><td>" + escapeHtml(String(p.shipments || "—")) + "</td></tr>";
      }).join("") + "</tbody></table>";
  }

  function renderShipments(list) {
    if (!list || !list.length) return "暂无近期提单。";
    return "<table><thead><tr><th>日期</th><th>发货人</th><th>产品</th><th>HS</th><th>船名</th></tr></thead><tbody>" +
      list.map(function (s) {
        return "<tr><td>" + escapeHtml(s.date || "—") + "</td><td>" + escapeHtml(s.shipper || "—") +
          "</td><td>" + escapeHtml(s.product || "—") + "</td><td>" + escapeHtml(s.hs_code || "—") +
          "</td><td>" + escapeHtml(s.vessel || "—") + "</td></tr>";
      }).join("") + "</tbody></table>";
  }

  function renderRecords(p) {
    return renderProducts(p.products) + renderShipments(p.shipments);
  }

  function renderShipTab(p) {
    const ships = renderShipments(p.shipments);
    const carriers = renderPartners(p.carriers, "承运人");
    return ships + (p.carriers && p.carriers.length ? "<h4>承运人</h4>" + carriers : "");
  }

  document.getElementById("modal-close").addEventListener("click", function () {
    document.getElementById("cus-modal").classList.add("hidden");
  });
  document.getElementById("cus-modal").addEventListener("click", function (ev) {
    if (ev.target.id === "cus-modal") ev.currentTarget.classList.add("hidden");
  });
  document.getElementById("modal-save").addEventListener("click", function () {
    if (!lastName) return;
    if (navigator.clipboard && navigator.clipboard.writeText) {
      navigator.clipboard.writeText(lastName).then(function () { toast("已复制企业名，可自行录入客户"); });
    }
  });

  fillYears();
  loadCountries();
  setRole("buyer");
})();
