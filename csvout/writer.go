// Package csvout writes scrapemate results as CSV. On top of the plain writer
// it can restrict the output to a chosen set of columns and append the
// background-research columns produced by company research.
package csvout

import (
	"context"
	"encoding/csv"
	"fmt"
	"reflect"
	"sync"

	"github.com/gosom/scrapemate"
)

// ResearchCsvCapable is implemented by results that carry background-research
// columns in addition to their base columns.
type ResearchCsvCapable interface {
	ResearchCsvRow() []string
}

// Option configures a Writer.
type Option func(*Writer)

// Writer implements scrapemate.ResultWriter.
type Writer struct {
	w *csv.Writer

	// selected restricts the output columns; nil means every column.
	selected map[string]bool
	// researchHeaders is non-empty when research columns should be appended.
	researchHeaders []string

	headerOnce sync.Once
	// columnIdx maps output position to source-row index, resolved once from
	// the first result's headers.
	columnIdx []int
}

var _ scrapemate.ResultWriter = (*Writer)(nil)

// New creates a CSV writer.
func New(w *csv.Writer, opts ...Option) *Writer {
	writer := &Writer{w: w}

	for _, opt := range opts {
		opt(writer)
	}

	return writer
}

// WithColumns restricts the output to the named columns. An empty set is
// ignored, which keeps "no selection" meaning "every column".
func WithColumns(columns map[string]bool) Option {
	return func(writer *Writer) {
		if len(columns) == 0 {
			return
		}

		writer.selected = columns
	}
}

// WithResearchColumns appends the given background-research columns, whose
// values come from ResearchCsvCapable.
func WithResearchColumns(headers []string) Option {
	return func(writer *Writer) {
		writer.researchHeaders = headers
	}
}

// Run implements scrapemate.ResultWriter.
func (writer *Writer) Run(_ context.Context, in <-chan scrapemate.Result) error {
	for result := range in {
		elements, err := csvCapableElements(result.Data)
		if err != nil {
			return err
		}

		if len(elements) == 0 {
			continue
		}

		writer.headerOnce.Do(func() {
			headers := writer.compose(elements[0].CsvHeaders(), writer.researchHeaders)

			writer.columnIdx = writer.selectIndexes(headers)

			_ = writer.w.Write(writer.pick(headers))
		})

		for _, element := range elements {
			row := writer.compose(element.CsvRow(), researchRow(element, len(writer.researchHeaders)))

			if err := writer.w.Write(writer.pick(row)); err != nil {
				return err
			}
		}

		writer.w.Flush()
	}

	return writer.w.Error()
}

// compose concatenates base and research values, keeping the research block at
// a fixed width so headers and rows stay aligned.
func (writer *Writer) compose(base, research []string) []string {
	if len(writer.researchHeaders) == 0 {
		return base
	}

	out := make([]string, 0, len(base)+len(writer.researchHeaders))
	out = append(out, base...)

	for i := range writer.researchHeaders {
		if i < len(research) {
			out = append(out, research[i])
		} else {
			out = append(out, "")
		}
	}

	return out
}

func (writer *Writer) selectIndexes(headers []string) []int {
	if writer.selected == nil {
		return nil
	}

	idx := make([]int, 0, len(headers))

	for i, header := range headers {
		if writer.selected[header] {
			idx = append(idx, i)
		}
	}

	return idx
}

func (writer *Writer) pick(row []string) []string {
	if writer.columnIdx == nil {
		return row
	}

	out := make([]string, 0, len(writer.columnIdx))

	for _, i := range writer.columnIdx {
		if i < len(row) {
			out = append(out, row[i])
		} else {
			out = append(out, "")
		}
	}

	return out
}

func researchRow(element scrapemate.CsvCapable, width int) []string {
	if width == 0 {
		return nil
	}

	if capable, ok := element.(ResearchCsvCapable); ok {
		return capable.ResearchCsvRow()
	}

	return nil
}

// csvCapableElements unwraps a result payload that is either a single
// CsvCapable or a slice of them.
func csvCapableElements(data any) ([]scrapemate.CsvCapable, error) {
	if data == nil {
		return nil, nil
	}

	if !isSlice(data) {
		element, ok := data.(scrapemate.CsvCapable)
		if !ok {
			return nil, fmt.Errorf("%w: unexpected data type: %T", scrapemate.ErrorNotCsvCapable, data)
		}

		return []scrapemate.CsvCapable{element}, nil
	}

	value := reflect.ValueOf(data)
	elements := make([]scrapemate.CsvCapable, 0, value.Len())

	for i := 0; i < value.Len(); i++ {
		item := value.Index(i).Interface()

		element, ok := item.(scrapemate.CsvCapable)
		if !ok {
			return nil, fmt.Errorf("%w: unexpected data type: %T", scrapemate.ErrorNotCsvCapable, item)
		}

		elements = append(elements, element)
	}

	return elements, nil
}

func isSlice(data any) bool {
	kind := reflect.TypeOf(data).Kind()

	return kind == reflect.Slice || kind == reflect.Array
}
