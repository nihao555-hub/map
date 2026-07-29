package web_test

import (
	"reflect"
	"testing"

	"github.com/gosom/google-maps-scraper/web"
)

func TestExpandBatchKeywords(t *testing.T) {
	tests := []struct {
		name      string
		types     []string
		locations []string
		want      []string
	}{
		{
			name:      "cartesian product",
			types:     []string{"咖啡店, 餐厅"},
			locations: []string{"深圳南山、广州天河"},
			want:      []string{"咖啡店 in 深圳南山", "咖啡店 in 广州天河", "餐厅 in 深圳南山", "餐厅 in 广州天河"},
		},
		{
			name:      "types without locations",
			types:     []string{"hotel"},
			locations: nil,
			want:      []string{"hotel"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := web.ExpandBatchKeywords(tt.types, tt.locations); !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("ExpandBatchKeywords() = %v, want %v", got, tt.want)
			}
		})
	}
}
