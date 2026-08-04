package web

import (
	"regexp"
	"strings"
	"unicode"
)

// 决策人闸门：只有能当真人看待的条目才保留姓名。
//
// 公网实测（雅加达批发商 354 家）暴露四类噪音，全部由这里拦掉：
//  1. 门店邮箱前缀被拼成人名：siljlpanjangkpbaru.jkt@… → "Siljlpanjangkpbaru Jkt"
//  2. 网页句子片段被当人名："usaha bisnis" / "CIO dan" / "related Internal control"
//  3. 模板占位人名："Aston Doe" / "John Doe"
//  4. 注册商与职能邮箱冒充决策人
//
// 拦不掉真人是更大的损失，所以判定只依据结构与词表，不做语义猜测。

// personNameTokenRe 单个姓名词：字母开头，可含撇号/连字符，允许缩写点结尾。
var personNameTokenRe = regexp.MustCompile(`^\p{L}[\p{L}'’\-.]*$`)

// nonPersonTokens 出现即判定不是人名的词（职能、部门、句子连接、地区/门店代号）。
var nonPersonTokens = map[string]bool{
	// 句子片段常见词
	"dan": true, "related": true, "internal": true, "control": true,
	"usaha": true, "bisnis": true, "our": true, "meet": true, "team": true,
	"the": true, "and": true, "for": true, "with": true, "from": true,
	"about": true, "more": true, "read": true, "view": true, "click": true,
	"latest": true, "blogs": true, "blog": true, "news": true, "article": true,
	"seputar": true, "tentang": true, "kami": true, "hubungi": true,
	// 职能/部门
	"ceo": true, "cfo": true, "cto": true, "coo": true, "cio": true, "cmo": true,
	"director": true, "direktur": true, "manager": true, "owner": true,
	"pemilik": true, "founder": true, "pendiri": true, "staff": true,
	"marketing": true, "sales": true, "purchasing": true, "procurement": true,
	"admin": true, "contact": true, "kontak": true, "support": true,
	"service": true, "customer": true, "finance": true, "account": true,
	"executive": true, "officer": true, "chief": true, "head": true,
	"department": true, "division": true, "office": true, "branch": true,
	"company": true, "corporate": true, "group": true, "holding": true,
	// 职称/组织角色被网页抽成「姓名」：Senior GM / Wakil Presiden / Investor Relations
	"senior": true, "junior": true, "gm": true, "vp": true, "svp": true, "evp": true,
	"president": true, "presiden": true, "wakil": true, "investor": true,
	"relation": true, "relations": true, "ir": true, "board": true,
	"komisaris": true, "commissioner": true, "secretary": true, "sekretaris": true,
	"chairman": true, "chairwoman": true, "chair": true, "ketua": true,
	// 主体后缀
	"pt": true, "cv": true, "ud": true, "tbk": true, "inc": true, "ltd": true,
	"llc": true, "gmbh": true, "bv": true, "sdn": true, "bhd": true,
	// 印尼门店/地区代号（邮箱前缀常带）
	"jkt": true, "tgr": true, "bks": true, "bdg": true, "sby": true, "dps": true,
	"jabodetabek": true, "pusat": true, "utara": true, "selatan": true,
	"timur": true, "barat": true, "cabang": true, "toko": true, "grosir": true,
	"store": true, "shop": true, "outlet": true, "indonesia": true,
	// 业务描述词（"Erinda Supplier" / "Domain Operations" 一类不是人）
	"supplier": true, "pemasok": true, "distributor": true, "wholesale": true,
	"trading": true, "domain": true, "operation": true, "operations": true,
	"seafood": true, "birdnest": true, "logistics": true, "export": true,
	"import": true, "importer": true, "exporter": true,
	// 模板占位
	"doe": true, "lorem": true, "ipsum": true, "foo": true, "bar": true,
	"test": true, "sample": true, "example": true, "dummy": true,
	"nama": true, "name": true, "namamu": true,
	// 安全/滥用告示（"Rtr Security Threats" 一类来自注册商与安全页）
	"security": true, "threat": true, "threats": true, "abuse": true,
	"phishing": true, "malware": true, "spam": true, "report": true,
	"reports": true, "notice": true, "alert": true, "alerts": true,
	"incident": true, "vulnerability": true, "rtr": true, "policy": true,
	"privacy": true, "terms": true, "cookie": true, "cookies": true,
}

// templateFullNames 整体即模板占位的人名。
var templateFullNames = map[string]bool{
	"john doe": true, "jane doe": true, "john smith": true,
	"aston doe": true, "nama lengkap": true, "full name": true,
}

// maxPersonNameToken 单词过长基本是把标识符/门店代号当成了名字。
const maxPersonNameToken = 14

// IsValidPersonName 判断这串文本能否作为真人姓名对外展示。
func IsValidPersonName(name string) bool {
	name = strings.TrimSpace(name)
	if name == "" || strings.ContainsAny(name, "@0123456789/\\|<>(){}[]") {
		return false
	}

	if templateFullNames[strings.ToLower(name)] {
		return false
	}

	parts := strings.Fields(name)
	if len(parts) < 2 || len(parts) > 4 {
		return false
	}

	upperStarts := 0

	for _, raw := range parts {
		token := strings.Trim(raw, ".,;:\"'")
		if token == "" || !personNameTokenRe.MatchString(token) {
			return false
		}

		low := strings.ToLower(token)
		if nonPersonTokens[low] {
			return false
		}

		runes := []rune(token)
		// 单字母首字母缩写常见（F. Tirto / Enristia P），但必须大写。
		if len(runes) < 2 && !unicode.IsUpper(runes[0]) {
			return false
		}

		// 超长单词基本是把门店代号/标识符当成了名字。
		if len(runes) > maxPersonNameToken {
			return false
		}

		if unicode.IsUpper(runes[0]) {
			upperStarts++
		}
	}

	// 真人名各词基本首字母大写；全小写多为句子片段。
	return upperStarts == len(parts)
}

// nameNormalizedForEmailMatch 只留小写字母，用于和邮箱前缀比对。
func nameNormalizedForEmailMatch(s string) string {
	var b strings.Builder

	for _, r := range strings.ToLower(s) {
		if unicode.IsLetter(r) {
			b.WriteRune(r)
		}
	}

	return b.String()
}

// NameDerivedFromEmail 判断姓名是否只来自邮箱前缀。
//
// 这不代表噪音：firstname.lastname@company 本身就是真人的强证据（Hunter 一类正是靠它）。
// 但「只有邮箱佐证」弱于团队页/公告里的署名，所以用它压低单人置信度，而不是直接丢弃。
func NameDerivedFromEmail(name, email string) bool {
	if name == "" || email == "" {
		return false
	}

	local, _, ok := strings.Cut(email, "@")
	if !ok {
		return false
	}

	n := nameNormalizedForEmailMatch(name)
	l := nameNormalizedForEmailMatch(local)

	if n == "" || l == "" {
		return false
	}

	return n == l || strings.Contains(l, n) || strings.Contains(n, l)
}

// parentheticalRe 取出店名里的括注内容，如 "Distributor OSB Jakarta (Siti Hodijah)"。
var parentheticalRe = regexp.MustCompile(`[（(]([^）)]{2,60})[）)]`)

// ownerNameInBusinessTitle 判断姓名是否是店名括注里标出的店主。
//
// 印尼 Maps 上「店名 (店主名)」很常见，这是真实店主，不能当成店名回声丢掉。
func ownerNameInBusinessTitle(name, business string) bool {
	target := nameNormalizedForEmailMatch(name)
	if target == "" {
		return false
	}

	for _, m := range parentheticalRe.FindAllStringSubmatch(business, -1) {
		inner := strings.TrimSpace(m[1])
		if nameNormalizedForEmailMatch(inner) == target && IsValidPersonName(inner) {
			return true
		}
	}

	return false
}

// NameEchoesBusiness 判断姓名只是商家名（或其片段）。
func NameEchoesBusiness(name, business string) bool {
	n := nameNormalizedForEmailMatch(name)
	b := nameNormalizedForEmailMatch(business)

	if n == "" {
		return true
	}

	if b == "" {
		return false
	}

	// 括注写明的店主是真人，不算回声
	if ownerNameInBusinessTitle(name, business) {
		return false
	}

	return n == b || strings.Contains(b, n) || strings.Contains(n, b)
}

// roleTitleWords 判断职称是否描述了岗位（用于决定退成渠道时是否保留原职称）。
var roleTitleWords = []string{
	"ceo", "cfo", "cto", "coo", "cio", "founder", "owner", "pemilik",
	"director", "direktur", "manager", "head", "chief", "president",
	"komisaris", "purchasing", "procurement", "buyer", "sales", "export",
	"general manager", "gm", "supply chain",
}

// isRoleTitle 判断职称是否为岗位描述而非「Contact」这类占位。
func isRoleTitle(title string) bool {
	low := strings.ToLower(strings.TrimSpace(title))
	if low == "" {
		return false
	}

	for _, w := range roleTitleWords {
		if strings.Contains(low, w) {
			return true
		}
	}

	return false
}

// channelTitleFor 给无名条目一个说明渠道来源的标签。
func channelTitleFor(d DecisionMaker) string {
	switch {
	case d.Email != "":
		return "Email channel"
	case d.WhatsApp != "":
		return "WhatsApp channel"
	case d.Phone != "":
		return "Phone channel"
	case d.LinkedIn != "":
		return "LinkedIn channel"
	default:
		return "Business contact"
	}
}

// nonDecisionTitles 这些岗位即便是真人也不是采购/经营决策人，列进名单只会误导销售。
var nonDecisionTitles = []string{
	"content author", "blog", "editor", "photographer", "webmaster",
	"intern", "student", "volunteer", "reviewer", "author",
}

// isNonDecisionTitle 判断职称是否与采购决策无关。
func isNonDecisionTitle(title string) bool {
	low := strings.ToLower(strings.TrimSpace(title))
	if low == "" {
		return false
	}

	for _, t := range nonDecisionTitles {
		if strings.Contains(low, t) {
			return true
		}
	}

	return false
}

// isPersonOrgNode 判断架构节点是否是以真人命名的岗位节点。
func isPersonOrgNode(o OrgUnit) bool {
	return IsValidPersonName(o.Name)
}

// QualifiesAsDecisionMaker 决定这条记录能否作为「具名决策人」展示。
//
// 判定只看结构与来源：姓名必须像真人、不能是店名回声、不能来自注册商通道、
// 岗位不能是与采购无关的角色。门店邮箱前缀（siljlpanjangkpbaru.jkt）会因超长词
// 与地区代号在 IsValidPersonName 处被拦下。
func QualifiesAsDecisionMaker(d DecisionMaker, businessTitle string) bool {
	if !IsValidPersonName(d.Name) {
		return false
	}

	if NameEchoesBusiness(d.Name, businessTitle) {
		return false
	}

	if isNonDecisionTitle(d.Title) {
		return false
	}

	return !isRegistrarContact(d.Email)
}
