(function () {
  const landing = document.getElementById("landing");
  const resultsView = document.getElementById("results-view");
  const form = document.getElementById("discover-form");
  const landingForm = document.getElementById("landing-form");
  const status = document.getElementById("discover-status");
  const warnings = document.getElementById("discover-warnings");
  const results = document.getElementById("discover-results");
  const empty = document.getElementById("discover-empty");
  const foot = document.getElementById("discover-foot");
  const keyword = document.getElementById("keyword");
  const keywordLanding = document.getElementById("keyword-landing");
  const toastEl = document.getElementById("toast");

  let catalog = [];
  let mode = "homepage";
  let channel = "email";
  let lastHits = [];
  let page = 1;
  const PAGE_SIZE = 20;

  const HOME_BULLETS = [
    "不是地图搜店：输入商品或企业关键词",
    "在抖音、TikTok 等社媒找店主 / 真人主页",
    "拿到的是可打开的官方主页，不是邮箱",
    "去私信：在右侧打开主页预览，系统不代发"
  ];
  const MARKET_BULLETS = [
    "营销模式对齐网易外贸通智能引擎：找已公开的联系方式",
    "从公开网页抽取邮箱或 WhatsApp",
    "结果是账号或邮箱、网页标题、来源链接",
    "一键营销在右侧写信，系统不代发"
  ];

  const PLATFORM_ICONS = {
    facebook: { color: "#1877F2", path: "M9.101 23.691v-7.98H6.627v-3.667h2.474v-1.58c0-4.085 1.848-5.978 5.858-5.978.401 0 .955.042 1.468.103a8.68 8.68 0 0 1 1.141.195v3.325a8.623 8.623 0 0 0-.653-.036 26.805 26.805 0 0 0-.733-.009c-.707 0-1.259.096-1.675.309a1.686 1.686 0 0 0-.679.622c-.258.42-.374.995-.374 1.752v1.297h3.919l-.386 2.103-.287 1.564h-3.246v8.245C19.396 23.238 24 18.179 24 12.044c0-6.627-5.373-12-12-12s-12 5.373-12 12c0 5.628 3.874 10.35 9.101 11.647Z" },
    linkedin: { color: "#0A66C2", path: "M20.447 20.452h-3.554v-5.569c0-1.328-.027-3.037-1.852-3.037-1.853 0-2.136 1.445-2.136 2.939v5.667H9.351V9h3.414v1.561h.046c.477-.9 1.637-1.85 3.37-1.85 3.601 0 4.267 2.37 4.267 5.455v6.286zM5.337 7.433c-1.144 0-2.063-.926-2.063-2.065 0-1.138.92-2.063 2.063-2.063 1.14 0 2.064.925 2.064 2.063 0 1.139-.925 2.065-2.064 2.065zm1.782 13.019H3.555V9h3.564v11.452zM22.225 0H1.771C.792 0 0 .774 0 1.729v20.542C0 23.227.792 24 1.771 24h20.451C23.2 24 24 23.227 24 22.271V1.729C24 .774 23.2 0 22.222 0h.003z" },
    instagram: { color: "#E4405F", path: "M12 0C8.74 0 8.333.015 7.053.072 5.775.132 4.905.333 4.14.63c-.789.306-1.459.717-2.126 1.384S.935 3.35.63 4.14C.333 4.905.131 5.775.072 7.053.012 8.333 0 8.74 0 12s.015 3.667.072 4.947c.06 1.277.261 2.148.558 2.913.306.788.717 1.459 1.384 2.126.667.666 1.336 1.079 2.126 1.384.766.296 1.636.499 2.913.558C8.333 23.988 8.74 24 12 24s3.667-.015 4.947-.072c1.277-.06 2.148-.262 2.913-.558.788-.306 1.459-.718 2.126-1.384.666-.667 1.079-1.335 1.384-2.126.296-.765.499-1.636.558-2.913.06-1.28.072-1.687.072-4.947s-.015-3.667-.072-4.947c-.06-1.277-.262-2.149-.558-2.913-.306-.789-.718-1.459-1.384-2.126C21.319 1.347 20.651.935 19.86.63c-.765-.297-1.636-.499-2.913-.558C15.667.012 15.26 0 12 0zm0 2.16c3.203 0 3.585.016 4.85.071 1.17.055 1.805.249 2.227.415.562.217.96.477 1.382.896.419.42.679.819.896 1.381.164.422.36 1.057.413 2.227.057 1.266.07 1.646.07 4.85s-.015 3.585-.074 4.85c-.061 1.17-.256 1.805-.421 2.227-.224.562-.479.96-.899 1.382-.419.419-.824.679-1.38.896-.42.164-1.065.36-2.235.413-1.274.057-1.649.07-4.859.07-3.211 0-3.586-.015-4.859-.074-1.171-.061-1.816-.256-2.236-.421-.569-.224-.96-.479-1.379-.899-.421-.419-.69-.824-.9-1.38-.165-.42-.359-1.065-.42-2.235-.045-1.26-.061-1.649-.061-4.844 0-3.196.016-3.586.061-4.861.061-1.17.255-1.814.42-2.234.21-.57.479-.96.9-1.381.419-.419.81-.689 1.379-.898.42-.166 1.051-.361 2.221-.421 1.275-.045 1.65-.06 4.859-.06l.045.03zm0 3.678c-3.405 0-6.162 2.76-6.162 6.162 0 3.405 2.76 6.162 6.162 6.162 3.405 0 6.162-2.76 6.162-6.162 0-3.405-2.757-6.162-6.162-6.162zM12 16c-2.21 0-4-1.79-4-4s1.79-4 4-4 4 1.79 4 4-1.79 4-4 4zm7.846-10.405c0 .795-.646 1.44-1.44 1.44-.795 0-1.44-.646-1.44-1.44 0-.794.646-1.439 1.44-1.439.793-.001 1.44.645 1.44 1.439z" },
    youtube: { color: "#FF0000", path: "M23.498 6.186a3.016 3.016 0 0 0-2.122-2.136C19.505 3.545 12 3.545 12 3.545s-7.505 0-9.377.505A3.017 3.017 0 0 0 .502 6.186C0 8.07 0 12 0 12s0 3.93.502 5.814a3.016 3.016 0 0 0 2.122 2.136c1.871.505 9.376.505 9.376.505s7.505 0 9.377-.505a3.015 3.015 0 0 0 2.122-2.136C24 15.93 24 12 24 12s0-3.93-.502-5.814zM9.545 15.568V8.432L15.818 12l-6.273 3.568z" },
    tiktok: { color: "#111111", path: "M12.525.02c1.31-.02 2.61-.01 3.91-.02.08 1.53.63 3.09 1.75 4.17 1.12 1.11 2.7 1.62 4.24 1.79v4.03c-1.44-.05-2.89-.35-4.2-.97-.57-.26-1.1-.59-1.62-.93-.01 2.92.01 5.84-.02 8.75-.08 1.4-.54 2.79-1.35 3.94-1.31 1.92-3.58 3.17-5.91 3.21-1.43.08-2.86-.31-4.08-1.03-2.02-1.19-3.44-3.37-3.65-5.71-.02-.5-.03-1-.01-1.49.18-1.9 1.12-3.72 2.58-4.96 1.66-1.44 3.98-2.13 6.15-1.72.02 1.48-.04 2.96-.04 4.44-.99-.32-2.15-.23-3.02.37-.63.41-1.11 1.04-1.36 1.75-.21.51-.15 1.07-.14 1.61.24 1.64 1.82 3.02 3.5 2.87 1.12-.01 2.19-.66 2.77-1.61.19-.33.4-.67.41-1.06.1-1.79.06-3.57.07-5.36.01-4.03-.01-8.05.02-12.07z" },
    x: { color: "#111111", path: "M18.901 1.153h3.68l-8.04 9.19L24 22.846h-7.406l-5.8-7.584-6.638 7.584H.474l8.6-9.83L0 1.154h7.594l5.243 6.932ZM17.61 20.644h2.039L6.486 3.24H4.298Z" },
    pinterest: { color: "#E60023", path: "M12.017 0C5.396 0 .029 5.367.029 11.987c0 5.079 3.158 9.417 7.618 11.162-.105-.949-.199-2.403.041-3.439.219-.937 1.406-5.957 1.406-5.957s-.359-.72-.359-1.781c0-1.663.967-2.911 2.168-2.911 1.024 0 1.518.769 1.518 1.688 0 1.029-.653 2.567-.992 3.992-.285 1.193.6 2.165 1.775 2.165 2.128 0 3.768-2.245 3.768-5.487 0-2.861-2.063-4.869-5.008-4.869-3.41 0-5.409 2.562-5.409 5.199 0 1.033.394 2.143.889 2.741.099.12.112.225.085.345-.09.375-.293 1.199-.334 1.363-.053.225-.172.271-.401.165-1.495-.69-2.433-2.878-2.433-4.646 0-3.776 2.748-7.252 7.92-7.252 4.158 0 7.392 2.967 7.392 6.923 0 4.135-2.607 7.462-6.233 7.462-1.214 0-2.354-.629-2.758-1.329l-.749 2.848c-.269 1.045-1.004 2.352-1.498 3.146 1.123.345 2.306.535 3.55.535 6.607 0 11.985-5.365 11.985-11.987C23.97 5.39 18.592.026 11.985.026L12.017 0z" },
    threads: { color: "#111111", path: "M18.263 11.097c-.03-3.486-1.92-5.586-5.111-5.586-2.13 0-3.922.963-4.863 2.499l2.062 1.438c.535-.843 1.272-1.543 2.628-1.543 1.528 0 2.318.85 2.544 2.431a15 15 0 0 0-2.236-.173c-4.125 0-6.068 1.867-6.068 4.336s1.943 3.99 4.804 3.99c3.139 0 5.013-2.115 5.781-4.735.798.361 1.348 1.204 1.348 2.47 0 3.387-3.907 5.232-7.22 5.232-4.885 0-8.077-3.207-8.077-8.424 0-6.392 4.223-10.487 9.9-10.487 3.808 0 5.69 1.671 6.97 3.914l2.108-1.475C21.44 2.078 18.331 0 13.663 0 6.227 0 1.168 5.277 1.168 12.934c0 7 4.953 11.066 10.856 11.066 4.878 0 9.809-2.846 9.809-7.716 0-2.545-1.46-4.231-3.569-5.187m-6.33 4.855c-1.077 0-2.026-.512-2.026-1.453 0-1.483 1.822-1.934 3.606-1.934.678 0 1.34.045 1.927.173-.422 1.927-1.671 3.215-3.508 3.214Z" },
    douyin: { color: "#111111", path: "M19.589 6.686a4.793 4.793 0 0 1-3.77-4.245V2h-3.425v13.672a2.896 2.896 0 0 1-2.888 2.888 2.896 2.896 0 0 1-2.888-2.888 2.896 2.896 0 0 1 2.888-2.888c.28 0 .556.04.813.118v-3.5a6.373 6.373 0 0 0-.813-.052 6.337 6.337 0 0 0-6.326 6.326 6.337 6.337 0 0 0 6.326 6.326 6.337 6.337 0 0 0 6.326-6.326V8.67a8.216 8.216 0 0 0 4.77 1.526V6.79a4.831 4.831 0 0 1-1.033-.104z" },
    xiaohongshu: { color: "#FF2442", path: "M6 3h12a2 2 0 0 1 2 2v14l-2-1.5L16 19l-2-1.5L12 19l-2-1.5L8 19l-2-1.5V5a2 2 0 0 1 2-2zm2 4h8v1.6H8V7zm0 3h8v1.6H8V10zm0 3h6v1.6H8V13z" },
    kuaishou: { color: "#FF4906", path: "M4 4h7.2v16H4zm9.2 0H20v7.2h-6.8zm0 8.8H20V20h-6.8z" },
    weibo: { color: "#E6162D", path: "M10.2 8.4c4.6.7 8.1 4.4 7.4 8.2-.7 3.8-4.9 5.8-9.5 5.1C3.5 21 0 17.3.7 13.5c.7-3.8 4.9-5.8 9.5-5.1zm.3 3.2c-2.7-.2-5.1 1.4-5.6 3.7-.5 2.3 1.2 4.4 3.9 4.7 2.7.2 5.1-1.4 5.6-3.7.5-2.3-1.2-4.5-3.9-4.7zm8.6-6.5c1.6 1.3 2.4 3.3 2.1 5.3h-2.1c.2-1.2-.2-2.4-1.1-3.2-.9-.8-2.1-1.1-3.3-1V4c1.9.1 3.6.9 4.4 2.1zM16 2.1c2.6.3 4.9 1.7 6.1 3.9 1.2 2.2 1.3 4.8.3 7.1h-2.2c.8-1.7.7-3.7-.2-5.3C19.1 6.2 17.4 5.1 15.5 4.9V2.1z" },
    bilibili: { color: "#00A1D6", path: "M17.8 4.7h.9c1.5 0 2.8.6 3.8 1.6 1 1 1.5 2.2 1.5 3.7v7.4c0 1.5-.5 2.7-1.5 3.7-1 1-2.3 1.6-3.8 1.6H5.3c-1.5 0-2.8-.6-3.8-1.6C.5 20.1 0 18.9 0 17.4V10c0-1.5.5-2.7 1.5-3.7 1-1 2.3-1.6 3.8-1.6h.8L4.1 3.6c-.3-.3-.4-.6-.4-.9 0-.4.1-.7.4-.9.3-.3.6-.4.9-.4s.7.1.9.4L9.7 4.4h4.3l2.8-2.7c.3-.3.6-.4.9-.4s.7.2.9.4c.3.2.4.5.4.9 0 .3-.1.6-.4.9zM8 11.1c.4 0 .7.1.9.4.3.2.4.6.4 1v1.1c0 .4-.1.7-.4 1-.2.2-.5.4-.9.4s-.7-.1-.9-.4c-.3-.3-.4-.6-.4-1v-1.1c0-.4.1-.7.4-1 .2-.3.5-.4.9-.4zm8 0c.4 0 .7.1.9.4.3.2.4.6.4 1v1.1c0 .4-.1.7-.4 1-.2.2-.5.4-.9.4s-.7-.1-.9-.4c-.3-.3-.4-.6-.4-1v-1.1c0-.4.1-.7.4-1 .2-.3.5-.4.9-.4z" },
    telegram: { color: "#26A5E4", path: "M11.944 0A12 12 0 0 0 0 12a12 12 0 0 0 12 12 12 12 0 0 0 12-12A12 12 0 0 0 12 0a12 12 0 0 0-.056 0zm4.962 7.224c.1-.002.321.023.465.14a.506.506 0 0 1 .171.325c.016.093.036.306.02.472-.18 1.898-.962 6.502-1.36 8.627-.168.9-.499 1.201-.82 1.23-.696.065-1.225-.46-1.9-.902-1.056-.693-1.653-1.124-2.678-1.8-1.185-.78-.417-1.21.258-1.91.177-.184 3.247-2.977 3.307-3.23.007-.032.014-.15-.056-.212s-.174-.041-.249-.024c-.106.024-1.793 1.14-5.061 3.345-.48.33-.913.49-1.302.48-.428-.008-1.252-.241-1.865-.44-.752-.245-1.349-.374-1.297-.789.027-.216.325-.437.893-.663 3.498-1.524 5.83-2.529 6.998-3.014 3.332-1.386 4.025-1.627 4.476-1.635z" },
    reddit: { color: "#FF4500", path: "M12 0A12 12 0 0 0 0 12a12 12 0 0 0 12 12 12 12 0 0 0 12-12A12 12 0 0 0 12 0zm5.01 4.744c.688 0 1.25.561 1.25 1.249a1.25 1.25 0 0 1-2.498.056l-2.597-.547-.8 3.747c1.824.07 3.48.632 4.674 1.488.308-.309.73-.491 1.207-.491.968 0 1.754.786 1.754 1.754 0 .716-.435 1.334-1.01 1.614a3.111 3.111 0 0 1 .042.52c0 2.368-2.69 4.201-6.037 4.201-3.346 0-6.037-1.833-6.037-4.201 0-.176.014-.351.042-.524-.575-.28-1.01-.898-1.01-1.614 0-.968.786-1.754 1.754-1.754.463 0 .898.196 1.207.49 1.207-.883 2.878-1.43 4.744-1.487l.885-4.182a.342.342 0 0 1 .14-.197.35.35 0 0 1 .238-.042l2.906.617a1.214 1.214 0 0 1 1.108-.701zM9.25 12C8.561 12 8 12.562 8 13.25c0 .687.561 1.248 1.25 1.248.687 0 1.248-.561 1.248-1.249 0-.688-.561-1.249-1.249-1.249zm5.5 0c-.687 0-1.248.561-1.248 1.25 0 .687.561 1.248 1.249 1.248.688 0 1.249-.561 1.249-1.249 0-.687-.562-1.249-1.25-1.249zm-5.466 3.99a.327.327 0 0 0-.231.094.33.33 0 0 0 0 .463c.841.841 2.484.913 2.961.913.477 0 2.105-.072 2.961-.913a.361.361 0 0 0 .029-.463.33.33 0 0 0-.464 0c-.547.533-1.684.807-2.526.807-.842 0-1.979-.274-2.526-.807a.329.329 0 0 0-.232-.095z" },
    twitch: { color: "#9146FF", path: "M11.571 4.714h1.715v5.143H11.57zm4.715 0H18v5.143h-1.714zM6 0L1.714 4.286v15.428h5.143V24l4.286-4.286h3.428L22.286 12V0zm14.571 11.143l-3.428 3.428h-3.429l-3 3v-3H6.857V1.714h13.714Z" },
  };

  function toast(msg) {
    toastEl.textContent = msg;
    toastEl.classList.remove("hidden");
    clearTimeout(toast.t);
    toast.t = setTimeout(function () { toastEl.classList.add("hidden"); }, 2400);
  }

  function platformSvg(id) {
    const ic = PLATFORM_ICONS[id];
    if (!ic) return "";
    return '<svg class="plat-logo" viewBox="0 0 24 24" aria-hidden="true"><path fill="' + ic.color + '" d="' + ic.path + '"/></svg>';
  }

  function platformLabel(id) {
    for (let i = 0; i < catalog.length; i++) {
      if (catalog[i].id === id) return catalog[i].label;
    }
    return id;
  }

  function bindChips(root) {
    root.querySelectorAll(".plat-chip").forEach(function (btn) {
      btn.addEventListener("click", function () {
        btn.classList.toggle("is-on");
        syncChips(btn);
      });
    });
  }

  function syncChips(src) {
    const id = src.getAttribute("data-platform");
    const on = src.classList.contains("is-on");
    document.querySelectorAll('.plat-chip[data-platform="' + id + '"]').forEach(function (el) {
      el.classList.toggle("is-on", on);
    });
  }

  function renderChips(platforms) {
    const html = platforms.map(function (p) {
      const on = p.default ? " is-on" : "";
      return '<button type="button" class="plat-chip' + on + '" data-platform="' + escapeAttr(p.id) + '">' +
        platformSvg(p.id) + "<span>" + escapeHtml(p.label) + "</span></button>";
    }).join("");
    ["platform-group", "platform-group-landing"].forEach(function (id) {
      const el = document.getElementById(id);
      if (!el) return;
      el.innerHTML = html;
      bindChips(el);
    });
  }

  function selectedPlatforms() {
    const chips = document.querySelectorAll("#platform-group .plat-chip.is-on");
    if (!chips.length) {
      return catalog.filter(function (p) { return p.default; }).map(function (p) { return p.id; });
    }
    return Array.prototype.map.call(chips, function (el) { return el.getAttribute("data-platform"); });
  }

  function selectedChannel() {
    const on = document.querySelector("#channel-group button.is-on") ||
      document.querySelector("#channel-group-landing button.is-on");
    return on ? on.getAttribute("data-channel") : channel;
  }

  function bindMode() {
    document.querySelectorAll("button[data-mode]").forEach(function (btn) {
      btn.addEventListener("click", function () {
        setMode(btn.getAttribute("data-mode"));
      });
    });
  }

  function bindChannel() {
    document.querySelectorAll("button[data-channel]").forEach(function (btn) {
      btn.addEventListener("click", function () {
        setChannel(btn.getAttribute("data-channel"));
      });
    });
  }

  function setMode(next) {
    const prev = mode;
    mode = next === "marketing" ? "marketing" : "homepage";
    document.querySelectorAll("button[data-mode]").forEach(function (el) {
      el.classList.toggle("is-on", el.getAttribute("data-mode") === mode);
    });
    const marketing = mode === "marketing";
    const onLanding = !landing.classList.contains("hidden");
    ["channel-group", "channel-group-landing"].forEach(function (id) {
      const el = document.getElementById(id);
      if (el) el.hidden = !marketing;
    });
    const actions = document.getElementById("market-actions");
    if (actions) actions.hidden = !marketing || onLanding;
    const crumb = document.getElementById("results-crumb");
    if (crumb) crumb.textContent = marketing ? "营销获客" : "社媒主页";
    const bullets = document.getElementById("mode-bullets");
    if (bullets) {
      const items = marketing ? MARKET_BULLETS : HOME_BULLETS;
      bullets.innerHTML = items.map(function (t) {
        return '<li><span class="wmt-check"><svg class="wmt-icon" style="width:10px;height:10px"><use href="#i-tick"/></svg></span>' + t + "</li>";
      }).join("");
    }
    const thead = document.getElementById("discover-thead");
    if (thead) {
      thead.innerHTML = marketing
        ? "<tr><th style=\"width:36px\"></th><th>账号或邮箱</th><th>网页标题</th><th>来源链接</th><th style=\"width:120px\">操作</th></tr>"
        : "<tr><th>名称</th><th style=\"width:130px\">平台</th><th style=\"width:180px\">账号</th><th>主页</th><th style=\"width:160px\">操作</th></tr>";
    }
    if (prev !== mode && !onLanding && keyword.value) {
      doSearch(keyword.value);
      return;
    }
    if (lastHits.length) renderHits(lastHits);
  }

  function setChannel(next) {
    channel = next === "whatsapp" ? "whatsapp" : "email";
    document.querySelectorAll("button[data-channel]").forEach(function (el) {
      el.classList.toggle("is-on", el.getAttribute("data-channel") === channel);
    });
    if (!landing.classList.contains("hidden")) return;
    if (mode === "marketing" && keyword.value) doSearch(keyword.value);
  }

  function loadPlatforms() {
    return fetch("/api/v1/discover/platforms")
      .then(function (r) { return r.json().then(function (j) { return { ok: r.ok, j: j }; }); })
      .then(function (out) {
        if (!out.ok || !out.j.platforms) {
          toast("无法加载已支持平台");
          return;
        }
        catalog = out.j.platforms;
        renderChips(catalog);
      })
      .catch(function () { toast("无法连接搜索引擎"); });
  }

  function setBusy(on) {
    ["landing-btn", "discover-btn"].forEach(function (id) {
      const b = document.getElementById(id);
      if (!b) return;
      b.disabled = on;
      b.textContent = on ? "搜索中" : "搜索";
    });
  }

  function doSearch(kw) {
    kw = (kw || "").trim();
    if (!kw) return;
    keyword.value = kw;
    if (keywordLanding) keywordLanding.value = kw;

    landing.classList.add("hidden");
    resultsView.classList.remove("hidden");
    status.textContent = mode === "marketing" ? "正在检索公开邮箱 / WhatsApp…" : "正在检索公开主页…";
    warnings.classList.add("hidden");
    warnings.textContent = "";
    results.innerHTML = "";
    empty.classList.add("hidden");
    foot.textContent = "";
    page = 1;
    lastHits = [];
    const pager = document.getElementById("pager");
    if (pager) pager.hidden = true;
    setBusy(true);

    fetch("/api/v1/discover/search", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({
        keyword: kw,
        kind: mode === "marketing" ? "marketing" : "people",
        mode: mode,
        channel: mode === "marketing" ? selectedChannel() : "",
        platforms: selectedPlatforms(),
        limit: 0,
      }),
    })
      .then(function (r) { return r.json().then(function (j) { return { ok: r.ok, j: j }; }); })
      .then(function (out) {
        setBusy(false);
        if (!out.ok) {
          status.textContent = out.j.message || "搜索失败";
          empty.classList.remove("hidden");
          empty.textContent = status.textContent;
          return;
        }
        const data = out.j;
        lastHits = data.hits || [];
        if (mode === "marketing") {
          const want = selectedChannel();
          lastHits = lastHits.filter(function (h) { return !want || h.channel === want; });
        }
        page = 1;
        status.textContent = lastHits.length
          ? ("已找到 " + lastHits.length + (mode === "marketing" ? " 条联系方式" : " 条主页"))
          : "";
        const actions = document.getElementById("market-actions");
        if (actions) actions.hidden = mode !== "marketing";
        renderHits(lastHits);
      })
      .catch(function (err) {
        setBusy(false);
        status.textContent = "搜索失败，请稍后重试。";
      });
  }

  landingForm.addEventListener("submit", function (ev) {
    ev.preventDefault();
    doSearch(keywordLanding.value);
  });
  form.addEventListener("submit", function (ev) {
    ev.preventDefault();
    doSearch(keyword.value);
  });

  function pageCount() {
    return Math.max(1, Math.ceil(lastHits.length / PAGE_SIZE));
  }

  function pagedHits() {
    const total = pageCount();
    if (page > total) page = total;
    if (page < 1) page = 1;
    const start = (page - 1) * PAGE_SIZE;
    return lastHits.slice(start, start + PAGE_SIZE);
  }

  function updatePager() {
    const pager = document.getElementById("pager");
    const countEl = document.getElementById("hit-count");
    if (!pager) return;
    if (!lastHits.length) {
      pager.hidden = true;
      return;
    }
    pager.hidden = false;
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

  function renderHits(hits) {
    lastHits = hits || [];
    if (mode === "marketing") {
      renderMarketHits(lastHits);
      return;
    }
    if (!lastHits.length) {
      empty.classList.remove("hidden");
      empty.textContent = "没有命中公开主页。可换关键词或勾选更多平台。";
      foot.textContent = "";
      updatePager();
      results.innerHTML = "";
      return;
    }
    empty.classList.add("hidden");
    const rows = pagedHits();
    results.innerHTML = rows.map(function (h) {
      const plat = (h.platform || "").toLowerCase();
      const handle = h.handle ? "@" + h.handle : "—";
      const home = h.homepage_url || "";
      const msg = h.message_url || home;
      const src = home.replace(/^https?:\/\/(www\.)?/, "");
      return (
        "<tr>" +
          '<td class="hit-title">' + escapeHtml(h.name || handle) + "</td>" +
          '<td><span class="hit-badge">' + platformSvg(plat) + "<span>" + escapeHtml(platformLabel(plat)) + "</span></span></td>" +
          "<td>" + escapeHtml(handle) + "</td>" +
          "<td>" + (home
            ? '<a class="hit-home" target="_blank" rel="noopener" href="' + escapeAttr(home) + '">' +
                platformSvg(plat) + escapeHtml(src) + "</a>"
            : "—") + "</td>" +
          '<td class="row-actions">' +
            '<button type="button" class="linkish" data-open="home" data-url="' + escapeAttr(home) +
              '" data-name="' + escapeAttr(h.name || handle) + '" data-platform="' + escapeAttr(plat) +
              '" data-handle="' + escapeAttr(h.handle || "") + '" data-snippet="' + escapeAttr(h.snippet || "") +
              '">打开主页</button>' +
            '<button type="button" class="linkish btn-msg" data-open="msg" data-url="' + escapeAttr(msg || home) +
              '" data-name="' + escapeAttr(h.name || handle) + '" data-platform="' + escapeAttr(plat) +
              '" data-handle="' + escapeAttr(h.handle || "") + '" data-snippet="' + escapeAttr(h.snippet || h.message_hint || "") +
              '">去私信</button>' +
          "</td>" +
        "</tr>"
      );
    }).join("");
    foot.textContent = "";
    updatePager();
  }

  function renderMarketHits(hits) {
    lastHits = hits || [];
    if (!lastHits.length) {
      empty.classList.remove("hidden");
      empty.textContent = "没有命中公开" + (selectedChannel() === "whatsapp" ? " WhatsApp" : "邮箱") + "。可换关键词或改回私信模式。";
      foot.textContent = "";
      results.innerHTML = "";
      updatePager();
      return;
    }
    empty.classList.add("hidden");
    const rows = pagedHits();
    results.innerHTML = rows.map(function (h, i) {
      const plat = (h.platform || "").toLowerCase();
      const contact = h.contact || h.handle || "";
      const title = h.title || h.name || "";
      const home = h.homepage_url || h.message_url || "";
      const src = home.replace(/^https?:\/\/(www\.)?/, "");
      return (
        "<tr>" +
          '<td><input type="checkbox" class="mkt-pick" data-i="' + i + '" data-channel="' +
            escapeAttr(h.channel || "") + '" data-contact="' + escapeAttr(contact) +
            '" data-msg="' + escapeAttr(h.message_url || "") + '"></td>' +
          '<td class="hit-mail">' + escapeHtml(contact) + "</td>" +
          '<td class="hit-title">' + escapeHtml(title) + "</td>" +
          "<td>" + (home
            ? '<a class="hit-home" target="_blank" rel="noopener" href="' + escapeAttr(home) + '">' +
                platformSvg(plat) + escapeHtml(src || home) + "</a>"
            : "—") + "</td>" +
          '<td class="row-actions">' +
            '<button type="button" class="linkish btn-msg" data-open="' +
              (h.channel === "whatsapp" ? "wa" : "mail") +
              '" data-url="' + escapeAttr(h.message_url || home) +
              '" data-contact="' + escapeAttr(contact) +
              '" data-name="' + escapeAttr(title || contact) +
              '" data-platform="' + escapeAttr(plat) +
              '" data-snippet="' + escapeAttr(h.snippet || "") +
              '">' + (h.channel === "whatsapp" ? "打开 WhatsApp" : "写邮件") + "</button>" +
          "</td>" +
        "</tr>"
      );
    }).join("");
    foot.textContent = "";
    updatePager();
  }

  function pickedRows() {
    return Array.prototype.map.call(document.querySelectorAll(".mkt-pick:checked"), function (el) {
      return {
        channel: el.getAttribute("data-channel"),
        contact: el.getAttribute("data-contact"),
        msg: el.getAttribute("data-msg"),
      };
    });
  }

  document.getElementById("page-prev").addEventListener("click", function () {
    if (page > 1) {
      page -= 1;
      renderHits(lastHits);
    }
  });
  document.getElementById("page-next").addEventListener("click", function () {
    if (page < pageCount()) {
      page += 1;
      renderHits(lastHits);
    }
  });

  document.getElementById("select-all").addEventListener("click", function () {
    const boxes = document.querySelectorAll(".mkt-pick");
    const allOn = Array.prototype.every.call(boxes, function (b) { return b.checked; });
    boxes.forEach(function (b) { b.checked = !allOn; });
  });

  document.getElementById("market-btn").addEventListener("click", function () {
    const picked = pickedRows();
    if (!picked.length) {
      toast("请先勾选要营销的联系方式");
      return;
    }
    const emails = picked.filter(function (p) { return p.channel === "email" && p.contact; });
    const was = picked.filter(function (p) { return p.channel === "whatsapp" && (p.msg || p.contact); });
    if (emails.length) {
      showPreview({
        kind: "mail",
        name: "一键营销",
        contact: emails.map(function (p) { return p.contact; }).join(", "),
        url: "mailto:" + emails.map(function (p) { return p.contact; }).join(","),
        snippet: "已选 " + emails.length + " 个公开邮箱。在右侧写草稿后打开系统邮箱，系统不会代发。",
      });
      return;
    }
    if (was.length) {
      showPreview({
        kind: "wa",
        name: "WhatsApp",
        contact: was[0].contact,
        url: was[0].msg,
        snippet: "在右侧写草稿后打开 WhatsApp 官方窗口，系统不会代发。",
      });
    }
  });

  let previewOfficial = "";

  function showPreview(hit) {
    document.getElementById("preview-empty").classList.add("hidden");
    document.getElementById("preview-card").classList.remove("hidden");
        document.getElementById("preview-name").textContent = hit.name || hit.contact || "主页预览";
    document.getElementById("preview-kicker").textContent =
      hit.kind === "mail" ? "写开发信" : hit.kind === "wa" ? "WhatsApp" : hit.kind === "msg" ? "去私信" : "打开主页";
    document.getElementById("preview-desc").textContent = hit.snippet || "";
    document.getElementById("preview-to").value = hit.contact || (hit.handle ? "@" + hit.handle : "");
    document.getElementById("preview-to-label").textContent =
      hit.kind === "mail" ? "收件人邮箱" : hit.kind === "wa" ? "WhatsApp" : "账号";
    document.getElementById("preview-note").textContent = "正在载入公开页摘要…";
    document.getElementById("preview-image").classList.add("hidden");
    const frame = document.getElementById("preview-frame");
    frame.classList.add("hidden");
    frame.removeAttribute("src");
    previewOfficial = hit.url || "";
    const pageURL = (hit.url || "").indexOf("mailto:") === 0 ? "" : hit.url;
    if (!pageURL) {
      document.getElementById("preview-note").textContent = hit.snippet || "系统不会代发。";
      return;
    }
    fetch("/api/v1/discover/preview?url=" + encodeURIComponent(pageURL))
      .then(function (r) { return r.json().then(function (j) { return { ok: r.ok, j: j }; }); })
      .then(function (out) {
        if (!out.ok) {
          document.getElementById("preview-note").textContent = (out.j && out.j.message) ||
            "官方页无法内嵌。可点「在官方页打开」后手动发送，系统不会代发。";
          return;
        }
        const p = out.j;
        if (p.title && !hit.name) document.getElementById("preview-name").textContent = p.title;
        if (p.description) document.getElementById("preview-desc").textContent = p.description;
        document.getElementById("preview-note").textContent = p.note || "";
        if (p.final_url) previewOfficial = p.final_url;
        const img = document.getElementById("preview-image");
        if (p.image) {
          img.src = p.image;
          img.classList.remove("hidden");
        }
        if (p.embeddable && previewOfficial) {
          frame.src = previewOfficial;
          frame.classList.remove("hidden");
        }
        if (p.contacts && p.contacts.length && !hit.contact) {
          document.getElementById("preview-to").value = p.contacts[0].contact || "";
        }
      })
      .catch(function () {
        document.getElementById("preview-note").textContent =
          "无法载入摘要。可点「在官方页打开」后手动发送，系统不会代发。";
      });
  }

  results.addEventListener("click", function (ev) {
    const btn = ev.target.closest("[data-open]");
    if (!btn) return;
    ev.preventDefault();
    showPreview({
      kind: btn.getAttribute("data-open"),
      url: btn.getAttribute("data-url"),
      name: btn.getAttribute("data-name"),
      contact: btn.getAttribute("data-contact"),
      handle: btn.getAttribute("data-handle"),
      snippet: btn.getAttribute("data-snippet"),
    });
  });

  document.getElementById("preview-close").addEventListener("click", function () {
    document.getElementById("preview-card").classList.add("hidden");
    document.getElementById("preview-empty").classList.remove("hidden");
    document.getElementById("preview-frame").removeAttribute("src");
  });

  document.getElementById("preview-open-official").addEventListener("click", function () {
    if (!previewOfficial) {
      toast("没有可打开的官方页");
      return;
    }
    if (previewOfficial.indexOf("mailto:") === 0) {
      const to = document.getElementById("preview-to").value.trim();
      const draft = document.getElementById("preview-draft").value.trim();
      window.location.href = "mailto:" + encodeURIComponent(to).replace(/%40/g, "@").replace(/%2C/g, ",") +
        (draft ? "?body=" + encodeURIComponent(draft) : "");
      return;
    }
    window.open(previewOfficial, "_blank", "noopener");
  });

  document.getElementById("preview-copy").addEventListener("click", function () {
    const text = document.getElementById("preview-draft").value;
    if (!text) {
      toast("草稿是空的");
      return;
    }
    if (navigator.clipboard && navigator.clipboard.writeText) {
      navigator.clipboard.writeText(text).then(function () { toast("已复制草稿"); });
      return;
    }
    toast("请手动复制草稿");
  });

  function escapeHtml(s) {
    return String(s || "").replace(/[&<>"']/g, function (c) {
      return ({ "&": "&amp;", "<": "&lt;", ">": "&gt;", '"': "&quot;", "'": "&#39;" })[c];
    });
  }
  function escapeAttr(s) { return escapeHtml(s).replace(/`/g, ""); }

  bindMode();
  bindChannel();
  setMode("homepage");
  loadPlatforms();
})();
