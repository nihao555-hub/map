package engine

import (
	"fmt"
	"strings"
	"unicode"
	"unicode/utf8"
)

const (
	minKeywordRunes        = 2
	minPreciseKeywordRunes = 3
	maxKeywordRunes        = 64
)

var weakKeywords = map[string]struct{}{
	"的": {}, "了": {}, "吗": {}, "呢": {}, "啊": {}, "吧": {}, "是": {},
	"a": {}, "an": {}, "the": {}, "and": {}, "or": {}, "i": {}, "to": {},
	"搜索": {}, "客户": {}, "获客": {}, "test": {}, "aaa": {},
	"你好": {}, "hello": {}, "hi": {}, "ok": {},
}

// NormalizeKeyword trims control / zero-width characters and collapses whitespace.
func NormalizeKeyword(s string) string {
	s = strings.Map(func(r rune) rune {
		switch r {
		case '\u200b', '\u200c', '\u200d', '\ufeff':
			return -1
		case '\u00a0':
			return ' '
		}
		if unicode.IsControl(r) {
			return -1
		}

		return r
	}, s)

	return strings.Join(strings.Fields(strings.TrimSpace(s)), " ")
}

// ValidateKeyword rejects empty, tiny, symbol-only, or placeholder queries.
// When precise is true the keyword must be at least 3 runes (外贸通「精确」).
func ValidateKeyword(s string, precise bool) error {
	s = NormalizeKeyword(s)
	if s == "" {
		return fmt.Errorf("请输入商品或企业名称")
	}

	n := utf8.RuneCountInString(s)
	minN := minKeywordRunes
	if precise {
		minN = minPreciseKeywordRunes
	}
	if n < minN {
		if precise {
			return fmt.Errorf("精确搜索请输入至少 3 个字，例如「配电柜」")
		}

		return fmt.Errorf("关键词至少 2 个字，例如「配电柜」")
	}
	if n > maxKeywordRunes {
		return fmt.Errorf("关键词过长，请缩短到 64 个字以内")
	}
	if !hasSearchableRune(s) {
		return fmt.Errorf("请输入商品或企业名称，不要只填符号")
	}
	if digitsOnly(s) {
		return fmt.Errorf("请输入商品或企业名称，不要只填数字")
	}
	if repeatedRune(s) {
		return fmt.Errorf("请输入更具体的商品或企业名称，例如「电动工具」")
	}
	if _, weak := weakKeywords[strings.ToLower(s)]; weak {
		return fmt.Errorf("请输入更具体的商品或企业名称，例如「电动工具」")
	}

	low := strings.ToLower(s)
	if strings.ContainsAny(s, "<>") ||
		strings.Contains(low, "javascript:") ||
		strings.Contains(low, "data:text") ||
		strings.Contains(low, "<script") ||
		strings.Contains(low, "127.0.0.1") ||
		strings.Contains(low, "localhost") {
		return fmt.Errorf("不支持该关键词")
	}

	return nil
}

func hasSearchableRune(s string) bool {
	for _, r := range s {
		if unicode.IsLetter(r) || unicode.Is(unicode.Han, r) {
			return true
		}
	}

	return false
}

func digitsOnly(s string) bool {
	saw := false
	for _, r := range s {
		if unicode.IsSpace(r) {
			continue
		}
		if !unicode.IsDigit(r) {
			return false
		}
		saw = true
	}

	return saw
}

func repeatedRune(s string) bool {
	var first rune
	n := 0
	for _, r := range s {
		if unicode.IsSpace(r) {
			continue
		}
		if n == 0 {
			first = r
		} else if r != first {
			return false
		}
		n++
	}

	return n >= 2
}
