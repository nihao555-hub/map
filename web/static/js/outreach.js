/* 开发信中心前端：侧边栏、面板切换，以及三栏客户工作台
   （客户名单 · 沟通历史 · AI 生成内容 + AI 评估）。 */
(function () {
  'use strict';

  var state = {
    contacts: [],
    counts: { all: 0, uncontacted: 0, following: 0, replied: 0 },
    view: 'all',
    page: 1,
    pageSize: 8,
    selectedId: null,
    selected: null,
    loading: false,
    contactsLoaded: false
  };

  /* ---------- 小工具 ---------- */

  function $(id) { return document.getElementById(id); }

  function esc(value) {
    var div = document.createElement('div');
    div.textContent = value == null ? '' : String(value);
    return div.innerHTML;
  }

  function hasTime(iso) {
    if (!iso) return false;
    var d = new Date(iso);
    return !isNaN(d.getTime()) && d.getFullYear() > 2000;
  }

  function fmtTime(iso) {
    if (!hasTime(iso)) return '';
    var d = new Date(iso);
    var pad = function (n) { return (n < 10 ? '0' : '') + n; };
    return (d.getMonth() + 1) + '月' + d.getDate() + '日 ' + pad(d.getHours()) + ':' + pad(d.getMinutes());
  }

  function fmtDate(iso) {
    if (!hasTime(iso)) return '';
    var d = new Date(iso);
    return (d.getMonth() + 1) + '月' + d.getDate() + '日';
  }

  function refreshIcons() { if (window.lucide) lucide.createIcons(); }

  var AVATAR_COLORS = ['#3563e9', '#16a36a', '#d98b18', '#7c4dbd', '#dc4c64', '#0ea5b7', '#e8590c', '#2f6feb'];
  function avatarColor(seed) {
    var s = String(seed || '');
    var h = 0;
    for (var i = 0; i < s.length; i++) { h = (h * 31 + s.charCodeAt(i)) >>> 0; }
    return AVATAR_COLORS[h % AVATAR_COLORS.length];
  }
  function initial(text) {
    var s = String(text || '').trim();
    return s ? s[0].toUpperCase() : '?';
  }

  // 客户列表状态徽标（对应设计稿：已回复/跟进中/未联系 等）
  function badgeFor(c) {
    switch (c.status) {
      case 'replied': return { cls: 'replied', text: '已回复' };
      case 'bounced': return { cls: 'bounced', text: '退信' };
      case 'unsubscribed': return { cls: 'unsubscribed', text: '已退订' };
      case 'completed': return { cls: 'completed', text: '已发完' };
      case 'failed': return { cls: 'failed', text: '发送失败' };
      default:
        return hasTime(c.last_sent_at)
          ? { cls: 'following', text: '跟进中' }
          : { cls: 'uncontacted', text: '未联系' };
    }
  }

  /* ---------- 侧边栏 ---------- */

  var rail = $('orail');

  function setRailPinned(pinned) {
    rail.classList.toggle('pinned', pinned);
    try { localStorage.setItem('outreach-rail-pinned', pinned ? '1' : ''); } catch (e) { /* 忽略 */ }
  }

  $('rail-pin').addEventListener('click', function () {
    setRailPinned(!rail.classList.contains('pinned'));
  });

  try {
    if (localStorage.getItem('outreach-rail-pinned') === '1') setRailPinned(true);
  } catch (e) { /* 忽略 */ }

  /* 下拉/弹出菜单：fixed 定位以逃出收缩态的窄轨与溢出容器 */
  function closeMenus() {
    document.querySelectorAll('.rail-menu.open').forEach(function (m) { m.classList.remove('open'); });
  }

  document.querySelectorAll('[data-menu]').forEach(function (btn) {
    btn.addEventListener('click', function (event) {
      event.stopPropagation();
      var menu = $(btn.getAttribute('data-menu'));
      if (!menu) return;
      var willOpen = !menu.classList.contains('open');
      closeMenus();
      if (!willOpen) return;

      var rect = btn.getBoundingClientRect();
      menu.style.left = Math.round(Math.min(rect.left, window.innerWidth - 230)) + 'px';
      if (btn.closest('.rail-bottom')) {
        menu.style.top = 'auto';
        menu.style.bottom = (window.innerHeight - Math.round(rect.top) + 6) + 'px';
      } else {
        menu.style.bottom = 'auto';
        menu.style.top = (Math.round(rect.bottom) + 6) + 'px';
      }
      menu.classList.add('open');
    });
  });

  document.addEventListener('click', function (event) {
    if (!event.target.closest('.rail-menu') && !event.target.closest('[data-menu]')) closeMenus();
  });

  /* ---------- 面板切换 ---------- */

  var PANELS = ['overview', 'workspace', 'campaigns', 'settings'];
  var PANEL_TITLES = {
    overview: '数据分析',
    workspace: '开发信',
    campaigns: '模板 / 活动',
    settings: '设置'
  };

  function showPanel(name) {
    if (!PANEL_TITLES[name]) name = 'workspace';
    PANELS.forEach(function (p) {
      var panel = $('panel-' + p);
      if (panel) panel.classList.toggle('active', p === name);
    });
    document.querySelectorAll('[data-panel-btn]').forEach(function (btn) {
      btn.classList.toggle('active', btn.getAttribute('data-panel-btn') === name);
    });
    $('panel-title').textContent = PANEL_TITLES[name];
    document.body.classList.toggle('ws-full', name === 'workspace');
    var url = new URL(window.location.href);
    url.searchParams.set('panel', name);
    history.replaceState(null, '', url.toString());

    if (name === 'workspace' && !state.contactsLoaded) {
      state.contactsLoaded = true;
      loadContacts();
    }
  }

  document.querySelectorAll('[data-panel-btn]').forEach(function (btn) {
    btn.addEventListener('click', function () {
      closeMenus();
      showPanel(btn.getAttribute('data-panel-btn'));
    });
  });

  /* ---------- 客户名单 ---------- */

  function custRow(c) {
    var b = badgeFor(c);
    var name = c.name || c.email;
    var company = c.category || (c.website || '').replace(/^https?:\/\/(www\.)?/, '') || c.city || '';
    return '<button type="button" class="cust-row' + (state.selectedId === c.id ? ' selected' : '') + '" data-contact="' + c.id + '">' +
      '<span class="cust-avatar" style="background:' + avatarColor(name) + '">' + esc(initial(name)) + '</span>' +
      '<span class="cust-main">' +
        '<span class="cust-name">' + esc(name) + '</span>' +
        '<span class="cust-company">' + esc(company) + '</span>' +
      '</span>' +
      '<span class="cust-meta">' +
        '<span class="cust-badge ' + b.cls + '">' + b.text + '</span>' +
        '<span class="cust-date">' + esc(fmtDate(c.last_activity)) + '</span>' +
      '</span>' +
      '</button>';
  }

  function renderCounts() {
    $('cust-total').textContent = state.counts.all;
    $('cnt-all').textContent = state.counts.all;
    $('cnt-uncontacted').textContent = state.counts.uncontacted;
    $('cnt-following').textContent = state.counts.following;
    $('cnt-replied').textContent = state.counts.replied;
  }

  function renderPager(pageCount) {
    var pager = $('cust-pager');
    if (pageCount <= 1) { pager.innerHTML = ''; return; }
    var html = '<button data-page="prev"' + (state.page <= 1 ? ' disabled' : '') + '>‹</button>';
    for (var p = 1; p <= pageCount; p++) {
      html += '<button data-page="' + p + '"' + (p === state.page ? ' class="active"' : '') + '>' + p + '</button>';
    }
    html += '<button data-page="next"' + (state.page >= pageCount ? ' disabled' : '') + '>›</button>';
    pager.innerHTML = html;
    pager.querySelectorAll('button[data-page]').forEach(function (btn) {
      btn.addEventListener('click', function () {
        var v = btn.getAttribute('data-page');
        if (v === 'prev') state.page = Math.max(1, state.page - 1);
        else if (v === 'next') state.page = Math.min(pageCount, state.page + 1);
        else state.page = parseInt(v, 10);
        renderContacts();
      });
    });
  }

  function renderContacts() {
    var wrap = $('wk-contacts');
    if (!state.contacts.length) {
      wrap.innerHTML = '<div class="wk-hint">没有匹配的客户。<br>去“模板 / 活动”从地图任务批量群发即可导入客户。</div>';
      $('cust-pager').innerHTML = '';
      return;
    }
    var pageCount = Math.ceil(state.contacts.length / state.pageSize);
    if (state.page > pageCount) state.page = 1;
    var start = (state.page - 1) * state.pageSize;
    var pageItems = state.contacts.slice(start, start + state.pageSize);
    wrap.innerHTML = pageItems.map(custRow).join('');
    wrap.querySelectorAll('[data-contact]').forEach(function (row) {
      row.addEventListener('click', function () {
        selectContact(parseInt(row.getAttribute('data-contact'), 10));
      });
    });
    renderPager(pageCount);
  }

  function loadContacts() {
    var params = new URLSearchParams();
    var campaign = $('wk-campaign').value;
    var q = $('wk-search').value.trim();
    if (state.view && state.view !== 'all') params.set('view', state.view);
    if (campaign) params.set('campaign', campaign);
    if (q) params.set('q', q);

    fetch('/api/v1/outreach/contacts?' + params.toString())
      .then(function (r) { return r.json(); })
      .then(function (data) {
        state.contacts = (data && data.contacts) || [];
        state.counts = (data && data.counts) || state.counts;
        state.page = 1;
        renderCounts();
        renderContacts();
      })
      .catch(function () {
        $('wk-contacts').innerHTML = '<div class="wk-hint">客户加载失败，请刷新重试。</div>';
      });
  }

  var searchTimer = null;
  $('wk-search').addEventListener('input', function () {
    clearTimeout(searchTimer);
    searchTimer = setTimeout(loadContacts, 250);
  });
  $('wk-campaign').addEventListener('change', function () { closeMenus(); loadContacts(); });

  document.querySelectorAll('.cust-tab').forEach(function (tab) {
    tab.addEventListener('click', function () {
      document.querySelectorAll('.cust-tab').forEach(function (t) { t.classList.remove('active'); });
      tab.classList.add('active');
      state.view = tab.getAttribute('data-view');
      loadContacts();
    });
  });

  /* ---------- 会话（沟通历史 / 客户详情 / 活动记录） ---------- */

  function factItem(icon, label, value) {
    if (!value) return '';
    return '<div class="fact"><i data-lucide="' + icon + '"></i><div><small>' + esc(label) + '</small><span>' + esc(value) + '</span></div></div>';
  }

  var STATUS_TEXT = {
    active: '进行中', replied: '已回复', bounced: '退信',
    unsubscribed: '已退订', completed: '已发完', failed: '发送失败'
  };

  function timelineItem(m, index) {
    var out = m.direction === 'out';
    var who, dot, title, badge = '';
    if (out) {
      dot = 'send';
      if (m.step != null && m.step >= 0) { who = 'AI 发送'; title = '开发信 #' + (m.step + 1); }
      else { who = '我方回信'; title = m.subject || '回信'; }
      badge = '<span class="tl-sent">已送达</span>';
    } else {
      dot = m.kind === 'bounce' || m.kind === 'unsubscribe' ? 'alert-triangle' : 'reply';
      who = m.kind === 'bounce' ? '退信通知' : (m.kind === 'unsubscribe' ? '退订' : '客户回复');
      title = m.subject || '(无主题)';
    }
    var body = m.body || '(无正文)';
    var snippet = body.length > 140 ? body.slice(0, 140) + '…' : body;
    return '<div class="tl-item ' + (out ? 'out' : 'in') + '">' +
      '<div class="tl-meta"><span class="tl-dot"><i data-lucide="' + dot + '"></i></span>' +
        '<b>' + esc(who) + '</b> · ' + esc(fmtTime(m.created_at)) + '</div>' +
      '<div class="tl-card">' +
        '<div class="tl-card-head"><span class="tl-subject">' + esc(title) + '</span>' + badge + '</div>' +
        '<div class="tl-snippet">' + esc(snippet) + '</div>' +
        '<button type="button" class="tl-toggle" data-tl="' + index + '">查看详情 <i data-lucide="chevron-down"></i></button>' +
        '<div class="tl-full" data-tlfull="' + index + '"><pre>' + esc(body) + '</pre></div>' +
      '</div></div>';
  }

  function renderThread(view) {
    state.selected = view;
    $('wk-empty').classList.add('hidden');
    $('wk-thread').classList.remove('hidden');

    var c = view.contact;
    var name = c.name || c.email;
    var av = $('th-avatar');
    av.textContent = initial(name);
    av.style.background = avatarColor(name);
    $('th-name').textContent = name;

    var sub = [];
    if (c.category) sub.push(c.category);
    if (c.city) sub.push(c.city);
    if (!sub.length && c.email) sub.push(c.email);
    $('th-sub').textContent = sub.join(' · ');

    // 沟通历史时间线
    var timeline = $('th-timeline');
    if (view.messages && view.messages.length) {
      timeline.innerHTML = view.messages.map(timelineItem).join('');
      timeline.querySelectorAll('.tl-toggle').forEach(function (btn) {
        btn.addEventListener('click', function () {
          var full = timeline.querySelector('[data-tlfull="' + btn.getAttribute('data-tl') + '"]');
          if (full) full.classList.toggle('open');
        });
      });
      $('th-end').style.display = '';
    } else {
      timeline.innerHTML = '<div class="wk-hint">还没有往来邮件。点右上角“新建开发信”开始，或启用活动后自动在客户当地上午发送。</div>';
      $('th-end').style.display = 'none';
    }

    // 客户详情
    var nextInfo = '';
    if (c.status === 'active' && hasTime(c.next_send_at)) {
      nextInfo = '第 ' + ((c.next_step || 0) + 1) + '/' + view.sequence_steps + ' 封 · ' + fmtTime(c.next_send_at);
    }
    $('th-facts').innerHTML =
      factItem('mail', '邮箱', c.email) +
      factItem('briefcase', '行业', c.category) +
      factItem('map-pin', '城市 / 地址', c.city || c.address) +
      factItem('globe', '官网', c.website) +
      factItem('phone', '电话', c.phone) +
      factItem('star', '评分', c.rating ? c.rating + '（' + (c.review_count || 0) + ' 条评价）' : '') +
      factItem('clock', '当地时区', c.timezone) +
      factItem('flag', '所属活动', view.campaign_name) +
      factItem('git-branch', '当前状态', STATUS_TEXT[c.status] || c.status) +
      factItem('calendar-clock', '下一封', nextInfo);

    // 活动记录（由消息与状态派生）
    var activity = $('th-activity');
    if (view.messages && view.messages.length) {
      var rows = view.messages.map(function (m) {
        var label = m.direction === 'out'
          ? (m.step != null && m.step >= 0 ? '发出开发信 #' + (m.step + 1) : '发出回信')
          : (m.kind === 'bounce' ? '收到退信' : (m.kind === 'unsubscribe' ? '客户退订' : '收到客户回复'));
        return '<div class="activity-row"><time>' + esc(fmtTime(m.created_at)) + '</time><span>' + esc(label) + '：' + esc(m.subject || '') + '</span></div>';
      });
      activity.innerHTML = rows.join('');
    } else {
      activity.innerHTML = '<div class="wk-hint">暂无活动记录。</div>';
    }

    // 撰写框可用性
    var compose = $('th-compose');
    if (view.can_reply) { compose.classList.remove('disabled'); } else { compose.classList.add('hidden'); }

    // 会话标签回到“沟通历史”
    setConvTab('history');
    refreshIcons();

    renderAIPanel(view);
  }

  function setConvTab(tab) {
    document.querySelectorAll('.conv-tab').forEach(function (t) {
      t.classList.toggle('active', t.getAttribute('data-conv-tab') === tab);
    });
    document.querySelectorAll('.conv-tabpane').forEach(function (p) {
      p.classList.toggle('active', p.getAttribute('data-conv-pane') === tab);
    });
  }

  document.querySelectorAll('[data-conv-tab]').forEach(function (t) {
    t.addEventListener('click', function () { closeMenus(); setConvTab(t.getAttribute('data-conv-tab')); });
  });

  /* ---------- AI 面板：生成内容 + 评估 ---------- */

  function renderAIPanel(view) {
    $('ai-empty').classList.add('hidden');
    $('ai-content').classList.remove('hidden');

    var draft = view.latest_draft;
    if (draft) {
      $('ai-subject').textContent = '主题：' + (draft.subject || '');
      $('ai-mailbody').textContent = draft.body || '';
    } else {
      $('ai-subject').textContent = '';
      $('ai-mailbody').textContent = '该客户还没有已发送的开发信。启用活动或点“新建开发信”后，这里会显示邮件内容。';
    }

    var hint = $('ai-eval-hint');
    var body = $('ai-eval-body');
    if (!draft) {
      body.innerHTML = '<div class="ai-eval-hint">发送首封开发信后，可在此查看 AI 质量评分。</div>';
      return;
    }
    if (!view.ai_configured) {
      body.innerHTML = '<div class="ai-eval-hint">配置 AI（设置 → 邮箱与 AI）后，可查看 5 项质量评分与改进建议。</div>';
      return;
    }
    body.innerHTML = '<div class="ai-eval-hint">正在评估邮件质量…</div>';

    var reqId = view.contact.id;
    fetch('/api/v1/outreach/contacts/' + reqId + '/evaluation', { method: 'POST' })
      .then(function (r) { return r.json().then(function (d) { return { ok: r.ok, data: d }; }); })
      .then(function (res) {
        if (state.selectedId !== reqId) return; // 用户已切换客户
        if (res.ok) renderEvaluation(res.data);
        else body.innerHTML = '<div class="ai-eval-hint">评估失败：' + esc(res.data.message || '请稍后重试') + '</div>';
      })
      .catch(function () {
        if (state.selectedId === reqId) body.innerHTML = '<div class="ai-eval-hint">评估失败，请稍后重试。</div>';
      });
    void hint;
  }

  var EVAL_METRICS = [
    ['subject_appeal', '主题吸引力'],
    ['relevance', '内容相关性'],
    ['personalization', '个性化程度'],
    ['call_to_action', '行动号召'],
    ['readability', '整体可读性']
  ];

  function renderEvaluation(e) {
    var bars = EVAL_METRICS.map(function (m) {
      var v = e[m[0]] || 0;
      return '<div class="score-bar"><span>' + m[1] + '</span>' +
        '<div class="score-track"><i style="width:' + v + '%"></i></div>' +
        '<span class="score-val">' + v + '/100</span></div>';
    }).join('');

    $('ai-eval-body').innerHTML =
      '<div class="ai-score">' +
        '<div class="score-ring" style="--pct:' + (e.overall || 0) + '"><b>' + (e.overall || 0) + '</b><small>分</small></div>' +
        '<div class="score-info"><b>' + esc(e.grade || '') + '</b><small><i data-lucide="info"></i>AI 综合评分</small></div>' +
      '</div>' +
      '<div class="score-bars">' + bars + '</div>' +
      (e.suggestion ? '<div class="ai-suggest"><i data-lucide="sparkles"></i><div><b>AI 建议</b><p>' + esc(e.suggestion) + '</p></div></div>' : '');
    refreshIcons();
  }

  function selectContact(id) {
    state.selectedId = id;
    // 高亮列表项
    document.querySelectorAll('.cust-row').forEach(function (row) {
      row.classList.toggle('selected', parseInt(row.getAttribute('data-contact'), 10) === id);
    });
    fetch('/api/v1/outreach/contacts/' + id)
      .then(function (r) { if (!r.ok) throw new Error('load failed'); return r.json(); })
      .then(renderThread)
      .catch(function () {
        $('wk-empty').classList.remove('hidden');
        $('wk-thread').classList.add('hidden');
      });
  }

  /* ---------- 撰写 / 回信 / AI 草稿 ---------- */

  function openCompose(title) {
    var box = $('th-compose');
    box.classList.remove('hidden');
    $('compose-title').textContent = title || '新建开发信';
    $('reply-body').focus();
    box.scrollIntoView({ behavior: 'smooth', block: 'nearest' });
  }

  $('btn-new-mail').addEventListener('click', function () { openCompose('新建开发信'); });
  $('btn-compose-cancel').addEventListener('click', function () {
    $('th-compose').classList.add('hidden');
    $('reply-body').value = '';
  });

  function runSuggest() {
    if (!state.selectedId || state.loading) return;
    openCompose('回复客户');
    var btn = $('btn-suggest');
    state.loading = true;
    btn.disabled = true;
    btn.innerHTML = '<i data-lucide="loader-2" class="spin"></i>AI 起草中…';
    refreshIcons();
    fetch('/api/v1/outreach/contacts/' + state.selectedId + '/suggest', { method: 'POST' })
      .then(function (r) { return r.json().then(function (d) { return { ok: r.ok, data: d }; }); })
      .then(function (res) {
        if (res.ok && res.data.suggestion) $('reply-body').value = res.data.suggestion;
        else alert(res.data.message || 'AI 草稿生成失败，请检查 AI 设置');
      })
      .catch(function () { alert('AI 草稿生成失败，请稍后重试'); })
      .finally(function () {
        state.loading = false;
        btn.disabled = false;
        btn.innerHTML = '<i data-lucide="sparkles"></i>AI 生成草稿';
        refreshIcons();
      });
  }

  $('btn-suggest').addEventListener('click', runSuggest);
  $('act-suggest').addEventListener('click', function () { closeMenus(); runSuggest(); });

  $('act-copy-email').addEventListener('click', function () {
    closeMenus();
    if (state.selected && state.selected.contact) copyText(state.selected.contact.email);
  });

  $('btn-send-reply').addEventListener('click', function () {
    if (!state.selectedId || state.loading) return;
    var body = $('reply-body').value.trim();
    if (!body) { alert('内容不能为空'); return; }
    if (!confirm('确认发送吗？发送后无法撤回。')) return;
    var btn = this;
    state.loading = true;
    btn.disabled = true;
    fetch('/api/v1/outreach/reply', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ contact_id: state.selectedId, body: body })
    })
      .then(function (r) { return r.json().then(function (d) { return { ok: r.ok, data: d }; }); })
      .then(function (res) {
        if (res.ok) {
          $('reply-body').value = '';
          $('th-compose').classList.add('hidden');
          selectContact(state.selectedId);
          loadContacts();
        } else { alert(res.data.message || '发送失败'); }
      })
      .catch(function () { alert('发送失败，请稍后重试'); })
      .finally(function () { state.loading = false; btn.disabled = false; });
  });

  /* 复制 AI 生成内容 */
  function copyText(text) {
    if (!text) return;
    if (navigator.clipboard && navigator.clipboard.writeText) {
      navigator.clipboard.writeText(text).then(function () { flashCopied(); }, function () { fallbackCopy(text); });
    } else { fallbackCopy(text); }
  }
  function fallbackCopy(text) {
    var ta = document.createElement('textarea');
    ta.value = text; document.body.appendChild(ta); ta.select();
    try { document.execCommand('copy'); flashCopied(); } catch (e) { /* 忽略 */ }
    document.body.removeChild(ta);
  }
  function flashCopied() {
    var btn = $('btn-copy');
    var old = btn.innerHTML;
    btn.innerHTML = '<i data-lucide="check"></i>已复制';
    refreshIcons();
    setTimeout(function () { btn.innerHTML = old; refreshIcons(); }, 1400);
  }
  $('btn-copy').addEventListener('click', function () { copyText($('ai-mailbody').textContent); });

  /* ---------- 服务商预设（设置面板） ---------- */

  window.applyProvider = function (select) {
    var option = select.options[select.selectedIndex];
    if (!option || !option.dataset.smtpHost) return;
    $('smtp-host').value = option.dataset.smtpHost;
    $('smtp-port').value = option.dataset.smtpPort;
    $('smtp-tls').value = option.dataset.smtpTls;
    $('imap-host').value = option.dataset.imapHost;
    $('imap-port').value = option.dataset.imapPort;
  };

  /* ---------- 批量群发：全选任务开关 ---------- */

  var allJobs = $('all-jobs');
  if (allJobs) {
    allJobs.addEventListener('change', function () {
      var list = $('jobs-list');
      if (list) list.classList.toggle('disabled', allJobs.checked);
    });
  }

  /* ---------- 初始化 ---------- */

  var urlParams = new URLSearchParams(window.location.search);
  var presetCampaign = urlParams.get('campaign');
  if (presetCampaign && $('wk-campaign')) $('wk-campaign').value = presetCampaign;

  var initialPanel = presetCampaign ? 'workspace' : (document.body.getAttribute('data-panel') || 'workspace');
  showPanel(initialPanel);

  refreshIcons();
})();
