/* 地图获客 - 右侧大地图逻辑（Leaflet + 高德瓦片，OSM 兜底） */
(function () {
  'use strict';

  var DEFAULT_CENTER = [22.54, 114.06]; // 深圳
  var DEFAULT_ZOOM = 13;
  var AMAP_TILES = 'https://webrd0{s}.is.autonavi.com/appmaptile?lang=zh_cn&size=1&scale=1&style=8&x={x}&y={y}&z={z}';
  var OSM_TILES = 'https://{s}.tile.openstreetmap.org/{z}/{x}/{y}.png';

  var map = null;
  var markerLayer = null;   // 聚合/商家标记层
  var heatLayer = null;     // 热力图层
  var pickMarker = null;    // 地图选点标记
  var currentJobId = null;  // 当前选中任务
  var currentPlaces = [];   // 当前任务商家
  var heatOn = false;
  var osmFallback = false;

  function initMap() {
    if (map || typeof L === 'undefined') return;

    map = L.map('map', {
      center: DEFAULT_CENTER,
      zoom: DEFAULT_ZOOM,
      zoomControl: false,
      attributionControl: true
    });

    var amap = L.tileLayer(AMAP_TILES, {
      subdomains: ['1', '2', '3', '4'],
      maxZoom: 18,
      attribution: '© 高德地图 AutoNavi'
    });

    // 高德瓦片加载失败时自动切换 OSM 兜底
    amap.on('tileerror', function () {
      if (osmFallback || !map) return;
      osmFallback = true;
      map.removeLayer(amap);
      L.tileLayer(OSM_TILES, {
        maxZoom: 19,
        attribution: '© OpenStreetMap contributors'
      }).addTo(map);
    });

    amap.addTo(map);

    markerLayer = L.layerGroup().addTo(map);
    heatLayer = L.layerGroup();

    // 缩放/拖动后重新聚合计数
    map.on('zoomend moveend', function () {
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
  }

  // 程序化写入标记：避免触发“手动输入”监听导致锚点被清掉
  var pickProgrammatic = false;

  function setLocationText(text) {
    pickProgrammatic = true;
    document.getElementById('locations').value = text;
    pickProgrammatic = false;
  }

  // 选点后立即回填坐标；随后尝试逆地理编码升级为可读地址（失败保留坐标）
  function syncLocationInput(lat, lng) {
    var coordText = lat + ', ' + lng;
    setLocationText(coordText);

    var ctrl = new AbortController();
    var timer = setTimeout(function () { ctrl.abort(); }, 3500);
    fetch('https://nominatim.openstreetmap.org/reverse?format=jsonv2&accept-language=zh&lat=' + lat + '&lon=' + lng, { signal: ctrl.signal })
      .then(function (res) { return res.ok ? res.json() : null; })
      .then(function (data) {
        clearTimeout(timer);
        // 仅当用户未再次手动改动输入框时才升级显示
        if (data && data.display_name && document.getElementById('locations').value === coordText) {
          setLocationText(data.display_name);
        }
      })
      .catch(function () { clearTimeout(timer); });
  }

  // 用户手动编辑「在哪里」时，清除地图选点锚定，以输入文本为准；
  // 同时在浏览器侧做地理编码预取（支持中文地名，如「曼谷」「新加坡」），
  // 定位成功后回填隐藏经纬度并把地图飞过去 —— 输入中文也能精准找到目标国家
  var geocodeTimer = null;
  var geocodeSeq = 0;

  function prefetchGeocode(text) {
    var seq = ++geocodeSeq;
    var ctrl = new AbortController();
    var timer = setTimeout(function () { ctrl.abort(); }, 5000);
    fetch('https://nominatim.openstreetmap.org/search?format=jsonv2&accept-language=zh&limit=1&q=' + encodeURIComponent(text), { signal: ctrl.signal })
      .then(function (res) { return res.ok ? res.json() : null; })
      .then(function (data) {
        clearTimeout(timer);
        if (seq !== geocodeSeq) return; // 已有更新的输入
        if (!data || !data.length) return;
        var lat = parseFloat(data[0].lat);
        var lon = parseFloat(data[0].lon);
        if (isNaN(lat) || isNaN(lon)) return;
        // 仅当用户仍在输入同一地点时回填锚点
        var cur = document.getElementById('locations').value.trim();
        if (cur !== text) return;
        document.getElementById('latitude').value = lat.toFixed(6);
        document.getElementById('longitude').value = lon.toFixed(6);
        if (map) map.flyTo([lat, lon], 12);
        showTip('已定位：' + (data[0].display_name || text));
      })
      .catch(function () { clearTimeout(timer); });
  }

  document.addEventListener('DOMContentLoaded', function () {
    var locInput = document.getElementById('locations');
    if (!locInput) return;
    locInput.addEventListener('input', function () {
      if (pickProgrammatic) return;
      document.getElementById('latitude').value = '0';
      document.getElementById('longitude').value = '0';
      var text = locInput.value.trim();
      if (geocodeTimer) clearTimeout(geocodeTimer);
      // 坐标格式文本（地图选点回填）不做地理编码
      if (text.length >= 2 && !/^-?\d+(\.\d+)?\s*,\s*-?\d+(\.\d+)?$/.test(text)) {
        geocodeTimer = setTimeout(function () { prefetchGeocode(text); }, 900);
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
    if (p.phone) parts.push('电话：' + escapeHtml(p.phone));
    if (p.emails) parts.push('邮箱：' + escapeHtml(p.emails));
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

  // ============ 结果栏 ============
  function updateResultBar(count, modeLabel) {
    var bar = document.getElementById('result-bar');
    if (!currentJobId) {
      bar.classList.add('hidden');
      return;
    }
    bar.classList.remove('hidden');
    document.getElementById('result-count').textContent = count;
    document.getElementById('result-mode').textContent = '（' + modeLabel + '）';
  }

  document.addEventListener('DOMContentLoaded', function () {
    document.getElementById('result-detail-btn').addEventListener('click', function () {
      if (!currentJobId) return;
      htmx.ajax('GET', '/view?id=' + encodeURIComponent(currentJobId), { target: '#map-modal-container', swap: 'innerHTML' });
    });
  });

  // ============ 任务选择与数据加载 ============
  window.selectRecord = function (el) {
    document.querySelectorAll('#job-list .record-item').forEach(function (r) {
      r.classList.toggle('active', r === el);
    });
    loadJob(el);
  };

  function loadJob(el) {
    var id = el.dataset.jobId;
    currentJobId = id;

    // 地图定位到任务锚点
    var lat = parseFloat(el.dataset.lat);
    var lon = parseFloat(el.dataset.lon);
    var hasAnchor = !isNaN(lat) && !isNaN(lon) && !(lat === 0 && lon === 0) &&
      Math.abs(lat) <= 90 && Math.abs(lon) <= 180;

    var modeTag = el.querySelector('.mode-tag');
    var modeLabel = modeTag ? modeTag.textContent.trim() : '快速模式';

    if (el.dataset.status !== 'ok') {
      currentPlaces = [];
      renderMarkers();
      updateResultBar(0, modeLabel);
      if (hasAnchor) map.setView([lat, lon], DEFAULT_ZOOM);
      return;
    }

    fetch('/api/v1/jobs/' + encodeURIComponent(id) + '/places')
      .then(function (res) { return res.ok ? res.json() : []; })
      .then(function (places) {
        currentPlaces = places || [];
        renderMarkers();
        updateResultBar(currentPlaces.length, modeLabel);

        if (currentPlaces.length) {
          var bounds = currentPlaces.map(function (p) { return [p.latitude, p.longitude]; });
          map.fitBounds(bounds, { padding: [60, 60], maxZoom: 15 });
        } else if (hasAnchor) {
          map.setView([lat, lon], DEFAULT_ZOOM);
        }

        if (heatOn) renderHeat();
      })
      .catch(function () {
        currentPlaces = [];
        renderMarkers();
        updateResultBar(0, modeLabel);
      });
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
      renderMarkers();
      updateResultBar(0, '');
      return;
    }

    // 保留已选任务；否则选第一个已完成任务，再退化为第一条
    var selected = null;
    if (currentJobId) {
      selected = records.filter(function (r) { return r.dataset.jobId === currentJobId; })[0] || null;
    }
    if (!selected) {
      selected = records.filter(function (r) { return r.dataset.status === 'ok'; })[0] || records[0];
    }
    selectRecord(selected);
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
        map.setView([pos.coords.latitude, pos.coords.longitude], 15);
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
  });

  // 启动
  if (document.readyState === 'loading') {
    document.addEventListener('DOMContentLoaded', initMap);
  } else {
    initMap();
  }
})();
