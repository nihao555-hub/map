# 地图获客使用指南 / Panduan Pengguna Map Leads

> 中印尼双语说明 · Panduan dwibahasa (中文 / Bahasa Indonesia)

---

## 1. 邀请码登录 / Login dengan kode undangan

**中文**

- 邀请码 = 账号：同一邀请码可重复登录。
- 该码下的历史任务会一直保留。
- 登录会话约 30 天；过期后用同一邀请码重新登录即可。

**Bahasa Indonesia**

- Kode undangan = akun Anda: kode yang sama bisa dipakai login berulang.
- Riwayat tugas di bawah kode tersebut tetap tersimpan.
- Sesi login sekitar 30 hari; setelah kedaluwarsa, login lagi dengan kode yang sama.

---

## 2. 界面多语言 / Bahasa antarmuka

支持语言 / Bahasa yang didukung：

| 代码 Code | 语言 Bahasa |
|-----------|-------------|
| zh | 中文 |
| en | English |
| id | Bahasa Indonesia |
| ms | Bahasa Melayu |
| th | ไทย |
| vi | Tiếng Việt |
| tl | Filipino |
| km | ខ្មែរ（界面文案暂回落英文 / UI teks sementara English） |
| lo | ລາວ（界面文案暂回落英文 / UI teks sementara English） |
| my | မြန်မာ |

**中文**

- 顶部（或邀请页）可切换「界面语言」。
- 选择后：页面文案、任务状态、以及 **AI 背调产出**（公司摘要等）都会按该语言生成。
- 语言偏好保存在浏览器本地（`gms_ui_lang`），创建任务时会随表单提交 `ui_lang`。

**Bahasa Indonesia**

- Ganti bahasa di bilah atas (atau halaman undangan).
- Setelah dipilih: teks UI, status tugas, dan **hasil intelijen AI** (ringkasan perusahaan, dll.) mengikuti bahasa itu.
- Preferensi disimpan di browser (`gms_ui_lang`) dan dikirim sebagai `ui_lang` saat membuat tugas.

---

## 3. 当地原语言搜索能不能抓到？ / Bisakah cari dengan bahasa lokal?

**答案：可以。 / Jawaban: Ya.**

**中文**

- Google Maps 搜索用的是「目标国家」对应的当地语言参数（`lang` / `hl`），与界面语言 **相互独立**。
- **推荐**：用当地原语言写「找什么」，例如印尼写 `kafe` / `importir`，泰国写当地品类词——通常命中更好。
- 也可用中文关键词：勾选「AI 译成目标国搜索词」后，系统会译成当地可搜词再去 Maps（需配置 AI Key）。
- 地点名可用中文或当地写法（如 `雅加达` / `Jakarta`），系统会按目标国家锚定。

**Bahasa Indonesia**

- Pencarian Google Maps memakai parameter bahasa negara target (`lang` / `hl`), **terpisah** dari bahasa antarmuka.
- **Disarankan**: isi kata kunci dalam bahasa lokal, mis. `kafe` / `importir` di Indonesia — biasanya hasilnya lebih tepat.
- Kata kunci Mandarin juga bisa: centang terjemahan AI ke istilah lokal, lalu sistem menerjemahkan sebelum mencari di Maps (perlu AI Key).
- Nama lokasi boleh Mandarin atau lokal (`雅加达` / `Jakarta`); sistem menambatkan ke negara target.

---

## 4. AI 产出语言规则 / Aturan bahasa output AI

**中文**

- 用户选的界面语言 = AI 背调摘要等自然语言字段的语言。
- 专有名词、邮箱、电话、网址保持原文，不做乱译。
- 地图里商户标题/地址等来自 Google，本身多为当地语言，不会强行改写成界面语言。

**Bahasa Indonesia**

- Bahasa UI yang dipilih = bahasa field teks AI (ringkasan intelijen, dll.).
- Nama diri, email, telepon, URL tetap asli.
- Judul/alamat bisnis dari Google biasanya tetap bahasa lokal; tidak dipaksa jadi bahasa UI.

---

## 5. 常用流程 / Alur umum

1. 用邀请码登录 / Login dengan kode undangan  
2. 选择界面语言 / Pilih bahasa UI  
3. 选择目标国家 / Pilih negara target  
4. 填写「在哪里」「找什么」（当地语或中文） / Isi lokasi & kata kunci  
5. 设置半径与模式后点「开始搜索」 / Atur radius & mode, lalu mulai  
6. 在任务坞查看进度，完成后导出 CSV / Pantau di panel tugas, ekspor CSV  

---

## 6. 并发与导出 / Konkuriensi & ekspor

**中文**

- 默认可并行多个抓取任务（部署侧可配，常见为 4）。
- 结果支持一键导出 CSV（UTF-8 BOM，便于 Excel 打开）。

**Bahasa Indonesia**

- Beberapa tugas scraping bisa berjalan paralel (konfigurasi server, biasanya 4).
- Hasil bisa diekspor CSV sekali klik (UTF-8 BOM, nyaman dibuka di Excel).

---

## 7. 语言与搜索对照速查 / Ringkas bahasa vs pencarian

| 概念 Konsep | 作用 Fungsi |
|-------------|-------------|
| 界面语言 UI language (`ui_lang`) | 控制页面文案 + AI 背调文字 / Kontrol teks UI + teks AI |
| 目标国家 Country | 决定地图锚定与 Maps `hl` / Menentukan jangkar peta & `hl` Maps |
| 关键词 Keywords | 建议当地原语言；中文可 AI 翻译 / Disarankan bahasa lokal; Mandarin bisa diterjemahkan AI |

---

如有部署或邀请码问题，请联系管理员。  
Untuk masalah deploy atau kode undangan, hubungi admin.
