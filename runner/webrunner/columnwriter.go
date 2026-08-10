package webrunner

import (
	"context"
	"encoding/csv"
	"fmt"
	"os"
	"reflect"
	"sync"

	"github.com/gosom/google-maps-scraper/placeref"
	"github.com/gosom/scrapemate"
)

// 内部强制保留列：结果地图与 places 接口解析 CSV 时依赖它们，
// 即使用户在深度模式里取消勾选也必须写出，否则前端表格/地图会失效。
var forcedColumns = map[string]bool{
	"input_id":  true,
	"link":      true,
	"title":     true,
	"latitude":  true,
	"longitude": true,
	"cid":       true,
	"place_id":  true,
}

// 快速模式必要列（对客户有用的核心字段）
var essentialColumns = []string{
	"title", "address", "whatsapp", "emails", "phone",
	"website", "facebook", "instagram", "linkedin",
	"review_rating", "review_count",
}

// columnWriter 按任务配置过滤 CSV 输出列：
//   - 快速模式：必要列 + 内部强制列
//   - 深度/网格模式：默认全部列；用户选了 columns 时 = 选中列 + 内部强制列
//
// 同一 place_id/cid 再次写入时 upsert（地点先落盘、邮箱任务稍后补联系方式）。
type columnWriter struct {
	w          *csv.Writer
	file       *os.File // 非 nil 时支持按 key 重写整表
	selected   map[string]bool
	wroteHead  bool
	headerOnce sync.Once
	mu         sync.Mutex
	headers    []string
	colIdx     []int
	rows       map[string][]string
	order      []string
}

func newColumnWriter(w *csv.Writer, fastMode bool, columnsCSV string, file *os.File) *columnWriter {
	cw := &columnWriter{
		w:    w,
		file: file,
		rows: make(map[string][]string),
	}

	if columnsCSV != "" {
		cw.selected = map[string]bool{}
		for _, c := range splitCSV(columnsCSV) {
			cw.selected[c] = true
		}
		for c := range forcedColumns {
			cw.selected[c] = true
		}
	} else if fastMode {
		cw.selected = map[string]bool{}
		for _, c := range essentialColumns {
			cw.selected[c] = true
		}
		for c := range forcedColumns {
			cw.selected[c] = true
		}
	}

	return cw
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

func (c *columnWriter) filterRow(headers []string, row []string) ([]int, []string) {
	var (
		idx []int
		out []string
	)

	for i, h := range headers {
		if c.selected == nil || c.selected[h] {
			idx = append(idx, i)

			if i < len(row) {
				out = append(out, row[i])
			} else {
				out = append(out, "")
			}
		}
	}

	return idx, out
}

func rowKey(headers, row []string) string {
	get := func(name string) string {
		for i, h := range headers {
			if h == name && i < len(row) {
				return row[i]
			}
		}

		return ""
	}

	if key := placeref.RowKey(get); key != "" {
		return key
	}

	return fmt.Sprintf("geo:%s|%s|%s", get("title"), get("latitude"), get("longitude"))
}

// mergeRow 邮箱任务回写时：空字段不覆盖已有非空值（避免详情被冲掉）。
// 联系方式列两边都有值时保留更「完整」的一侧（更长），防止并发写丢 WA/邮箱。
func mergeRow(old, neu []string) []string {
	if old == nil {
		return neu
	}

	out := make([]string, max(len(neu), len(old)))
	for i := range out {
		var n, o string
		if i < len(neu) {
			n = neu[i]
		}
		if i < len(old) {
			o = old[i]
		}
		switch {
		case n == "" && o != "":
			out[i] = o
		case n != "" && o == "":
			out[i] = n
		case n != "" && o != "":
			if len(o) > len(n) {
				out[i] = o
			} else {
				out[i] = n
			}
		default:
			out[i] = n
		}
	}

	return out
}

func (c *columnWriter) rewriteLocked() error {
	if c.file == nil {
		return nil
	}

	if _, err := c.file.Seek(0, 0); err != nil {
		return err
	}
	if err := c.file.Truncate(0); err != nil {
		return err
	}

	c.w = csv.NewWriter(c.file)
	if err := c.w.Write(c.headers); err != nil {
		return err
	}

	for _, key := range c.order {
		if err := c.w.Write(c.rows[key]); err != nil {
			return err
		}
	}

	c.w.Flush()

	return c.w.Error()
}

// Run implements scrapemate.ResultWriter.
func (c *columnWriter) Run(_ context.Context, in <-chan scrapemate.Result) error {
	for result := range in {
		elements, err := getCsvCapable(result.Data)
		if err != nil {
			return err
		}

		if len(elements) == 0 {
			continue
		}

		c.mu.Lock()

		c.headerOnce.Do(func() {
			rawHeaders := elements[0].CsvHeaders()
			c.colIdx, c.headers = c.filterRow(rawHeaders, rawHeaders)
			c.wroteHead = true
			if c.file == nil {
				_ = c.w.Write(c.headers)
			}
		})

		for _, element := range elements {
			raw := element.CsvRow()
			filtered := make([]string, 0, len(c.colIdx))
			for _, i := range c.colIdx {
				if i < len(raw) {
					filtered = append(filtered, raw[i])
				} else {
					filtered = append(filtered, "")
				}
			}

			key := rowKey(c.headers, filtered)
			if prev, ok := c.rows[key]; ok {
				c.rows[key] = mergeRow(prev, filtered)
			} else {
				c.rows[key] = filtered
				c.order = append(c.order, key)
			}
		}

		if c.file != nil {
			err = c.rewriteLocked()
		} else {
			// 无文件句柄时退化为追加（可能重复行）
			for _, element := range elements {
				raw := element.CsvRow()
				filtered := make([]string, 0, len(c.colIdx))
				for _, i := range c.colIdx {
					if i < len(raw) {
						filtered = append(filtered, raw[i])
					} else {
						filtered = append(filtered, "")
					}
				}
				if err = c.w.Write(filtered); err != nil {
					c.mu.Unlock()
					return err
				}
			}
			c.w.Flush()
			err = c.w.Error()
		}

		c.mu.Unlock()

		if err != nil {
			return err
		}
	}

	return nil
}

// getCsvCapable mirrors csvwriter 的类型展开逻辑（单值或切片）。
func getCsvCapable(data any) ([]scrapemate.CsvCapable, error) {
	var elements []scrapemate.CsvCapable

	if interfaceIsSlice(data) {
		s := reflect.ValueOf(data)

		for i := 0; i < s.Len(); i++ {
			val := s.Index(i).Interface()
			if element, ok := val.(scrapemate.CsvCapable); ok {
				elements = append(elements, element)
			} else {
				return nil, fmt.Errorf("%w: unexpected data type: %T", scrapemate.ErrorNotCsvCapable, val)
			}
		}
	} else if element, ok := data.(scrapemate.CsvCapable); ok {
		elements = append(elements, element)
	}

	return elements, nil
}

func interfaceIsSlice(data any) bool {
	if data == nil {
		return false
	}

	k := reflect.TypeOf(data).Kind()

	return k == reflect.Slice || k == reflect.Array
}
