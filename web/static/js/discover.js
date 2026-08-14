(function () {
  const tab = document.body.getAttribute("data-tab") || "people";
  const form = document.getElementById("discover-form");
  const status = document.getElementById("discover-status");
  const warnings = document.getElementById("discover-warnings");
  const results = document.getElementById("discover-results");
  const empty = document.getElementById("discover-empty");
  const foot = document.getElementById("discover-foot");
  const title = document.getElementById("discover-title");
  const platformGroup = document.getElementById("platform-group");
  const keyword = document.getElementById("keyword");

  document.querySelectorAll(".rail-item[data-nav]").forEach(function (el) {
    if (el.getAttribute("data-nav") === tab) {
      el.classList.add("is-active");
    }
  });

  const copy = {
    people: {
      title: "智能引擎搜索",
      placeholder: "请输入企业或商品名称，如 power tools",
      status: "输入关键词后从 Facebook / LinkedIn / Instagram / YouTube / TikTok 等海外社媒检索公开主页",
    },
    exhibition: {
      title: "展会获客",
      placeholder: "例如：Canton Fair / CES",
      status: "没有高 star、仍在维护、可商用的开源展会库。按优先级先不自研。",
    },
    customs: {
      title: "海关数据",
      placeholder: "例如：Allbirds / HS 6404",
      status: "没有高 star 开源海关库可克隆。逐票提单已在 PR #12，这里不自研爬虫。",
    },
  };

  const cfg = copy[tab] || copy.people;
  title.textContent = cfg.title;
  keyword.placeholder = cfg.placeholder;
  status.textContent = cfg.status;
  if (tab !== "people") {
    platformGroup.classList.add("hidden");
  }

  document.querySelectorAll(".plat-chip").forEach(function (btn) {
    btn.addEventListener("click", function () {
      btn.classList.toggle("is-on");
    });
  });

  if (window.lucide) {
    window.lucide.createIcons();
  }

  form.addEventListener("submit", function (ev) {
    ev.preventDefault();
    const kw = keyword.value.trim();
    if (!kw) return;

    const platforms = Array.prototype.map.call(
      document.querySelectorAll(".plat-chip.is-on"),
      function (el) { return el.getAttribute("data-platform"); }
    );

    status.textContent = "正在检索公开主页…";
    warnings.classList.add("hidden");
    warnings.textContent = "";
    results.innerHTML = "";
    empty.classList.add("hidden");
    foot.classList.add("hidden");

    fetch("/api/v1/discover/search", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({
        keyword: kw,
        kind: tab,
        platforms: tab === "people" ? platforms : [],
        limit: Number(document.getElementById("limit").value) || 20,
      }),
    })
      .then(function (r) { return r.json().then(function (j) { return { ok: r.ok, j: j }; }); })
      .then(function (out) {
        if (!out.ok) {
          status.textContent = out.j.message || "搜索失败";
          empty.classList.remove("hidden");
          empty.textContent = status.textContent;
          return;
        }
        const data = out.j;
        status.textContent = "来源 " + ((data.sources || []).join("、") || "—") +
          " · " + (data.hits || []).length + " 条 · " + (data.took_ms || 0) + "ms" +
          (data.note ? " · " + data.note : "");
        if (data.warnings && data.warnings.length) {
          warnings.classList.remove("hidden");
          warnings.textContent = data.warnings.join("\n");
        }
        renderHits(data.hits || []);
      })
      .catch(function (err) {
        status.textContent = "请求失败：" + err.message;
      });
  });

  function renderHits(hits) {
    if (!hits.length) {
      empty.classList.remove("hidden");
      empty.textContent = "没有命中。可换关键词或勾选更多平台。";
      foot.classList.add("hidden");
      return;
    }
    empty.classList.add("hidden");
    results.innerHTML = hits.map(function (h) {
      const handle = h.handle ? "@" + h.handle : "—";
      const home = h.homepage_url || "";
      const msg = h.message_url || home;
      const plat = (h.platform || "").toLowerCase();
      return (
        "<tr>" +
          '<td><input type="checkbox" disabled></td>' +
          '<td class="hit-name">' + escapeHtml(h.name || handle) + "</td>" +
          '<td><span class="hit-badge is-' + escapeAttr(plat) + '">' + escapeHtml(h.platform || "") + "</span></td>" +
          "<td>" + escapeHtml(handle) + "</td>" +
          "<td>" + (home
            ? '<a class="hit-home" target="_blank" rel="noopener" href="' + escapeAttr(home) + '">' + escapeHtml(home.replace(/^https?:\/\/(www\.)?/, "")) + "</a>"
            : "—") + "</td>" +
          '<td class="hit-snip" title="' + escapeAttr(h.snippet || h.message_hint || "") + '">' +
            escapeHtml(h.snippet || h.message_hint || "") + "</td>" +
          '<td class="row-actions">' +
            '<a class="btn-home" target="_blank" rel="noopener" href="' + escapeAttr(home || "#") + '">打开主页</a>' +
            '<a class="btn-msg" target="_blank" rel="noopener" href="' + escapeAttr(msg || "#") + '" title="' +
              escapeAttr(h.message_hint || "") + '">去私信</a>' +
          "</td>" +
        "</tr>"
      );
    }).join("");
    foot.classList.remove("hidden");
    foot.textContent = "共 " + hits.length + " 条";
  }

  function escapeHtml(s) {
    return String(s || "").replace(/[&<>"']/g, function (c) {
      return ({ "&": "&amp;", "<": "&lt;", ">": "&gt;", '"': "&quot;", "'": "&#39;" })[c];
    });
  }

  function escapeAttr(s) {
    return escapeHtml(s).replace(/`/g, "");
  }
})();
