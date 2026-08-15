(function () {
  const keyword = document.getElementById("keyword");
  const status = document.getElementById("status");
  const board = document.getElementById("board");
  const empty = document.getElementById("empty");
  const toastEl = document.getElementById("toast");
  let role = "buyer";
  let lastHits = [];

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

  document.querySelectorAll("#role-group button").forEach(function (btn) {
    btn.addEventListener("click", function () {
      role = btn.getAttribute("data-role") === "seller" ? "seller" : "buyer";
      document.querySelectorAll("#role-group button").forEach(function (el) {
        el.classList.toggle("is-on", el.getAttribute("data-role") === role);
      });
      if (keyword.value) doSearch(keyword.value);
    });
  });

  document.querySelectorAll(".exh-examples button").forEach(function (btn) {
    btn.addEventListener("click", function () { doSearch(btn.getAttribute("data-q") || ""); });
  });

  function renderHits(hits) {
    lastHits = hits || [];
    document.getElementById("hit-count").textContent = lastHits.length ? ("共 " + lastHits.length + " 条") : "";
    if (!lastHits.length) {
      empty.classList.remove("hidden");
      empty.querySelector("strong").textContent = role === "seller" ? "没有公开参展商页" : "没有公开展会";
      board.innerHTML = "";
      board.appendChild(empty);
      return;
    }
    empty.classList.add("hidden");
    board.innerHTML = lastHits.map(function (h, i) {
      const when = [extra(h, "start"), extra(h, "end")].filter(Boolean).join(" – ") || "日期未公开";
      const where = [h.country_label || h.country || "", extra(h, "city")].filter(Boolean).join(" · ");
      const home = h.homepage_url || "";
      return (
        '<article class="exh-card" data-i="' + i + '">' +
          '<span class="exh-when">' + escapeHtml(when) + "</span>" +
          "<h3>" + escapeHtml(h.name || h.title || "未命名") + "</h3>" +
          '<p class="exh-where">' + escapeHtml(where || "地点未公开") + "</p>" +
          '<p class="exh-snip">' + escapeHtml(h.snippet || "") + "</p>" +
          '<span class="exh-card-foot">' + (home ? "打开官网" : "暂无官网") + "</span>" +
        "</article>"
      );
    }).join("");
  }

  board.addEventListener("click", function (ev) {
    const card = ev.target.closest(".exh-card");
    if (!card) return;
    const h = lastHits[Number(card.getAttribute("data-i"))];
    if (!h) return;
    const drawer = document.getElementById("exh-drawer");
    drawer.classList.remove("hidden");
    document.getElementById("drawer-kicker").textContent = role === "seller" ? "参展商" : "展会";
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
  });

  document.getElementById("drawer-close").addEventListener("click", function () {
    document.getElementById("exh-drawer").classList.add("hidden");
  });

  function doSearch(kw) {
    kw = normalizeKeyword(kw);
    const errEl = document.getElementById("keyword-error");
    if (!kw || kw.length < 2) {
      errEl.hidden = false;
      errEl.textContent = "请输入行业或展会名";
      return;
    }
    errEl.hidden = true;
    keyword.value = kw;
    status.textContent = role === "seller" ? "正在找公开参展商页…" : "正在找公开展会…";
    const btn = document.getElementById("search-btn");
    btn.disabled = true;
    fetch("/api/v1/discover/search", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({
        keyword: kw,
        kind: "exhibition",
        role: role,
        country: (document.getElementById("country") || {}).value || "",
        limit: 0,
      }),
    })
      .then(function (r) { return r.json().then(function (j) { return { ok: r.ok, j: j }; }); })
      .then(function (out) {
        btn.disabled = false;
        if (!out.ok) {
          status.textContent = (out.j && out.j.message) || "查找失败";
          renderHits([]);
          return;
        }
        const hits = out.j.hits || [];
        status.textContent = hits.length
          ? ("已找到 " + hits.length + (role === "seller" ? " 个参展商线索" : " 场展会"))
          : (out.j.note || "没有命中");
        renderHits(hits);
      })
      .catch(function () {
        btn.disabled = false;
        status.textContent = "搜索繁忙，请稍后再试。";
      });
  }

  document.getElementById("exh-form").addEventListener("submit", function (ev) {
    ev.preventDefault();
    doSearch(keyword.value);
  });
})();
