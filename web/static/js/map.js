/* 地图获客 - 右侧大地图逻辑（Leaflet）
 *
 * 底图策略：
 * - 中国大陆视野：优先高德（中文注记更好）
 * - 海外视野：Carto Voyager（高德海外几乎无图，表现为灰底/空白）
 * - 任一失败时互相兜底
 */
(function () {
  'use strict';

  var DEFAULT_CENTER = [22.54, 114.06]; // 深圳
  var DEFAULT_ZOOM = 13;
  var AMAP_TILES = 'https://webrd0{s}.is.autonavi.com/appmaptile?lang=zh_cn&size=1&scale=1&style=8&x={x}&y={y}&z={z}';
  // Carto 全球可用，避免 OSM 官方瓦片对数据中心/无 Referer 请求的封锁
  var CARTO_TILES = 'https://{s}.basemaps.cartocdn.com/rastertiles/voyager/{z}/{x}/{y}{r}.png';

  var map = null;
  var baseLayer = null;
  var markerLayer = null;   // 聚合/商家标记层
  var heatLayer = null;     // 热力图层
  var pickMarker = null;    // 地图选点标记
  var currentJobId = null;  // 当前选中任务
  var currentPlaces = [];   // 当前任务商家
  var heatOn = false;
  var usingAmap = false;
  var baseSwitching = false;
  var livePollTimer = null; // 进行中任务：主地图结果增量轮询
  var dockCountCache = {};  // jobId -> place count
  var dockCountInflight = {};

  function inChinaView(lat, lng) {
    // 粗略中国大陆范围（含近海）；海外搜索（如纽约）走全球底图
    return lat >= 18 && lat <= 54 && lng >= 73 && lng <= 135;
  }

  function createAmapLayer() {
    return L.tileLayer(AMAP_TILES, {
      subdomains: ['1', '2', '3', '4'],
      maxZoom: 18,
      attribution: '© 高德地图 AutoNavi'
    });
  }

  function createCartoLayer() {
    return L.tileLayer(CARTO_TILES, {
      subdomains: 'abcd',
      maxZoom: 20,
      attribution: '© OpenStreetMap © CARTO'
    });
  }

  function setBaseLayer(preferAmap) {
    if (!map || baseSwitching) return;

    if (baseLayer && preferAmap === usingAmap) {
      return;
    }

    baseSwitching = true;

    var next = preferAmap ? createAmapLayer() : createCartoLayer();
    next.on('tileerror', function () {
      // 当前底图失败时切到另一套，避免灰屏
      if (preferAmap) {
        setBaseLayer(false);
      } else if (!usingAmap) {
        setBaseLayer(true);
      }
    });

    if (baseLayer) {
      map.removeLayer(baseLayer);
    }

    baseLayer = next.addTo(map);
    usingAmap = preferAmap;
    baseSwitching = false;
  }

  function syncBaseLayerForView() {
    if (!map) return;
    var c = map.getCenter();
    setBaseLayer(inChinaView(c.lat, c.lng));
  }

  function refreshMapSize() {
    if (!map) return;
    setTimeout(function () {
      map.invalidateSize({ pan: false });
      syncBaseLayerForView();
    }, 50);
  }

  function initMap() {
    if (map || typeof L === 'undefined') return;

    map = L.map('map', {
      center: DEFAULT_CENTER,
      zoom: DEFAULT_ZOOM,
      zoomControl: false,
      attributionControl: true
    });

    setBaseLayer(inChinaView(DEFAULT_CENTER[0], DEFAULT_CENTER[1]));

    markerLayer = L.layerGroup().addTo(map);
    heatLayer = L.layerGroup();

    // 缩放/拖动后重新聚合计数，并按视野切换底图
    map.on('zoomend moveend', function () {
      syncBaseLayerForView();
      renderMarkers();
      if (heatOn) renderHeat();
    });

    // 地图选点：点击写入隐藏经纬度字段，并同步到左侧「在哪里」输入框
    map.on('click', function (e) {
      var lat = e.latlng.lat.toFixed(6);
      var lng = e.latlng.lng.toFixed(6);
      document.getElementById('latitude').value = lat;
      document.getElementById('longitude').value = lng;

      if (pickMarker) pickMarker.remove();
      pickMarker = L.marker(e.latlng, {
        icon: L.divIcon({
          className: 'pick-marker-wrap',
          html: '<div class="pick-marker"></div>',
          iconSize: [20, 20],
          iconAnchor: [10, 10]
        })
      }).addTo(map);

      syncLocationInput(lat, lng);
      showTip('已选点：' + lat + ', ' + lng);
    });

    refreshMapSize();
  }

  // 程序化写入标记：避免触发“手动输入”监听导致锚点被清掉
  var pickProgrammatic = false;

  function setLocationText(text) {
    pickProgrammatic = true;
    document.getElementById('locations').value = text;
    pickProgrammatic = false;
  }

  // 选点后立即回填坐标；随后逆地理编码同步「在哪里」+ 目标国家
  function syncLocationInput(lat, lng) {
    var coordText = lat + ', ' + lng;
    setLocationText(coordText);

    var ctrl = new AbortController();
    var timer = setTimeout(function () { ctrl.abort(); }, 5000);
    // 走后端代理：避免浏览器直连 Nominatim 被 CSP/限流拦掉
    fetch('/api/v1/reverse-geocode?lat=' + encodeURIComponent(lat) + '&lon=' + encodeURIComponent(lng), {
      signal: ctrl.signal
    })
      .then(function (res) { return res.ok ? res.json() : null; })
      .then(function (data) {
        clearTimeout(timer);
        if (!data) return;
        // 仅当用户未再次手动改动输入框时才升级显示
        if (data.display_name && document.getElementById('locations').value === coordText) {
          // 优先短地名（城市级），便于搜索；过长则截断到前两段
          var name = shortenDisplayName(data.display_name);
          setLocationText(name || data.display_name);
        }
        if (data.country_code && window.selectCountryByCode) {
          window.selectCountryByCode(data.country_code);
        } else if (data.lang) {
          var langInput = document.getElementById('lang');
          if (langInput) langInput.value = data.lang;
        }
        showTip('已同步地点与国家');
      })
      .catch(function () { clearTimeout(timer); });
  }

  function shortenDisplayName(display) {
    if (!display) return '';
    var parts = display.split(',').map(function (s) { return s.trim(); }).filter(Boolean);
    if (parts.length <= 2) return parts.join(', ');
    // 取前两段 + 国家（最后一段），避免整段 OSM 长地址塞进输入框
    var country = parts[parts.length - 1];
    return parts[0] + ', ' + parts[1] + (country && country !== parts[1] ? ', ' + country : '');
  }

  // 用户手动编辑「在哪里」时，清除地图选点锚定，以输入文本为准；
  // 同时在浏览器侧做地理编码预取（支持中文地名，如「曼谷」「新加坡」），
  // 定位成功后回填隐藏经纬度并把地图飞过去 —— 输入中文也能精准找到目标国家
  var geocodeTimer = null;
  var geocodeSeq = 0;

  function prefetchGeocode(text) {
    var seq = ++geocodeSeq;
    var ctrl = new AbortController();
    var timer = setTimeout(function () { ctrl.abort(); }, 8000);
    // 走后端代理：支持中文海外地名，并带上目标地推荐语言
    fetch('/api/v1/geocode?q=' + encodeURIComponent(text), { signal: ctrl.signal })
      .then(function (res) { return res.ok ? res.json() : null; })
      .then(function (data) {
        clearTimeout(timer);
        if (seq !== geocodeSeq) return; // 已有更新的输入
        if (!data) {
          showTip('未找到地点：' + text);
          return;
        }
        var lat = parseFloat(data.lat);
        var lon = parseFloat(data.lon);
        if (isNaN(lat) || isNaN(lon)) return;
        // 仅当用户仍在输入同一地点时回填锚点
        var cur = document.getElementById('locations').value.trim();
        if (cur !== text) return;
        document.getElementById('latitude').value = lat.toFixed(6);
        document.getElementById('longitude').value = lon.toFixed(6);
        // 海外中文地名：按国家同步左侧「目标国家」与 hl
        if (data.country_code && window.selectCountryByCode) {
          window.selectCountryByCode(data.country_code);
        } else if (data.lang) {
          var langInput = document.getElementById('lang');
          if (langInput) langInput.value = data.lang;
        }
        if (map) {
          setBaseLayer(inChinaView(lat, lon));
          map.flyTo([lat, lon], 12);
          refreshMapSize();
        }
        showTip('已定位：' + (data.display_name || text));
      })
      .catch(function () {
        clearTimeout(timer);
        showTip('地点定位失败，仍可提交（后端会再试一次）');
      });
  }

  document.addEventListener('DOMContentLoaded', function () {
    var locInput = document.getElementById('locations');
    if (!locInput) return;
    function scheduleGeocode() {
      if (pickProgrammatic) return;
      document.getElementById('latitude').value = '0';
      document.getElementById('longitude').value = '0';
      var text = locInput.value.trim();
      if (geocodeTimer) clearTimeout(geocodeTimer);
      // 坐标格式文本（地图选点回填）不做地理编码
      if (text.length >= 2 && !/^-?\d+(\.\d+)?\s*,\s*-?\d+(\.\d+)?$/.test(text)) {
        geocodeTimer = setTimeout(function () { prefetchGeocode(text); }, 450);
      }
    }
    locInput.addEventListener('input', scheduleGeocode);
    locInput.addEventListener('change', scheduleGeocode);
    locInput.addEventListener('blur', function () {
      var text = locInput.value.trim();
      if (text.length >= 2 && document.getElementById('latitude').value === '0') {
        prefetchGeocode(text);
      }
    });
  });

  // 清除地图选点
  window.clearPickMarker = function () {
    if (pickMarker) { pickMarker.remove(); pickMarker = null; }
    document.getElementById('latitude').value = '0';
    document.getElementById('longitude').value = '0';
  };

  // ============ 标记渲染（简易网格聚合） ============
  function clusterIcon(count) {
    var size = count >= 10 ? 44 : count >= 5 ? 38 : count >= 2 ? 32 : 14;
    var cls = count > 1 ? 'cluster-marker' : 'dot-marker';
    var html = count > 1
      ? '<div class="' + cls + '" style="width:' + size + 'px;height:' + size + 'px;">' + count + '</div>'
      : '<div class="' + cls + '"></div>';
    return L.divIcon({
      className: 'cluster-wrap',
      html: html,
      iconSize: [size, size],
      iconAnchor: [size / 2, size / 2]
    });
  }

  function placePopup(p) {
    var parts = ['<strong>' + escapeHtml(p.title || '未命名商家') + '</strong>'];
    if (p.address) parts.push(escapeHtml(p.address));
    if (p.emails) parts.push('邮箱：' + escapeHtml(p.emails));
    if (p.phone) parts.push('电话：' + escapeHtml(p.phone));
    if (p.review_rating) parts.push('评分：★ ' + escapeHtml(String(p.review_rating)));
    return parts.join('<br>');
  }

  function escapeHtml(str) {
    var div = document.createElement('div');
    div.textContent = str == null ? '' : str;
    return div.innerHTML;
  }

  function renderMarkers() {
    if (!map || !markerLayer) return;
    markerLayer.clearLayers();

    if (!currentPlaces.length) return;

    // 按当前缩放的屏幕像素网格聚合
    var cellPx = 80;
    var cells = {};
    currentPlaces.forEach(function (p) {
      if (typeof p.latitude !== 'number' || typeof p.longitude !== 'number') return;
      if (!isFinite(p.latitude) || !isFinite(p.longitude)) return;
      var pt = map.latLngToContainerPoint([p.latitude, p.longitude]);
      var key = Math.floor(pt.x / cellPx) + ':' + Math.floor(pt.y / cellPx);
      (cells[key] = cells[key] || []).push(p);
    });

    Object.keys(cells).forEach(function (key) {
      var group = cells[key];
      var lat = 0, lng = 0;
      group.forEach(function (p) { lat += p.latitude; lng += p.longitude; });
      lat /= group.length;
      lng /= group.length;

      var marker = L.marker([lat, lng], { icon: clusterIcon(group.length) });
      if (group.length === 1) {
        marker.bindPopup(placePopup(group[0]));
      } else {
        marker.bindPopup('<strong>' + group.length + ' 个商家</strong><br>放大地图查看详情');
        marker.on('click', function () {
          map.setView([lat, lng], Math.min(map.getZoom() + 2, 18));
        });
      }
      marker.addTo(markerLayer);
    });
  }

  // ============ 热力图（橙色光晕圆点叠加） ============
  function renderHeat() {
    if (!map) return;
    heatLayer.clearLayers();
    if (!heatOn || !currentPlaces.length) return;

    currentPlaces.forEach(function (p) {
      if (typeof p.latitude !== 'number' || typeof p.longitude !== 'number') return;
      var weight = p.review_rating ? Math.min(p.review_rating / 5, 1) : 0.5;
      L.circleMarker([p.latitude, p.longitude], {
        radius: 26,
        stroke: false,
        fillColor: '#FAAD14',
        fillOpacity: 0.18 + weight * 0.22,
        interactive: false
      }).addTo(heatLayer);
      L.circleMarker([p.latitude, p.longitude], {
        radius: 10,
        stroke: false,
        fillColor: '#FA541C',
        fillOpacity: 0.25 + weight * 0.3,
        interactive: false
      }).addTo(heatLayer);
    });

    if (!map.hasLayer(heatLayer)) heatLayer.addTo(map);
  }

  window.toggleHeatmap = function (el) {
    el.classList.toggle('on');
    heatOn = el.classList.contains('on');
    if (heatOn) {
      if (!currentPlaces.length) {
        showTip('当前任务暂无可视化的商家数据');
        el.classList.remove('on');
        heatOn = false;
        return;
      }
      renderHeat();
    } else if (map && map.hasLayer(heatLayer)) {
      map.removeLayer(heatLayer);
    }
  };

  // 结果栏仅在用户点击右侧任务后显示（不自动展开）
  var detailUnlocked = false;

  // ============ 结果栏 ============
  function updateResultBar(count, modeLabel) {
    var bar = document.getElementById('result-bar');
    if (!currentJobId || !detailUnlocked) {
      bar.classList.add('hidden');
      return;
    }
    bar.classList.remove('hidden');
    document.getElementById('result-count').textContent = count;
    document.getElementById('result-mode').textContent = '（' + modeLabel + '）';
  }

  // 打开结果详情（底部表 + 右侧背调）；仅任务坞点击后调用
  window.openJobView = function (jobId) {
    if (!jobId) return;
    detailUnlocked = true;
    htmx.ajax('GET', '/view?id=' + encodeURIComponent(jobId), {
      target: '#map-modal-container',
      swap: 'innerHTML'
    });
  };

  document.addEventListener('DOMContentLoaded', function () {
    document.getElementById('result-detail-btn').addEventListener('click', function () {
      if (!currentJobId) return;
      window.openJobView(currentJobId);
    });
  });

  function stopLivePoll() {
    if (livePollTimer) {
      clearInterval(livePollTimer);
      livePollTimer = null;
    }
  }

  function applyPlacesToMap(places, modeLabel, hasAnchor, lat, lon, fit) {
    currentPlaces = places || [];
    renderMarkers();
    updateResultBar(currentPlaces.length, modeLabel);
    dockCountCache[currentJobId] = currentPlaces.length;
    window.syncTaskDock && window.syncTaskDock();

    if (currentPlaces.length) {
      var mid = currentPlaces[0];
      setBaseLayer(inChinaView(mid.latitude, mid.longitude));
      if (fit) {
        var bounds = currentPlaces.map(function (p) { return [p.latitude, p.longitude]; });
        map.fitBounds(bounds, { padding: [60, 60], maxZoom: 15 });
      }
    } else if (hasAnchor) {
      setBaseLayer(inChinaView(lat, lon));
      map.setView([lat, lon], DEFAULT_ZOOM);
    }

    refreshMapSize();
    if (heatOn) renderHeat();
  }

  function fetchPlacesForJob(id, modeLabel, hasAnchor, lat, lon, fit) {
    return fetch('/api/v1/jobs/' + encodeURIComponent(id) + '/places')
      .then(function (res) { return res.ok ? res.json() : []; })
      .then(function (places) {
        if (currentJobId !== id) return;
        applyPlacesToMap(places, modeLabel, hasAnchor, lat, lon, fit);
      })
      .catch(function () {
        if (currentJobId !== id) return;
        if (!currentPlaces.length) {
          applyPlacesToMap([], modeLabel, hasAnchor, lat, lon, false);
        }
      });
  }

  // ============ 右侧任务进度面板 ============
  function statusLabel(status) {
    if (status === 'working') return '进行中';
    if (status === 'pending') return '排队中';
    if (status === 'ok') return '已完成';
    if (status === 'failed') return '失败';
    return status || '未知';
  }

  function modeFromRecord(el) {
    var tag = el.querySelector('.mode-tag');
    return tag ? tag.textContent.trim() : '快速模式';
  }

  function refreshDockCount(jobId) {
    if (!jobId || dockCountInflight[jobId]) return;
    dockCountInflight[jobId] = true;
    fetch('/api/v1/jobs/' + encodeURIComponent(jobId) + '/places')
      .then(function (res) { return res.ok ? res.json() : []; })
      .then(function (places) {
        dockCountCache[jobId] = (places || []).length;
        var countEl = document.querySelector('#task-dock-list [data-dock-id="' + jobId + '"] .task-dock-count');
        if (countEl) {
          var n = dockCountCache[jobId] || 0;
          countEl.textContent = n ? ('已抓 ' + n + ' 家') : '等待首条结果…';
        }
      })
      .catch(function () {})
      .finally(function () { delete dockCountInflight[jobId]; });
  }

  window.syncTaskDock = function () {
    var list = document.getElementById('task-dock-list');
    if (!list) return;

    var records = Array.prototype.slice.call(document.querySelectorAll('#job-list .record-item'));
    if (!records.length) {
      list.innerHTML = '<p class="task-dock-empty">还没有任务。填写左侧表单后点击「开始搜索」。</p>';
      return;
    }

    // 进行中优先，其次最近完成/失败
    var running = records.filter(function (r) {
      return r.dataset.status === 'working' || r.dataset.status === 'pending';
    });
    var done = records.filter(function (r) {
      return r.dataset.status === 'ok' || r.dataset.status === 'failed';
    }).slice(0, 6);
    var shown = running.concat(done);

    list.innerHTML = shown.map(function (el) {
      var id = el.dataset.jobId;
      var status = el.dataset.status || '';
      var name = (el.querySelector('.record-name') || {}).textContent || '未命名任务';
      var mode = modeFromRecord(el);
      var count = dockCountCache[id];
      var countText = typeof count === 'number'
        ? (count ? ('已抓 ' + count + ' 家') : (status === 'ok' ? '暂无结果' : '等待首条结果…'))
        : (status === 'working' || status === 'pending' ? '同步进度…' : '查看详情');
      var active = id === currentJobId ? ' active' : '';
      return '<button type="button" class="task-dock-item status-' + status + active + '" data-dock-id="' + id + '" onclick="window.focusTaskFromDock(\'' + id + '\')">' +
        '<div class="task-dock-item-top">' +
          '<span class="task-dock-name">' + escapeHtml(name.trim()) + '</span>' +
          '<span class="task-dock-badge">' + statusLabel(status) + '</span>' +
        '</div>' +
        '<div class="task-dock-item-meta">' +
          '<span>' + escapeHtml(mode) + '</span>' +
          '<span class="task-dock-count">' + countText + '</span>' +
        '</div>' +
      '</button>';
    }).join('');

    // 进行中的任务持续拉数量（结果逐渐增加）
    running.forEach(function (el) { refreshDockCount(el.dataset.jobId); });
    done.slice(0, 3).forEach(function (el) {
      if (typeof dockCountCache[el.dataset.jobId] !== 'number') {
        refreshDockCount(el.dataset.jobId);
      }
    });

    if (window.lucide) lucide.createIcons();
  };

  window.focusTaskFromDock = function (jobId) {
    detailUnlocked = true;
    var el = document.querySelector('#job-list .record-item[data-job-id="' + jobId + '"]');
    if (el) {
      window.selectRecord(el);
    } else {
      currentJobId = jobId;
    }
    // 只有点击右侧任务后，才展开下方结果详情
    window.openJobView(jobId);
  };

  // ============ 任务选择与数据加载 ============
  window.selectRecord = function (el) {
    document.querySelectorAll('#job-list .record-item').forEach(function (r) {
      r.classList.toggle('active', r === el);
    });
    loadJob(el);
    window.syncTaskDock && window.syncTaskDock();
  };

  function loadJob(el) {
    var id = el.dataset.jobId;
    currentJobId = id;
    stopLivePoll();

    // 地图定位到任务锚点
    var lat = parseFloat(el.dataset.lat);
    var lon = parseFloat(el.dataset.lon);
    var hasAnchor = !isNaN(lat) && !isNaN(lon) && !(lat === 0 && lon === 0) &&
      Math.abs(lat) <= 90 && Math.abs(lon) <= 180;

    var modeTag = el.querySelector('.mode-tag');
    var modeLabel = modeTag ? modeTag.textContent.trim() : '快速模式';
    var status = el.dataset.status;

    // 进行中/排队：先定位，再轮询增量结果（主地图标记逐渐增加）
    if (status === 'working' || status === 'pending') {
      currentPlaces = [];
      renderMarkers();
      updateResultBar(dockCountCache[id] || 0, modeLabel);
      if (hasAnchor && map) {
        setBaseLayer(inChinaView(lat, lon));
        map.setView([lat, lon], DEFAULT_ZOOM);
        refreshMapSize();
      }
      var fitted = false;
      fetchPlacesForJob(id, modeLabel, hasAnchor, lat, lon, true).then(function () {
        fitted = currentPlaces.length > 0;
      });
      livePollTimer = setInterval(function () {
        var rec = document.querySelector('#job-list .record-item[data-job-id="' + id + '"]');
        if (!rec) return;
        var st = rec.dataset.status;
        fetchPlacesForJob(id, modeLabel, hasAnchor, lat, lon, !fitted).then(function () {
          if (currentPlaces.length) fitted = true;
        });
        if (st === 'ok' || st === 'failed') {
          stopLivePoll();
          // 完成后最后拉一次完整结果
          fetchPlacesForJob(id, modeLabel, hasAnchor, lat, lon, false);
        }
      }, 4000);
      return;
    }

    if (status !== 'ok') {
      currentPlaces = [];
      renderMarkers();
      updateResultBar(0, modeLabel);
      if (hasAnchor && map) {
        setBaseLayer(inChinaView(lat, lon));
        map.setView([lat, lon], DEFAULT_ZOOM);
        refreshMapSize();
      }
      return;
    }

    fetchPlacesForJob(id, modeLabel, hasAnchor, lat, lon, true);
  }

  // 记录列表刷新后同步：相对时间 + 默认选中任务
  window.syncMapWithRecords = function () {
    var records = Array.prototype.slice.call(document.querySelectorAll('#job-list .record-item'));

    // 相对时间
    records.forEach(function (el) {
      var ts = parseInt(el.dataset.ts, 10);
      var timeEl = el.querySelector('.record-time');
      if (ts && timeEl) timeEl.textContent = formatTimeAgo(ts);
    });

    if (!records.length) {
      currentJobId = null;
      currentPlaces = [];
      detailUnlocked = false;
      stopLivePoll();
      renderMarkers();
      updateResultBar(0, '');
      window.syncTaskDock && window.syncTaskDock();
      return;
    }

    // 不自动展开结果：仅当用户已点过右侧任务且任务仍存在时，继续同步该任务
    if (detailUnlocked && currentJobId) {
      var selected = records.filter(function (r) { return r.dataset.jobId === currentJobId; })[0] || null;
      if (selected) {
        selectRecord(selected);
      }
    } else {
      // 未点任务：只刷任务坞，不展开下方结果栏/详情
      updateResultBar(0, '');
    }
    refreshMapSize();
    window.syncTaskDock && window.syncTaskDock();
  };

  function formatTimeAgo(tsSec) {
    var diff = Math.floor(Date.now() / 1000) - tsSec;
    if (diff < 60) return '刚刚';
    if (diff < 3600) return Math.floor(diff / 60) + ' 分钟前';
    if (diff < 86400) return Math.floor(diff / 3600) + ' 小时前';
    if (diff < 172800) return '昨天';
    return Math.floor(diff / 86400) + ' 天前';
  }

  // ============ 地图控件 ============
  window.zoomMap = function (delta) {
    if (!map) return;
    if (delta > 0) map.zoomIn(); else map.zoomOut();
  };

  window.locateMe = function () {
    if (!map) return;
    if (!navigator.geolocation) {
      showTip('当前浏览器不支持定位');
      return;
    }
    navigator.geolocation.getCurrentPosition(
      function (pos) {
        setBaseLayer(inChinaView(pos.coords.latitude, pos.coords.longitude));
        map.setView([pos.coords.latitude, pos.coords.longitude], 15);
        refreshMapSize();
      },
      function () { showTip('定位失败，请检查浏览器权限'); },
      { timeout: 8000 }
    );
  };

  function showTip(text) {
    var tip = document.getElementById('pick-tip');
    tip.textContent = text;
    tip.classList.remove('hidden');
    clearTimeout(tip._timer);
    tip._timer = setTimeout(function () { tip.classList.add('hidden'); }, 3000);
  }

  // ============ 在当前区域重新搜索 ============
  document.addEventListener('DOMContentLoaded', function () {
    document.getElementById('research-btn').addEventListener('click', function () {
      if (!map) return;
      var keywords = document.getElementById('keywords').value.trim();
      if (!keywords) {
        showTip('请先输入「找什么」再搜索');
        document.getElementById('keywords').focus();
        return;
      }
      var center = map.getCenter();
      document.getElementById('latitude').value = center.lat.toFixed(6);
      document.getElementById('longitude').value = center.lng.toFixed(6);
      document.getElementById('search-form').requestSubmit();
    });

    // 抓取提交遮罩消失后，强制重算地图尺寸，避免灰屏/半截图
    document.body.addEventListener('htmx:afterRequest', function (event) {
      if (event.detail && event.detail.elt && event.detail.elt.id === 'search-form') {
        document.getElementById('map-area').classList.remove('map-loading');
        refreshMapSize();
      }
    });
  });

  // 启动
  if (document.readyState === 'loading') {
    document.addEventListener('DOMContentLoaded', initMap);
  } else {
    initMap();
  }
})();
