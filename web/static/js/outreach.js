/* 开发信中心前端：侧边栏、面板切换、客户工作台（联系人列表 + 互动历史 + 意向 + AI 回信） */
(function () {
  'use strict';

  var state = {
    contacts: [],
    selectedId: null,
    loading: false
  };

  /* ---------- 小工具 ---------- */

  function $(id) { return document.getElementById(id); }

  function esc(value) {
    var div = document.createElement('div');
    div.textContent = value == null ? '' : String(value);
    return div.innerHTML;
  }

  function fmtTime(iso) {
    if (!iso) return '';
    var d = new Date(iso);
    if (isNaN(d.getTime()) || d.getFullYear() < 2000) return '';
    var pad = function (n) { return (n < 10 ? '0' : '') + n; };
    return (d.getMonth() + 1) + '月' + d.getDate() + '日 ' + pad(d.getHours()) + ':' + pad(d.getMinutes());
  }

  function refreshIcons() {
    if (window.lucide) lucide.createIcons();
  }

  var STATUS_TEXT = {
    active: '进行中',
    replied: '已回复',
    bounced: '退信',
    unsubscribed: '已退订',
    completed: '已发完',
    failed: '发送失败'
  };

  function intentClass(score) {
    if (score >= 80) return 'intent-high';
    if (score >= 55) return 'intent-mid';
    if (score >= 25) return 'intent-low';
    return 'intent-none';
  }

  /* ---------- 侧边栏 ---------- */

  var rail = $('orail');

  function setRailPinned(pinned) {
    rail.classList.toggle('pinned', pinned);
    rail.classList.toggle('collapsed', !pinned);
    try { localStorage.setItem('outreach-rail-pinned', pinned ? '1' : ''); } catch (e) { /* 忽略 */ }
  }

  $('rail-pin').addEventListener('click', function () {
    setRailPinned(!rail.classList.contains('pinned'));
  });

  try {
    if (localStorage.getItem('outreach-rail-pinned') === '1') setRailPinned(true);
  } catch (e) { /* 忽略 */ }

  /* ---------- 面板切换 ---------- */

  var PANEL_TITLES = {
    workspace: '客户工作台',
    campaigns: '开发信活动',
    settings: '邮箱与 AI 设置'
  };

  function showPanel(name) {
    if (!PANEL_TITLES[name]) name = 'workspace';
    ['workspace', 'campaigns', 'settings'].forEach(function (p) {
      var panel = $('panel-' + p);
      if (panel) panel.classList.toggle('active', p === name);
    });
    document.querySelectorAll('[data-panel-btn]').forEach(function (btn) {
      btn.classList.toggle('active', btn.getAttribute('data-panel-btn') === name);
    });
    $('panel-title').textContent = PANEL_TITLES[name];
    var url = new URL(window.location.href);
    url.searchParams.set('panel', name);
    history.replaceState(null, '', url.toString());
  }

  document.querySelectorAll('[data-panel-btn]').forEach(function (btn) {
    btn.addEventListener('click', function () { showPanel(btn.getAttribute('data-panel-btn')); });
  });

  /* ---------- 联系人列表 ---------- */

  function contactRow(c) {
    var intent = c.intent || {};
    var snippet = c.last_snippet || '尚无往来邮件';
    var name = c.name || c.email;
    return '<button type="button" class="contact-row' + (state.selectedId === c.id ? ' selected' : '') + '" data-contact="' + c.id + '">' +
      '<div class="contact-top">' +
      '<b>' + esc(name) + '</b>' +
      '<span class="intent-badge ' + intentClass(intent.score || 0) + '">' + esc(intent.label || '待观察') + ' ' + (intent.score != null ? intent.score : '') + '</span>' +
      '</div>' +
      '<div class="contact-mid">' + esc(c.email) + '</div>' +
      '<div class="contact-bottom">' +
      '<span class="contact-status ' + esc(c.status) + '">' + esc(STATUS_TEXT[c.status] || c.status) + '</span>' +
      '<span class="contact-snippet">' + esc(snippet) + '</span>' +
      '<time>' + esc(fmtTime(c.last_activity)) + '</time>' +
      '</div>' +
      '</button>';
  }

  function renderContacts() {
    var wrap = $('wk-contacts');
    if (!state.contacts.length) {
      wrap.innerHTML = '<div class="wk-hint">没有匹配的联系人。<br>先在“开发信活动”面板从地图任务导入客户。</div>';
      return;
    }
    wrap.innerHTML = state.contacts.map(contactRow).join('');
    wrap.querySelectorAll('[data-contact]').forEach(function (row) {
      row.addEventListener('click', function () {
        selectContact(parseInt(row.getAttribute('data-contact'), 10));
      });
    });
  }

  function loadContacts() {
    var params = new URLSearchParams();
    var campaign = $('wk-campaign').value;
    var status = $('wk-status').value;
    var q = $('wk-search').value.trim();
    if (campaign) params.set('campaign', campaign);
    if (status) params.set('status', status);
    if (q) params.set('q', q);

    fetch('/api/v1/outreach/contacts?' + params.toString())
      .then(function (r) { return r.json(); })
      .then(function (list) {
        state.contacts = Array.isArray(list) ? list : [];
        renderContacts();
      })
      .catch(function () {
        $('wk-contacts').innerHTML = '<div class="wk-hint">联系人加载失败，请刷新重试。</div>';
      });
  }

  var searchTimer = null;
  $('wk-search').addEventListener('input', function () {
    clearTimeout(searchTimer);
    searchTimer = setTimeout(loadContacts, 250);
  });
  $('wk-campaign').addEventListener('change', loadContacts);
  $('wk-status').addEventListener('change', loadContacts);

  /* ---------- 会话详情 ---------- */

  function factItem(icon, label, value) {
    if (!value) return '';
    return '<div class="fact"><i data-lucide="' + icon + '"></i><div><small>' + esc(label) + '</small><span>' + esc(value) + '</span></div></div>';
  }

  var KIND_TEXT = { reply: '客户回复', bounce: '退信', unsubscribe: '退订', manual_reply: '人工回信' };

  function messageBubble(m) {
    var out = m.direction === 'out';
    var kind = KIND_TEXT[m.kind] || (out ? (m.step >= 0 ? '第 ' + (m.step + 1) + ' 封开发信' : '发出') : '收到');
    return '<div class="bubble-row ' + (out ? 'out' : 'in') + '">' +
      '<div class="bubble ' + esc(m.kind || '') + '">' +
      '<div class="bubble-meta">' +
      '<span class="bubble-kind">' + esc(kind) + '</span>' +
      '<span class="bubble-subject">' + esc(m.subject || '') + '</span>' +
      '<time>' + esc(fmtTime(m.created_at)) + '</time>' +
      '</div>' +
      '<pre>' + esc(m.body || '(无正文)') + '</pre>' +
      '</div></div>';
  }

  function renderThread(view) {
    $('wk-empty').classList.add('hidden');
    $('wk-thread').classList.remove('hidden');

    var c = view.contact;
    $('th-name').textContent = c.name || c.email;
    $('th-email').textContent = c.email;
    $('th-campaign').textContent = view.campaign_name ? '活动：' + view.campaign_name : '';

    var statusEl = $('th-status');
    statusEl.textContent = STATUS_TEXT[c.status] || c.status;
    statusEl.className = 'status-pill ' + c.status;

    var intent = view.intent || {};
    $('th-intent-score').textContent = intent.score != null ? intent.score : '–';
    $('th-intent-label').textContent = intent.label || '待观察';
    $('th-intent-reason').textContent = intent.reason || '';
    $('th-intent-ring').className = 'intent-ring ' + intentClass(intent.score || 0);

    var nextInfo = '';
    if (c.status === 'active') {
      nextInfo = '第 ' + (c.next_step + 1) + '/' + view.sequence_steps + ' 封 · ' + fmtTime(c.next_send_at);
    }
    $('th-facts').innerHTML =
      factItem('briefcase', '行业', c.category) +
      factItem('map-pin', '城市', c.city || c.address) +
      factItem('globe', '官网', c.website) +
      factItem('phone', '电话', c.phone) +
      factItem('star', '评分', c.rating ? c.rating + '（' + c.review_count + ' 条评价）' : '') +
      factItem('clock', '当地时区', c.timezone) +
      factItem('calendar-clock', '下一封', nextInfo);

    var msgWrap = $('th-messages');
    if (view.messages.length) {
      msgWrap.innerHTML = view.messages.map(messageBubble).join('');
    } else {
      msgWrap.innerHTML = '<div class="wk-hint">还没有往来邮件。活动启用后会在客户当地时间窗口自动发出第一封。</div>';
    }
    msgWrap.scrollTop = msgWrap.scrollHeight;

    $('th-replybox').classList.toggle('hidden', !view.can_reply);
    refreshIcons();
  }

  function selectContact(id) {
    state.selectedId = id;
    renderContacts();
    fetch('/api/v1/outreach/contacts/' + id)
      .then(function (r) {
        if (!r.ok) throw new Error('load failed');
        return r.json();
      })
      .then(renderThread)
      .catch(function () {
        $('wk-empty').classList.remove('hidden');
        $('wk-thread').classList.add('hidden');
      });
  }

  /* ---------- AI 建议与发送回信 ---------- */

  $('btn-suggest').addEventListener('click', function () {
    if (!state.selectedId || state.loading) return;
    var btn = this;
    state.loading = true;
    btn.disabled = true;
    btn.innerHTML = '<i data-lucide="loader-2" class="spin"></i>AI 正在按外贸专家口吻起草…';
    refreshIcons();

    fetch('/api/v1/outreach/contacts/' + state.selectedId + '/suggest', { method: 'POST' })
      .then(function (r) { return r.json().then(function (data) { return { ok: r.ok, data: data }; }); })
      .then(function (res) {
        if (res.ok && res.data.suggestion) {
          $('reply-body').value = res.data.suggestion;
        } else {
          alert(res.data.message || 'AI 草稿生成失败，请检查 AI 设置');
        }
      })
      .catch(function () { alert('AI 草稿生成失败，请稍后重试'); })
      .finally(function () {
        state.loading = false;
        btn.disabled = false;
        btn.innerHTML = '<i data-lucide="sparkles"></i>AI 生成回信草稿';
        refreshIcons();
      });
  });

  $('btn-send-reply').addEventListener('click', function () {
    if (!state.selectedId || state.loading) return;
    var body = $('reply-body').value.trim();
    if (!body) { alert('回信内容不能为空'); return; }
    if (!confirm('确认发送这封回信吗？发送后无法撤回。')) return;

    var btn = this;
    state.loading = true;
    btn.disabled = true;

    fetch('/api/v1/outreach/reply', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ contact_id: state.selectedId, body: body })
    })
      .then(function (r) { return r.json().then(function (data) { return { ok: r.ok, data: data }; }); })
      .then(function (res) {
        if (res.ok) {
          $('reply-body').value = '';
          selectContact(state.selectedId);
          loadContacts();
        } else {
          alert(res.data.message || '发送失败');
        }
      })
      .catch(function () { alert('发送失败，请稍后重试'); })
      .finally(function () {
        state.loading = false;
        btn.disabled = false;
      });
  });

  /* ---------- 服务商预设 ---------- */

  window.applyProvider = function (select) {
    var option = select.options[select.selectedIndex];
    if (!option || !option.dataset.smtpHost) return;
    $('smtp-host').value = option.dataset.smtpHost;
    $('smtp-port').value = option.dataset.smtpPort;
    $('smtp-tls').value = option.dataset.smtpTls;
    $('imap-host').value = option.dataset.imapHost;
    $('imap-port').value = option.dataset.imapPort;
  };

  /* ---------- 初始化 ---------- */

  var initialPanel = document.body.getAttribute('data-panel') || 'workspace';
  showPanel(initialPanel);

  var urlParams = new URLSearchParams(window.location.search);
  var presetCampaign = urlParams.get('campaign');
  if (presetCampaign) $('wk-campaign').value = presetCampaign;

  loadContacts();
  refreshIcons();
})();
