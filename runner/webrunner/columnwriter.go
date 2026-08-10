package webrunner

import (
	"encoding/csv"

	"github.com/gosom/google-maps-scraper/csvout"
	"github.com/gosom/google-maps-scraper/gmaps"
)

// 内部强制保留列：结果地图与 places 接口解析 CSV 时依赖它们，
// 即使用户在深度模式里取消勾选也必须写出，否则前端表格/地图会失效。
var forcedColumns = []string{
	"input_id",
	"link",
	"title",
	"latitude",
	"longitude",
	"cid",
	"place_id",
}

// 快速模式必要列（对客户有用的核心字段）
var essentialColumns = []string{
	"title", "address", "phone", "emails",
	"review_rating", "review_count", "website",
}

// 背调必要列：开启背调时无论快速/深度模式都写出，
// 保证「谁能联系、怎么联系、有多可信」这三件事一定在表里。
var essentialResearchColumns = []string{
	"research_completeness", "best_email", "role_emails",
	"contact_people", "linkedin", "whatsapp",
	"trade_roles", "certifications",
	"lei", "mail_provider", "tech_stack", "domain_age_days",
}

// newColumnWriter 按任务配置过滤 CSV 输出列：
//   - 快速模式：必要列 + 内部强制列
//   - 深度/网格模式：默认全部列；用户选了 columns 时 = 选中列 + 内部强制列
//
// 开启背调时追加背调列；未开启则列布局与原来完全一致。
func newColumnWriter(w *csv.Writer, fastMode bool, columnsCSV string, companyResearch bool) *csvout.Writer {
	opts := []csvout.Option{}

	if companyResearch {
		opts = append(opts, csvout.WithResearchColumns(gmaps.ResearchCsvHeaders()))
	}

	if selected := selectedColumns(fastMode, columnsCSV, companyResearch); selected != nil {
		opts = append(opts, csvout.WithColumns(selected))
	}

	return csvout.New(w, opts...)
}

// selectedColumns 返回需要输出的列集合，nil 表示输出全部列。
func selectedColumns(fastMode bool, columnsCSV string, companyResearch bool) map[string]bool {
	var base []string

	switch {
	case columnsCSV != "":
		// 用户自选列（深度模式）
		base = splitCSV(columnsCSV)
	case fastMode:
		base = essentialColumns
	default:
		// 深度模式默认全部列
		return nil
	}

	selected := make(map[string]bool, len(base)+len(forcedColumns)+len(essentialResearchColumns))

	for _, c := range base {
		selected[c] = true
	}

	for _, c := range forcedColumns {
		selected[c] = true
	}

	if companyResearch {
		for _, c := range essentialResearchColumns {
			selected[c] = true
		}
	}

	return selected
}

func splitCSV(s string) []string {
	var out []string

	cur := ""

	for _, r := range s {
		if r == ',' || r == ' ' || r == '\n' {
			if cur != "" {
				out = append(out, cur)
				cur = ""
			}
		} else {
			cur += string(r)
		}
	}

	if cur != "" {
		out = append(out, cur)
	}

	return out
}
