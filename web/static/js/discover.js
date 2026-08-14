(function () {
  const tab = document.body.getAttribute("data-tab") || "people";
  const form = document.getElementById("discover-form");
  const status = document.getElementById("discover-status");
  const warnings = document.getElementById("discover-warnings");
  const results = document.getElementById("discover-results");
  const title = document.getElementById("discover-title");
  const sub = document.getElementById("discover-sub");
  const platformGroup = document.getElementById("platform-group");
  const keyword = document.getElementById("keyword");

  document.querySelectorAll(".mod-nav-item[data-nav]").forEach(function (el) {
    if (el.getAttribute("data-nav") === tab) {
      el.classList.add("is-active");
    }
  });

  const copy = {
    people: {
      title: "智能引擎搜索",
      sub: "输入关键词，调用 TikTok-Api / f2 找到公开主页。私信请在官方页面手动发送。",
      placeholder: "例如：power tools importer",
    },
    exhibition: {
      title: "展会获客",
      sub: "没有高 star、仍在维护、可商用的开源展会库。按优先级先不自研，下一步再接第三方 API。",
      placeholder: "例如：Canton Fair / CES",
    },
    customs: {
      title: "海关数据",
      sub: "没有高 star 开源海关库可克隆。逐票提单已在 PR #12 用 Kirchner / ImportYeti，这里不自研爬虫。",
      placeholder: "例如：Allbirds / HS 6404",
    },
  };

  const cfg = copy[tab] || copy.people;
  title.textContent = cfg.title;
  sub.textContent = cfg.sub;
  keyword.placeholder = cfg.placeholder;
  if (tab !== "people") {
    platformGroup.classList.add("hidden");
  }

  if (window.lucide) {
    window.lucide.createIcons();
  }

  form.addEventListener("submit", function (ev) {
    ev.preventDefault();
    const kw = keyword.value.trim();
    if (!kw) return;

    const platforms = Array.prototype.map.call(
      document.querySelectorAll('input[name="platform"]:checked'),
      function (el) { return el.value; }
    );

    status.textContent = "正在调用开源 sidecar…";
    warnings.classList.add("hidden");
    warnings.textContent = "";
    results.innerHTML = "";

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
          return;
        }
        const data = out.j;
        status.textContent = "来源 " + (data.sources || []).join("、") +
          " · " + (data.hits || []).length + " 条 · " + (data.took_ms || 0) + "ms";
        if (data.note) {
          status.textContent += " · " + data.note;
        }
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
      results.innerHTML = '<p class="discover-status">没有命中。确认 sidecar 已启动，或换关键词 / 抖音主页 URL。</p>';
      return;
    }
    results.innerHTML = hits.map(function (h) {
      const initial = (h.name || h.handle || "?").slice(0, 1).toUpperCase();
      const handle = h.handle ? "@" + h.handle : "";
      const home = h.homepage_url || "#";
      const msg = h.message_url || home;
      return (
        '<article class="hit-card">' +
          '<div class="hit-top">' +
            '<span class="hit-avatar">' + escapeHtml(initial) + "</span>" +
            '<div><div class="hit-name">' + escapeHtml(h.name || "") + "</div>" +
            '<div class="hit-handle">' + escapeHtml(handle) + "</div></div>" +
            '<span class="hit-badge">' + escapeHtml(h.platform || "") + "</span>" +
          "</div>" +
          '<div class="hit-snip">' + escapeHtml(h.snippet || h.message_hint || "") + "</div>" +
          '<div class="hit-actions">' +
            '<a class="btn-home" target="_blank" rel="noopener" href="' + escapeAttr(home) + '">打开主页</a>' +
            '<a class="btn-msg" target="_blank" rel="noopener" href="' + escapeAttr(msg) + '" title="' +
              escapeAttr(h.message_hint || "") + '">去私信</a>' +
          "</div>" +
        "</article>"
      );
    }).join("");
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
