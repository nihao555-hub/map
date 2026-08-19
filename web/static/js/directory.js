(function () {
  const form = document.getElementById("dir-form");
  const rowsEl = document.getElementById("dir-rows");
  const status = document.getElementById("dir-status");
  const note = document.getElementById("dir-note");
  const cards = document.getElementById("dir-cards");
  const pager = document.getElementById("dir-pager");
  const hitCount = document.getElementById("dir-hit-count");
  const pageCur = document.getElementById("dir-page-cur");
  const pageTotal = document.getElementById("dir-page-total");
  const PAGE = 20;
  let page = 1;
  let total = 0;

  function fmt(n) {
    return Number(n || 0).toLocaleString("en-US");
  }

  function homeLabel(url) {
    const u = String(url || "");
    if (!u) return "—";
    if (u.indexOf("gleif.org") >= 0) return "GLEIF 登记页";
    if (u.indexOf("openstreetmap.org") >= 0) return "OSM 地图点";
    return u.replace(/^https?:\/\//, "").replace(/\/$/, "");
  }

  function renderCards(cov) {
    const items = [
      [cov.merchants, "库内全部"],
      [cov.legal_name_only, "只有法律名"],
      [cov.tiktok_unique, "TikTok 去重主页"],
      [cov.douyin_unique, "抖音 去重主页"],
    ];
    cards.innerHTML = items.map(function (it) {
      return "<div class=\"dir-card\"><b>" + fmt(it[0]) + "</b><span>" + it[1] + "</span></div>";
    }).join("");
  }

  function renderRows(rows) {
    if (!rows || !rows.length) {
      rowsEl.innerHTML = "<tr><td colspan=\"6\">这一页没有记录。换个筛选，或先跑抖音/TikTok 入库。</td></tr>";
      return;
    }
    rowsEl.innerHTML = rows.map(function (row) {
      const plats = (row.Profiles || row.profiles || []).map(function (p) {
        return p.platform || p.Platform;
      }).filter(function (p) { return p && p !== "website"; }).join(" ");
      const home = row.Homepage || row.homepage || "";
      return "<tr>" +
        "<td>" + escapeHtml(row.Name || row.name || "") + "</td>" +
        "<td>" + escapeHtml(row.Source || row.source || "") + "</td>" +
        "<td>" + escapeHtml(row.Shop || row.shop || "") + "</td>" +
        "<td>" + escapeHtml(row.Country || row.country || "") + "</td>" +
        "<td>" + (home ? "<a class=\"dir-home\" href=\"" + escapeHtml(home) + "\" target=\"_blank\" rel=\"noreferrer\">" + escapeHtml(homeLabel(home)) + "</a>" : "—") + "</td>" +
        "<td class=\"dir-plats\">" + escapeHtml(plats || "—") + "</td>" +
        "</tr>";
    }).join("");
  }

  function escapeHtml(s) {
    return String(s).replace(/[&<>"']/g, function (ch) {
      return ({ "&": "&amp;", "<": "&lt;", ">": "&gt;", "\"": "&quot;", "'": "&#39;" })[ch];
    });
  }

  function load() {
    const filter = document.getElementById("dir-filter").value;
    const source = document.getElementById("dir-source").value;
    const q = document.getElementById("dir-q").value.trim();
    const offset = (page - 1) * PAGE;
    const url = "/api/v1/discover/directory?filter=" + encodeURIComponent(filter) +
      "&source=" + encodeURIComponent(source) +
      "&q=" + encodeURIComponent(q) +
      "&limit=" + PAGE + "&offset=" + offset;
    status.textContent = "正在读取本地库…";
    fetch(url).then(function (res) { return res.json(); }).then(function (data) {
      if (data && data.message && !data.coverage) {
        status.textContent = data.message;
        return;
      }
      total = data.total || 0;
      if (data.coverage) renderCards(data.coverage);
      if (data.coverage && data.coverage.note) note.textContent = data.coverage.note;
      if (data.note) status.textContent = data.note;
      renderRows(data.rows || []);
      const pages = Math.max(1, Math.ceil(total / PAGE));
      hitCount.textContent = "共 " + fmt(total) + " 条";
      pageCur.textContent = String(page);
      pageTotal.textContent = String(pages);
      pager.hidden = total <= 0;
      document.getElementById("dir-prev").disabled = page <= 1;
      document.getElementById("dir-next").disabled = page >= pages;
    }).catch(function () {
      status.textContent = "本地库暂时读不到。确认 store/merchants.db 还在。";
    });
  }

  form.addEventListener("submit", function (ev) {
    ev.preventDefault();
    page = 1;
    load();
  });
  document.getElementById("dir-prev").addEventListener("click", function () {
    if (page > 1) { page -= 1; load(); }
  });
  document.getElementById("dir-next").addEventListener("click", function () {
    page += 1;
    load();
  });
  load();
})();
