package webrunner

import (
	"context"
	"encoding/csv"
	"fmt"
	"reflect"
	"sync"

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
	"review_rating", "review_count", "website",
}

// columnWriter 按任务配置过滤 CSV 输出列：
//   - 快速模式：必要列 + 内部强制列
//   - 深度/网格模式：默认全部列；用户选了 columns 时 = 选中列 + 内部强制列
type columnWriter struct {
	w          *csv.Writer
	selected   map[string]bool // nil 表示全部列
	wroteHead  bool
	headerOnce sync.Once
}

func newColumnWriter(w *csv.Writer, fastMode bool, columnsCSV string) *columnWriter {
	cw := &columnWriter{w: w}

	if columnsCSV != "" {
		// 用户自选列（深度模式）
		cw.selected = map[string]bool{}
		for _, c := range splitCSV(columnsCSV) {
			cw.selected[c] = true
		}
		for c := range forcedColumns {
			cw.selected[c] = true
		}
	} else if fastMode {
		// 快速模式：只要必要列
		cw.selected = map[string]bool{}
		for _, c := range essentialColumns {
			cw.selected[c] = true
		}
		for c := range forcedColumns {
			cw.selected[c] = true
		}
	}

	// columnsCSV 为空且非快速模式：selected = nil → 输出全部列
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

// Run implements scrapemate.ResultWriter.
func (c *columnWriter) Run(_ context.Context, in <-chan scrapemate.Result) error {
	var colIdx []int

	for result := range in {
		elements, err := getCsvCapable(result.Data)
		if err != nil {
			return err
		}

		if len(elements) == 0 {
			continue
		}

		c.headerOnce.Do(func() {
			headers := elements[0].CsvHeaders()
			var head []string
			colIdx, head = c.filterRow(headers, headers)
			_ = c.w.Write(head)
		})

		for _, element := range elements {
			row := element.CsvRow()

			filtered := make([]string, 0, len(colIdx))
			for _, i := range colIdx {
				if i < len(row) {
					filtered = append(filtered, row[i])
				} else {
					filtered = append(filtered, "")
				}
			}

			if err := c.w.Write(filtered); err != nil {
				return err
			}
		}

		c.w.Flush()
	}

	return c.w.Error()
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
