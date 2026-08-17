package engine

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"
)

const (
	directoryBrowseDefaultLimit = 20
	directoryBrowseMaxLimit     = 50
	directoryCoverageTTL        = 2 * time.Minute
)

// usefulSocialPlatforms are the ones a salesperson can actually open.
var usefulSocialPlatforms = map[string]bool{
	PlatformFacebook:  true,
	PlatformInstagram: true,
	PlatformLinkedIn:  true,
	PlatformYouTube:   true,
	PlatformX:         true,
	PlatformTikTok:    true,
	PlatformDouyin:    true,
}

const (
	sqlNoSocial = `NOT EXISTS (
  SELECT 1 FROM merchant_profiles p
  WHERE p.ext_id=m.ext_id AND p.platform NOT IN ('website',''))`
	sqlUsefulSocial = `EXISTS (
  SELECT 1 FROM merchant_profiles p
  WHERE p.ext_id=m.ext_id AND p.platform IN ('facebook','instagram','linkedin','youtube','x','tiktok','douyin'))`
	sqlAnySocial = `EXISTS (
  SELECT 1 FROM merchant_profiles p
  WHERE p.ext_id=m.ext_id AND p.platform NOT IN ('website',''))`
	sqlRealHomepage = `m.homepage!='' AND m.homepage NOT LIKE '%gleif.org%' AND m.homepage NOT LIKE '%openstreetmap.org%' AND m.homepage NOT LIKE '%wikidata.org%'`
)

// CountPair is a labeled total used in coverage breakdowns.
type CountPair struct {
	Key string `json:"key"`
	N   int    `json:"n"`
}

// DirectoryCoverage splits the dump so GLEIF legal names are not counted
// as "shops missing socials".
type DirectoryCoverage struct {
	Merchants           int            `json:"merchants"`
	BySource            map[string]int `json:"by_source"`
	LegalNameOnly       int            `json:"legal_name_only"`
	Operating           int            `json:"operating"`
	WithUsefulSocial    int            `json:"with_useful_social"`
	WithAnySocial       int            `json:"with_any_social"`
	NoSocial            int            `json:"no_social"`
	NoSocialButHomepage int            `json:"no_social_but_homepage"`
	ByPlatform          map[string]int `json:"by_platform,omitempty"`
	UsefulByPlatform    map[string]int `json:"useful_by_platform,omitempty"`
	LegalByCountry      []CountPair    `json:"legal_by_country,omitempty"`
	LegalByCategory     []CountPair    `json:"legal_by_category,omitempty"`
	Note                string         `json:"note"`
}

// DirectoryBrowseQuery pages through the local dump.
type DirectoryBrowseQuery struct {
	Filter  string
	Source  string
	Country string
	Name    string
	Limit   int
	Offset  int
}

// DirectoryBrowse is one page of dump rows plus coverage.
type DirectoryBrowse struct {
	Coverage DirectoryCoverage `json:"coverage"`
	Total    int               `json:"total"`
	Offset   int               `json:"offset"`
	Limit    int               `json:"limit"`
	Filter   string            `json:"filter"`
	Source   string            `json:"source,omitempty"`
	Country  string            `json:"country,omitempty"`
	Rows     []Merchant        `json:"rows"`
	Note     string            `json:"note"`
}

func (d *Directory) Coverage(ctx context.Context) (DirectoryCoverage, error) {
	var empty DirectoryCoverage
	if d == nil || d.db == nil {
		return empty, nil
	}
	d.covMu.Lock()
	defer d.covMu.Unlock()
	if time.Since(d.covAt) < directoryCoverageTTL && d.cov.Merchants > 0 {
		return d.cov, nil
	}
	cov, err := d.loadCoverage(ctx)
	if err != nil {
		return empty, err
	}
	d.cov = cov
	d.covAt = time.Now()
	return cov, nil
}

func (d *Directory) loadCoverage(ctx context.Context) (DirectoryCoverage, error) {
	var cov DirectoryCoverage
	var err error
	if cov.Merchants, err = d.Count(ctx); err != nil {
		return cov, err
	}
	cov.BySource = map[string]int{}
	srcRows, err := d.db.QueryContext(ctx, `SELECT source, COUNT(*) FROM merchants GROUP BY source`)
	if err != nil {
		return cov, err
	}
	for srcRows.Next() {
		var src string
		var n int
		if err := srcRows.Scan(&src, &n); err != nil {
			_ = srcRows.Close()
			return cov, err
		}
		cov.BySource[src] = n
	}
	if err := srcRows.Err(); err != nil {
		_ = srcRows.Close()
		return cov, err
	}
	_ = srcRows.Close()

	if err := d.db.QueryRowContext(ctx, `
SELECT COUNT(DISTINCT ext_id) FROM merchant_profiles
WHERE platform NOT IN ('website','')`).Scan(&cov.WithAnySocial); err != nil {
		return cov, err
	}
	if err := d.db.QueryRowContext(ctx, `
SELECT COUNT(DISTINCT ext_id) FROM merchant_profiles
WHERE platform IN ('facebook','instagram','linkedin','youtube','x','tiktok','douyin')`).Scan(&cov.WithUsefulSocial); err != nil {
		return cov, err
	}
	cov.NoSocial = cov.Merchants - cov.WithAnySocial
	if cov.NoSocial < 0 {
		cov.NoSocial = 0
	}

	gleif := cov.BySource["gleif"]
	osm := cov.BySource["osm"]
	wikidata := cov.BySource["wikidata"]
	var gleifWithHome, gleifWithSocial int
	_ = d.db.QueryRowContext(ctx, `
SELECT COUNT(*) FROM merchants
WHERE source='gleif' AND homepage!='' AND homepage NOT LIKE '%gleif.org%'`).Scan(&gleifWithHome)
	_ = d.db.QueryRowContext(ctx, `
SELECT COUNT(DISTINCT m.ext_id) FROM merchants m
JOIN merchant_profiles p ON p.ext_id=m.ext_id
WHERE m.source='gleif' AND p.platform NOT IN ('website','')`).Scan(&gleifWithSocial)
	cov.LegalNameOnly = gleif - gleifWithHome
	if cov.LegalNameOnly < 0 {
		cov.LegalNameOnly = 0
	}
	cov.Operating = osm + wikidata + gleifWithHome
	_ = d.db.QueryRowContext(ctx, `
SELECT COUNT(*) FROM merchants m
WHERE `+sqlRealHomepage+` AND `+sqlNoSocial).Scan(&cov.NoSocialButHomepage)

	platRows, err := d.db.QueryContext(ctx, `SELECT platform, COUNT(*) FROM merchant_profiles GROUP BY platform`)
	if err != nil {
		return cov, err
	}
	cov.ByPlatform = map[string]int{}
	cov.UsefulByPlatform = map[string]int{}
	for platRows.Next() {
		var plat string
		var n int
		if err := platRows.Scan(&plat, &n); err != nil {
			_ = platRows.Close()
			return cov, err
		}
		cov.ByPlatform[plat] = n
		if usefulSocialPlatforms[plat] {
			cov.UsefulByPlatform[plat] = n
		}
	}
	if err := platRows.Err(); err != nil {
		_ = platRows.Close()
		return cov, err
	}
	_ = platRows.Close()

	cov.LegalByCountry, err = d.countPairs(ctx, `
SELECT country, COUNT(*) FROM merchants WHERE source='gleif' AND country!=''
GROUP BY country ORDER BY 2 DESC LIMIT 15`)
	if err != nil {
		return cov, err
	}
	cov.LegalByCategory, err = d.countPairs(ctx, `
SELECT shop, COUNT(*) FROM merchants WHERE source='gleif'
GROUP BY shop ORDER BY 2 DESC`)
	if err != nil {
		return cov, err
	}
	cov.Note = directoryCoverageNote(cov)
	return cov, nil
}

func directoryCoverageNote(cov DirectoryCoverage) string {
	return fmt.Sprintf(
		"库里 %s 条里，约 %s 条是 GLEIF 法律登记名（只有国别/城市和 LEI 页，没有官网和社媒）。真正能当店铺/公司用的大约 %s 条；其中约 %s 条挂了 Facebook / Instagram / LinkedIn / YouTube / X / TikTok / 抖音。公开源补不齐那 300 多万条空法律名，也灌不进各平台全量企业号。",
		formatInt(cov.Merchants), formatInt(cov.LegalNameOnly), formatInt(cov.Operating), formatInt(cov.WithUsefulSocial),
	)
}

func formatInt(n int) string {
	s := strconv.Itoa(n)
	if n < 0 {
		s = strconv.Itoa(-n)
	}
	var b strings.Builder
	for i, r := range s {
		if i > 0 && (len(s)-i)%3 == 0 {
			b.WriteByte(',')
		}
		b.WriteRune(r)
	}
	out := b.String()
	if n < 0 {
		return "-" + out
	}
	return out
}

func (d *Directory) countPairs(ctx context.Context, q string) ([]CountPair, error) {
	rows, err := d.db.QueryContext(ctx, q)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []CountPair
	for rows.Next() {
		var p CountPair
		if err := rows.Scan(&p.Key, &p.N); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

func (d *Directory) Browse(ctx context.Context, q DirectoryBrowseQuery) (DirectoryBrowse, error) {
	var out DirectoryBrowse
	if d == nil {
		return out, nil
	}
	q.Filter = strings.ToLower(strings.TrimSpace(q.Filter))
	if q.Filter == "" {
		q.Filter = "legal_name"
	}
	q.Source = strings.ToLower(strings.TrimSpace(q.Source))
	q.Country = strings.ToUpper(strings.TrimSpace(q.Country))
	q.Name = strings.TrimSpace(q.Name)
	if q.Limit <= 0 {
		q.Limit = directoryBrowseDefaultLimit
	}
	if q.Limit > directoryBrowseMaxLimit {
		q.Limit = directoryBrowseMaxLimit
	}
	if q.Offset < 0 {
		q.Offset = 0
	}
	out.Filter = q.Filter
	out.Source = q.Source
	out.Country = q.Country
	out.Limit = q.Limit
	out.Offset = q.Offset

	cov, err := d.Coverage(ctx)
	if err != nil {
		return out, err
	}
	out.Coverage = cov

	where, args := browseWhere(q)
	countQ := `SELECT COUNT(*) FROM merchants m WHERE ` + where
	if err := d.db.QueryRowContext(ctx, countQ, args...).Scan(&out.Total); err != nil {
		return out, err
	}
	selectQ := `SELECT m.ext_id, m.source, m.name, m.shop, m.country, m.city, m.homepage, m.phone
FROM merchants m WHERE ` + where + ` ORDER BY m.id LIMIT ? OFFSET ?`
	args = append(args, q.Limit, q.Offset)
	rows, err := d.scanMerchants(ctx, selectQ, args...)
	if err != nil {
		return out, err
	}
	out.Rows = d.attachProfiles(ctx, rows)
	out.Note = browseNote(q.Filter)
	return out, nil
}

func browseWhere(q DirectoryBrowseQuery) (string, []any) {
	parts := []string{"1=1"}
	var args []any
	switch q.Filter {
	case "legal_name":
		parts = append(parts, `m.source='gleif'`)
		parts = append(parts, `(m.homepage='' OR m.homepage LIKE '%gleif.org%')`)
	case "no_social":
		parts = append(parts, sqlNoSocial)
	case "homepage":
		parts = append(parts, sqlRealHomepage)
		parts = append(parts, sqlNoSocial)
	case "with_social", "useful":
		parts = append(parts, sqlUsefulSocial)
	case "operating":
		parts = append(parts, `(m.source IN ('osm','wikidata') OR (`+sqlRealHomepage+`))`)
	}
	if q.Source != "" && q.Source != "all" {
		parts = append(parts, `m.source=?`)
		args = append(args, q.Source)
	}
	if q.Country != "" {
		parts = append(parts, `m.country=?`)
		args = append(args, q.Country)
	}
	if q.Name != "" {
		parts = append(parts, `m.name LIKE ?`)
		args = append(args, "%"+q.Name+"%")
	}
	return strings.Join(parts, " AND "), args
}

func browseNote(filter string) string {
	switch filter {
	case "legal_name":
		return "这些是 GLEIF 法律实体：公司/基金/个体户的登记名、国家、城市，主页是 LEI 查询页。Golden Copy 没有官网、电话、社媒字段。"
	case "no_social":
		return "还没挂上 Facebook / Instagram / LinkedIn 等主页的行。其中绝大多数是上面那种法律名，不是门店。"
	case "homepage":
		return "有真实官网、但还没从官网 HTML 抽出社媒的公司。补社媒应优先跑这一批。"
	case "with_social", "useful":
		return "已挂上 Facebook / Instagram / LinkedIn / YouTube / X / TikTok / 抖音的行。短视频企业号来自公开检索和 TikTok-Api / f2 sidecar。"
	case "operating":
		return "OSM 店铺、Wikidata 公司和带真实官网的行。搜配电柜这类品类时，引擎优先用这一层，而不是 300 万条法律名。"
	default:
		return "本地公开源目录，不是 Facebook/LinkedIn 全量企业号，也不是海关逐票库。"
	}
}

func (c *Client) DirectoryCoverage(ctx context.Context) (DirectoryCoverage, error) {
	if c == nil {
		return DirectoryCoverage{}, nil
	}
	return c.directory().Coverage(ctx)
}

func (c *Client) BrowseDirectory(ctx context.Context, q DirectoryBrowseQuery) (DirectoryBrowse, error) {
	if c == nil {
		return DirectoryBrowse{}, nil
	}
	return c.directory().Browse(ctx, q)
}
