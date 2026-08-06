package web

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"
	"unicode"
)

// myMemoryTranslateURL 可在测试中替换
var myMemoryTranslateURL = "https://api.mymemory.translated.net/get"

// containsChinese 判断文本是否含汉字（用于决定是否需要译成目标国语言）
func containsChinese(s string) bool {
	for _, r := range s {
		if unicode.Is(unicode.Han, r) {
			return true
		}
	}

	return false
}

// zhBusinessLexicon 常见获客品类：中文 → 目标语言。
// 优先走词典，避免机器翻译把「美甲店」收成 NailSalon 这类难搜词。
var zhBusinessLexicon = map[string]map[string]string{
	"咖啡":   {"en": "coffee", "ja": "カフェ", "ko": "커피", "fr": "café", "de": "Café", "es": "café", "it": "caffè", "pt": "café", "th": "กาแฟ", "vi": "cà phê", "ru": "кофе", "ar": "قهوة", "nl": "koffie", "pl": "kawa", "sv": "kaffe", "tr": "kahve", "id": "kopi", "ms": "kopi", "uk": "кава"},
	"咖啡店":  {"en": "coffee shop", "ja": "カフェ", "ko": "커피숍", "fr": "café", "de": "Café", "es": "cafetería", "it": "caffetteria", "pt": "cafeteria", "th": "ร้านกาแฟ", "vi": "quán cà phê", "ru": "кофейня", "ar": "مقهى", "nl": "koffiezaak", "id": "kedai kopi", "ms": "kedai kopi"},
	"咖啡馆":  {"en": "cafe", "ja": "カフェ", "ko": "카페", "fr": "café", "de": "Café", "es": "café", "it": "caffè", "pt": "café", "th": "คาเฟ่", "vi": "quán cà phê", "ru": "кафе"},
	"咖啡厅":  {"en": "cafe", "ja": "カフェ", "ko": "카페", "fr": "café", "de": "Café", "es": "café", "it": "caffè", "pt": "café"},
	"拉面":   {"en": "ramen", "ja": "ラーメン", "ko": "라멘", "fr": "ramen", "de": "Ramen", "es": "ramen", "it": "ramen", "pt": "ramen"},
	"餐厅":   {"en": "restaurant", "ja": "レストラン", "ko": "레스토랑", "fr": "restaurant", "de": "Restaurant", "es": "restaurante", "it": "ristorante", "pt": "restaurante", "th": "ร้านอาหาร", "vi": "nhà hàng", "ru": "ресторан", "ar": "مطعم"},
	"饭店":   {"en": "restaurant", "ja": "レストラン", "ko": "식당", "fr": "restaurant", "de": "Restaurant", "es": "restaurante"},
	"美甲":   {"en": "nail salon", "ja": "ネイルサロン", "ko": "네일샵", "fr": "salon de manucure", "de": "Nagelstudio", "es": "salón de uñas", "it": "centro unghie", "pt": "salão de unhas"},
	"美甲店":  {"en": "nail salon", "ja": "ネイルサロン", "ko": "네일샵", "fr": "salon de manucure", "de": "Nagelstudio", "es": "salón de uñas"},
	"牙医":   {"en": "dentist", "ja": "歯科", "ko": "치과", "fr": "dentiste", "de": "Zahnarzt", "es": "dentista", "it": "dentista", "pt": "dentista", "th": "ทันตแพทย์", "vi": "nha sĩ", "ru": "стоматолог", "ar": "طبيب أسنان"},
	"牙科":   {"en": "dental clinic", "ja": "歯科クリニック", "ko": "치과", "fr": "cabinet dentaire", "de": "Zahnklinik", "es": "clínica dental"},
	"书店":   {"en": "bookstore", "ja": "本屋", "ko": "서점", "fr": "librairie", "de": "Buchhandlung", "es": "librería", "it": "libreria", "pt": "livraria"},
	"健身房":  {"en": "gym", "ja": "ジム", "ko": "헬스장", "fr": "salle de sport", "de": "Fitnessstudio", "es": "gimnasio", "it": "palestra", "pt": "academia"},
	"美容院":  {"en": "beauty salon", "ja": "美容室", "ko": "미용실", "fr": "salon de beauté", "de": "Schönheitssalon", "es": "salón de belleza"},
	"理发店":  {"en": "barbershop", "ja": "理髪店", "ko": "이발소", "fr": "coiffeur", "de": "Friseur", "es": "barbería"},
	"理发":   {"en": "hair salon", "ja": "美容院", "ko": "미용실", "fr": "coiffeur", "de": "Friseur"},
	"宠物店":  {"en": "pet store", "ja": "ペットショップ", "ko": "펫샵", "fr": "animalerie", "de": "Zoohandlung", "es": "tienda de mascotas"},
	"酒店":   {"en": "hotel", "ja": "ホテル", "ko": "호텔", "fr": "hôtel", "de": "Hotel", "es": "hotel", "it": "hotel", "pt": "hotel"},
	"宾馆":   {"en": "hotel", "ja": "ホテル", "ko": "호텔", "fr": "hôtel", "de": "Hotel"},
	"超市":   {"en": "supermarket", "ja": "スーパー", "ko": "슈퍼마켓", "fr": "supermarché", "de": "Supermarkt", "es": "supermercado"},
	"药店":   {"en": "pharmacy", "ja": "薬局", "ko": "약국", "fr": "pharmacie", "de": "Apotheke", "es": "farmacia"},
	"药房":   {"en": "pharmacy", "ja": "薬局", "ko": "약국", "fr": "pharmacie", "de": "Apotheke"},
	"医院":   {"en": "hospital", "ja": "病院", "ko": "병원", "fr": "hôpital", "de": "Krankenhaus", "es": "hospital"},
	"诊所":   {"en": "clinic", "ja": "クリニック", "ko": "클리닉", "fr": "clinique", "de": "Klinik", "es": "clínica"},
	"律师":   {"en": "lawyer", "ja": "弁護士", "ko": "변호사", "fr": "avocat", "de": "Anwalt", "es": "abogado"},
	"会计师":  {"en": "accountant", "ja": "会計士", "ko": "회계사", "fr": "comptable", "de": "Buchhalter", "es": "contador"},
	"房地产":  {"en": "real estate", "ja": "不動産", "ko": "부동산", "fr": "immobilier", "de": "Immobilien", "es": "bienes raíces"},
	"中介":   {"en": "real estate agency", "ja": "不動産会社", "ko": "부동산", "fr": "agence immobilière", "de": "Immobilienmakler"},
	"汽车维修": {"en": "auto repair", "ja": "自動車修理", "ko": "자동차 수리", "fr": "réparation auto", "de": "Autowerkstatt"},
	"修车":   {"en": "auto repair", "ja": "車の修理", "ko": "자동차 수리", "fr": "garage", "de": "Autowerkstatt"},
	"洗车":   {"en": "car wash", "ja": "洗車", "ko": "세차", "fr": "lavage auto", "de": "Autowäsche"},
	"花店":   {"en": "florist", "ja": "花屋", "ko": "꽃집", "fr": "fleuriste", "de": "Blumenladen", "es": "floristería"},
	"面包店":  {"en": "bakery", "ja": "パン屋", "ko": "빵집", "fr": "boulangerie", "de": "Bäckerei", "es": "panadería"},
	"蛋糕店":  {"en": "cake shop", "ja": "ケーキ屋", "ko": "케이크 가게", "fr": "pâtisserie", "de": "Konditorei"},
	"奶茶店":  {"en": "bubble tea", "ja": "タピオカ", "ko": "버블티", "fr": "bubble tea", "de": "Bubble Tea", "es": "té de burbujas"},
	"奶茶":   {"en": "bubble tea", "ja": "タピオカミルクティー", "ko": "버블티", "fr": "bubble tea"},
	"酒吧":   {"en": "bar", "ja": "バー", "ko": "바", "fr": "bar", "de": "Bar", "es": "bar", "it": "bar"},
	"夜店":   {"en": "nightclub", "ja": "ナイトクラブ", "ko": "나이트클럽", "fr": "boîte de nuit", "de": "Nachtclub"},
	"寿司":   {"en": "sushi", "ja": "寿司", "ko": "스시", "fr": "sushi", "de": "Sushi", "es": "sushi"},
	"披萨":   {"en": "pizza", "ja": "ピザ", "ko": "피자", "fr": "pizza", "de": "Pizza", "es": "pizza", "it": "pizza"},
	"火锅":   {"en": "hot pot", "ja": "火鍋", "ko": "훠궈", "fr": "fondue chinoise", "de": "Hot Pot", "es": "hot pot"},
	"烧烤":   {"en": "bbq", "ja": "焼肉", "ko": "바베큐", "fr": "barbecue", "de": "Grill", "es": "barbacoa"},
	"按摩":   {"en": "massage", "ja": "マッサージ", "ko": "마사지", "fr": "massage", "de": "Massage", "es": "masaje", "th": "นวด"},
	"spa":  {"en": "spa", "ja": "スパ", "ko": "스파", "fr": "spa", "de": "Spa"},
	"瑜伽":   {"en": "yoga", "ja": "ヨガ", "ko": "요가", "fr": "yoga", "de": "Yoga", "es": "yoga"},
	"幼儿园":  {"en": "kindergarten", "ja": "幼稚園", "ko": "유치원", "fr": "jardin d'enfants", "de": "Kindergarten"},
	"学校":   {"en": "school", "ja": "学校", "ko": "학교", "fr": "école", "de": "Schule", "es": "escuela"},
	"培训":   {"en": "tutoring", "ja": "塾", "ko": "학원", "fr": "cours particuliers", "de": "Nachhilfe"},
	"物流":   {"en": "logistics", "ja": "物流", "ko": "물류", "fr": "logistique", "de": "Logistik"},
	"快递":   {"en": "courier", "ja": "宅配", "ko": "택배", "fr": "coursier", "de": "Kurier"},
	"打印":   {"en": "print shop", "ja": "印刷所", "ko": "인쇄소", "fr": "imprimerie", "de": "Druckerei"},
	"照相馆":  {"en": "photo studio", "ja": "写真館", "ko": "사진관", "fr": "studio photo", "de": "Fotostudio"},
	"干洗":   {"en": "dry cleaner", "ja": "クリーニング", "ko": "세탁소", "fr": "pressing", "de": "Reinigung"},
	"洗衣店":  {"en": "laundry", "ja": "コインランドリー", "ko": "세탁소", "fr": "laverie", "de": "Wäscherei"},
	// B2B / 外贸获客：中文品类在海外 Maps 几乎搜不到，必须落到英文/当地词
	"采购商":  {"en": "importer", "id": "importir", "ms": "pengimport", "th": "ผู้นำเข้า", "vi": "nhà nhập khẩu", "ja": "輸入業者", "ko": "수입상", "fr": "importateur", "de": "Importeur", "es": "importador", "pt": "importador", "ar": "مستورد", "ru": "импортер"},
	"买家":   {"en": "buyer", "id": "pembeli", "ms": "pembeli", "th": "ผู้ซื้อ", "vi": "người mua", "ja": "バイヤー", "ko": "바이어", "fr": "acheteur", "de": "Käufer", "es": "comprador"},
	"进口商":  {"en": "importer", "id": "importir", "ms": "pengimport", "th": "ผู้นำเข้า", "vi": "nhà nhập khẩu", "ja": "輸入業者", "ko": "수입상", "fr": "importateur", "de": "Importeur", "es": "importador"},
	"出口商":  {"en": "exporter", "id": "eksportir", "ms": "pengeksport", "th": "ผู้ส่งออก", "vi": "nhà xuất khẩu", "ja": "輸出業者", "ko": "수출상", "fr": "exportateur", "de": "Exporteur", "es": "exportador"},
	"批发商":  {"en": "wholesaler", "id": "grosir", "ms": "pemborong", "th": "ร้านขายส่ง", "vi": "nhà bán buôn", "ja": "卸売業者", "ko": "도매상", "fr": "grossiste", "de": "Großhändler", "es": "mayorista"},
	"批发":   {"en": "wholesale", "id": "grosir", "ms": "borong", "th": "ขายส่ง", "vi": "bán buôn", "ja": "卸売", "ko": "도매"},
	"供应商":  {"en": "supplier", "id": "pemasok", "ms": "pembekal", "th": "ซัพพลายเออร์", "vi": "nhà cung cấp", "ja": "サプライヤー", "ko": "공급업체", "fr": "fournisseur", "de": "Lieferant", "es": "proveedor"},
	"经销商":  {"en": "distributor", "id": "distributor", "ms": "pengedar", "th": "ตัวแทนจำหน่าย", "vi": "nhà phân phối", "ja": "販売代理店", "ko": "유통업체", "fr": "distributeur", "de": "Händler", "es": "distribuidor"},
	"贸易公司": {"en": "trading company", "id": "perusahaan dagang", "ms": "syarikat perdagangan", "th": "บริษัทการค้า", "vi": "công ty thương mại", "ja": "商社", "ko": "무역회사", "fr": "société de trading", "de": "Handelsunternehmen"},
	"贸易":   {"en": "trading company", "id": "perusahaan dagang", "ms": "syarikat perdagangan", "ja": "商社", "ko": "무역"},
	"工厂":   {"en": "factory", "id": "pabrik", "ms": "kilang", "th": "โรงงาน", "vi": "nhà máy", "ja": "工場", "ko": "공장", "fr": "usine", "de": "Fabrik", "es": "fábrica"},
	"制造商":  {"en": "manufacturer", "id": "produsen", "ms": "pengilang", "th": "ผู้ผลิต", "vi": "nhà sản xuất", "ja": "メーカー", "ko": "제조사", "fr": "fabricant", "de": "Hersteller"},
	"生产厂家": {"en": "manufacturer", "id": "produsen", "ms": "pengilang", "ja": "メーカー", "ko": "제조사"},
	// 工业电气 / 配电（海外 Maps 用当地/英文工业词，中文几乎搜不到）
	"配电":    {"en": "switchgear", "id": "panel listrik", "ms": "suis elektrik"},
	"配电柜":   {"en": "switchgear", "id": "panel listrik", "ms": "panel elektrik"},
	"配电箱":   {"en": "electrical panel", "id": "panel listrik", "ms": "panel elektrik"},
	"开关柜":   {"en": "switchgear", "id": "switchgear", "ms": "switchgear"},
	"电气":    {"en": "electrical", "id": "listrik", "ms": "elektrik"},
	"电气设备":  {"en": "electrical equipment", "id": "peralatan listrik", "ms": "peralatan elektrik"},
	"电气经销商": {"en": "electrical distributor", "id": "distributor listrik", "ms": "pengedar elektrik"},
	"电力设备":  {"en": "power equipment", "id": "peralatan listrik", "ms": "peralatan kuasa"},
	"变压器":   {"en": "transformer", "id": "trafo", "ms": "transformer"},
	"电缆":    {"en": "cable", "id": "kabel listrik", "ms": "kabel"},
	"商家":    {"en": "business", "id": "bisnis", "ms": "perniagaan", "th": "ธุรกิจ", "vi": "doanh nghiệp", "ja": "事業者", "ko": "업체"},
	"公司":    {"en": "company", "id": "perusahaan", "ms": "syarikat", "th": "บริษัท", "vi": "công ty", "ja": "会社", "ko": "회사"},
}

// zhPlaceLexicon 常见中文地名 → Google Maps 更友好的英文/当地写法
var zhPlaceLexicon = map[string]string{
	"纽约":    "New York",
	"纽约市":   "New York",
	"纽约曼哈顿": "Manhattan, New York",
	"曼哈顿":   "Manhattan, New York",
	"布鲁克林":  "Brooklyn, New York",
	"洛杉矶":   "Los Angeles",
	"旧金山":   "San Francisco",
	"芝加哥":   "Chicago",
	"西雅图":   "Seattle",
	"波士顿":   "Boston",
	"迈阿密":   "Miami",
	"华盛顿":   "Washington, DC",
	"拉斯维加斯": "Las Vegas",
	"休斯顿":   "Houston",
	"伦敦":    "London",
	"曼彻斯特":  "Manchester",
	"伯明翰":   "Birmingham",
	"巴黎":    "Paris",
	"柏林":    "Berlin",
	"慕尼黑":   "Munich",
	"法兰克福":  "Frankfurt",
	"罗马":    "Rome",
	"米兰":    "Milan",
	"马德里":   "Madrid",
	"巴塞罗那":  "Barcelona",
	"阿姆斯特丹": "Amsterdam",
	"悉尼":    "Sydney",
	"墨尔本":   "Melbourne",
	"布里斯班":  "Brisbane",
	"奥克兰":   "Auckland",
	"东京":    "Tokyo",
	"东京涩谷":  "Shibuya, Tokyo",
	"涩谷":    "Shibuya, Tokyo",
	"新宿":    "Shinjuku, Tokyo",
	"大阪":    "Osaka",
	"京都":    "Kyoto",
	"横滨":    "Yokohama",
	"名古屋":   "Nagoya",
	"首尔":    "Seoul",
	"釜山":    "Busan",
	"曼谷":    "Bangkok",
	"清迈":    "Chiang Mai",
	"新加坡":   "Singapore",
	"吉隆坡":   "Kuala Lumpur",
	"雅加达":   "Jakarta",
	"巴厘岛":   "Bali",
	"峇里岛":   "Bali",
	"泗水":    "Surabaya",
	"万隆":    "Bandung",
	"棉兰":    "Medan",
	"日惹":    "Yogyakarta",
	"河内":    "Hanoi",
	"胡志明市":  "Ho Chi Minh City",
	"迪拜":    "Dubai",
	"阿布扎比":  "Abu Dhabi",
	"多伦多":   "Toronto",
	"温哥华":   "Vancouver",
	"蒙特利尔":  "Montreal",
	"墨西哥城":  "Mexico City",
	"圣保罗":   "Sao Paulo",
	"里约热内卢": "Rio de Janeiro",
	"莫斯科":   "Moscow",
	"伊斯坦布尔": "Istanbul",
	"开罗":    "Cairo",
	"约翰内斯堡": "Johannesburg",
	"开普敦":   "Cape Town",
}

var camelSplitRE = regexp.MustCompile(`([a-z])([A-Z])`)

// localizeOpts 控制是否走 AI 翻译
type localizeOpts struct {
	CountryName string // 目标国家英文/中文名，给 AI 提示用
	UseAI       bool   // 用户勾选且服务端已配置 GRSAI_API_KEY
	// SkipPlaceHint omits " in {city}" from keywords. Use when the job already
	// has a geocoded Lat/Lon / grid pin — appending the city string makes Maps
	// ignore peripheral grid cells and collapses recall on large metros.
	SkipPlaceHint bool
}

// localizeSearchQuery 把中文「找什么 / 在哪里」转成目标国可搜的查询。
// 优先级：AI（勾选且已配置）→ 业务词典 → MyMemory。
// 海外任务绝不以汉字进 Google Maps。
// 返回：用于 Google Maps 的关键词列表、搜索用地名、是否发生了翻译。
func localizeSearchQuery(ctx context.Context, rawKeywords []string, locations string, targetLang string, opts ...localizeOpts) (keywords []string, searchLocation string, translated bool) {
	targetLang = strings.ToLower(strings.TrimSpace(targetLang))
	if targetLang == "" {
		targetLang = "en"
	}

	var opt localizeOpts
	if len(opts) > 0 {
		opt = opts[0]
	}
	useAI := opt.UseAI && AITranslateEnabled()

	searchLocation = strings.TrimSpace(locations)
	if searchLocation != "" && containsChinese(searchLocation) && targetLang != "zh" {
		// 地名：词典优先，再 AI / 机翻落到英文（Maps 对英文地名最稳）
		if loc, ok := zhPlaceLexicon[searchLocation]; ok {
			searchLocation = loc
			translated = true
		} else if loc, ok := fuzzyPlaceLexicon(searchLocation); ok {
			searchLocation = loc
			translated = true
		}
		if containsChinese(searchLocation) && useAI {
			if name, err := AITranslateKeyword(ctx, searchLocation, opt.CountryName, "en"); err == nil && name != "" {
				searchLocation = name
				translated = true
			}
		}
		if containsChinese(searchLocation) {
			if name, err := translateText(ctx, searchLocation, "zh", "en"); err == nil && name != "" && !containsChinese(name) {
				searchLocation = name
				translated = true
			}
		}
	}

	for _, k := range rawKeywords {
		k = strings.TrimSpace(k)
		if k == "" {
			continue
		}

		searchK := k
		if containsChinese(k) && targetLang != "zh" {
			// 品类：词典优先（毫秒级），未命中再走 AI / 机翻
			if t, ok := translateBusinessTerm(k, targetLang); ok {
				searchK = t
				translated = true
			}
			if containsChinese(searchK) && useAI {
				if t, err := AITranslateKeyword(ctx, k, opt.CountryName, targetLang); err == nil && t != "" {
					searchK = t
					translated = true
				}
			}
			if containsChinese(searchK) {
				if t, err := translateText(ctx, k, "zh", "en"); err == nil && t != "" && !containsChinese(t) {
					searchK = normalizeMT(t)
					translated = true
				}
			}
		}

		// 仍含汉字则不要拼进海外查询（质量会直接崩）
		if containsChinese(searchK) && targetLang != "zh" {
			continue
		}

		if !opt.SkipPlaceHint {
			if placeHint := mapsPlaceHint(searchLocation, opt.CountryName); placeHint != "" {
				searchK = searchK + " in " + placeHint
			}
		}

		keywords = append(keywords, searchK)
	}

	// 全部被跳过：再试一次 AI（若开启）/ 词典英文；仍含汉字则丢弃
	if len(keywords) == 0 && len(rawKeywords) > 0 && targetLang != "zh" {
		k := strings.TrimSpace(rawKeywords[0])
		fallback := ""
		if useAI {
			if t, err := AITranslateKeyword(ctx, k, opt.CountryName, targetLang); err == nil && t != "" && !containsChinese(t) {
				fallback = t
			}
		}
		if fallback == "" {
			if t, ok := translateBusinessTerm(k, "en"); ok {
				fallback = t
			} else if t, err := translateText(ctx, k, "zh", "en"); err == nil && t != "" && !containsChinese(t) {
				fallback = normalizeMT(t)
			}
		}
		if fallback != "" && !containsChinese(fallback) {
			if !opt.SkipPlaceHint {
				if placeHint := mapsPlaceHint(searchLocation, opt.CountryName); placeHint != "" {
					fallback = fallback + " in " + placeHint
				}
			}
			keywords = append(keywords, fallback)
			translated = true
		}
	}

	return keywords, searchLocation, translated
}

// isLatLonLocation reports whether s is raw map coordinates (bad as Maps "in …" text).
func isLatLonLocation(s string) bool {
	s = strings.TrimSpace(s)
	if s == "" {
		return false
	}
	var lat, lon float64
	n, err := fmt.Sscanf(s, "%f,%f", &lat, &lon)
	if err != nil || n != 2 {
		n, err = fmt.Sscanf(s, "%f, %f", &lat, &lon)
	}
	if err != nil || n != 2 {
		return false
	}
	return lat >= -90 && lat <= 90 && lon >= -180 && lon <= 180
}

// mapsPlaceHint returns a human place name suitable for Google Maps queries.
// Raw coordinates are never appended (they poison ranking and yield few junk hits).
func mapsPlaceHint(location, countryName string) string {
	location = strings.TrimSpace(location)
	countryName = strings.TrimSpace(countryName)
	if location == "" || isLatLonLocation(location) {
		return countryName
	}
	// Overseas Chinese place names should already have been translated above;
	// if Chinese remains, prefer country English name over poisoning Maps with Han chars.
	if containsChinese(location) {
		if countryName != "" {
			return countryName
		}
		return location
	}
	return location
}

// fuzzyPlaceLexicon 处理输入法丢字（「约曼哈顿」←「纽约曼哈顿」）
func fuzzyPlaceLexicon(term string) (string, bool) {
	bestKey := ""
	bestScore := 0

	for key, en := range zhPlaceLexicon {
		if key == term {
			return en, true
		}
		if strings.Contains(key, term) || strings.Contains(term, key) {
			score := len([]rune(key))
			if strings.Contains(key, term) {
				score += 5
			}
			if score > bestScore {
				bestScore = score
				bestKey = key
			}
		}
	}

	if bestKey == "" {
		return "", false
	}

	return zhPlaceLexicon[bestKey], true
}

func translateBusinessTerm(term, targetLang string) (string, bool) {
	term = strings.TrimSpace(term)
	if term == "" {
		return "", false
	}

	pick := func(byLang map[string]string) (string, bool) {
		// A/B on Jakarta electrical (2026-08): local "panel listrik" ≈155 hits;
		// English "electrical distributor" ≈172 but broader (toko listrik);
		// English-only jargon "switchgear" ≈3 (too narrow). Prefer local Maps
		// phrases when the lexicon has them; fall back to English.
		if v, ok := byLang[targetLang]; ok && v != "" {
			return v, true
		}
		// 目标语没有时回退英文（海外 Google 对英文品类识别最好）
		if targetLang != "en" {
			if v, ok := byLang["en"]; ok && v != "" {
				return v, true
			}
		}

		return "", false
	}

	if byLang, ok := zhBusinessLexicon[term]; ok {
		return pick(byLang)
	}

	// 尝试去掉常见后缀再匹配：店/馆/厅
	for _, suffix := range []string{"店", "馆", "厅", "院"} {
		if strings.HasSuffix(term, suffix) && len([]rune(term)) > 1 {
			base := strings.TrimSuffix(term, suffix)
			if byLang, ok := zhBusinessLexicon[base]; ok {
				if v, ok := pick(byLang); ok {
					return v, true
				}
			}
		}
	}

	// 模糊匹配：前端/输入法偶发丢字（「啡店」←「咖啡店」），用最长包含关系回落词典
	bestKey := ""
	bestLen := 0
	termRunes := len([]rune(term))

	for key := range zhBusinessLexicon {
		if key == term {
			continue
		}
		if strings.Contains(key, term) || strings.Contains(term, key) {
			n := len([]rune(key))
			// 更偏好与输入长度接近且更长的词条
			score := n
			if strings.Contains(key, term) {
				score += 10 - absInt(n-termRunes)
			}
			if score > bestLen {
				bestLen = score
				bestKey = key
			}
		}
	}

	if bestKey != "" {
		return pick(zhBusinessLexicon[bestKey])
	}

	return "", false
}

func absInt(v int) int {
	if v < 0 {
		return -v
	}

	return v
}

func normalizeMT(s string) string {
	s = strings.TrimSpace(s)
	s = camelSplitRE.ReplaceAllString(s, "$1 $2")
	s = strings.Join(strings.Fields(s), " ")

	return s
}

type myMemoryResponse struct {
	ResponseData struct {
		TranslatedText string `json:"translatedText"`
	} `json:"responseData"`
}

// translateText 调用 MyMemory；失败返回错误，由调用方决定是否回退原文。
func translateText(ctx context.Context, text, from, to string) (string, error) {
	text = strings.TrimSpace(text)
	if text == "" {
		return "", fmt.Errorf("empty text")
	}

	from = myMemoryLang(from)
	to = myMemoryLang(to)
	if from == to {
		return text, nil
	}

	apiURL := fmt.Sprintf("%s?q=%s&langpair=%s|%s&de=%s",
		myMemoryTranslateURL,
		url.QueryEscape(text),
		url.QueryEscape(from),
		url.QueryEscape(to),
		url.QueryEscape("google-maps-scraper@local"),
	)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, apiURL, nil)
	if err != nil {
		return "", err
	}

	req.Header.Set("User-Agent", "google-maps-scraper/1.0")

	client := &http.Client{Timeout: 8 * time.Second}

	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("translate request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("translate status %d", resp.StatusCode)
	}

	var body myMemoryResponse
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return "", fmt.Errorf("translate decode: %w", err)
	}

	out := strings.TrimSpace(body.ResponseData.TranslatedText)
	if out == "" || strings.EqualFold(out, text) {
		return "", fmt.Errorf("empty translation")
	}
	// MyMemory 偶发返回 QUERY LENGTH LIMIT 之类错误串
	if strings.Contains(strings.ToUpper(out), "QUERY LENGTH LIMIT") ||
		strings.Contains(strings.ToUpper(out), "INVALID") {
		return "", fmt.Errorf("translate api error: %s", out)
	}

	return out, nil
}

func myMemoryLang(code string) string {
	switch strings.ToLower(strings.TrimSpace(code)) {
	case "zh", "zh-cn", "zh-hans":
		return "zh-CN"
	case "zh-tw", "zh-hant":
		return "zh-TW"
	default:
		return strings.ToLower(strings.TrimSpace(code))
	}
}
