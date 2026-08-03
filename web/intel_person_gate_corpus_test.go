package web

import "testing"

// 语料取自公网实测（雅加达批发商 382 家，354 家 ready）里真实出现过的姓名字段。
// 每一条都必须被拦住或放行，避免噪音回归。

func TestPersonGateRejectsRealNoise(t *testing.T) {
	noise := []struct {
		name     string
		email    string
		business string
		why      string
	}{
		// 1) 门店邮箱前缀被拼成人名
		{"Siljlpanjangkpbaru Jkt", "siljlpanjangkpbaru.jkt@store.sils.co.id", "DanDan Jl Panjang Kp Baru", "邮箱前缀+地区代号"},
		{"Silpanjangcidodol Jkt", "silpanjangcidodol.jkt@store.sils.co.id", "DanDan Jl Panjang Kp Baru", "同上"},
		{"Silhmuchtarraya Tgr", "silhmuchtarraya.tgr@store.sils.co.id", "DanDan Jl Panjang Kp Baru", "同上"},
		{"Silmeruyaselatan Jkt", "silmeruyaselatan.jkt@store.sils.co.id", "DanDan Jl Panjang Kp Baru", "同上"},
		{"Silcipadu Tgr", "silcipadu.tgr@store.sils.co.id", "DanDan Jl Panjang Kp Baru", "同上"},
		{"Athangemilangperkasa", "athangemilangperkasa@gmail.com", "PT Sempurna", "单段邮箱前缀"},

		// 2) 网页句子片段
		{"usaha bisnis", "", "DuniaMasak.com", "全小写句子片段"},
		{"CIO dan", "", "PT. Mitra Computa Asia", "职能词+连接词"},
		{"related Internal control", "", "PT. Meiwa Trading Indonesia", "句子片段"},
		{"Management / Founder", "", "Any", "职能描述"},
		{"Meet Our", "", "Any", "页面标题片段"},
		{"Latest Blogs", "", "Any", "页面区块标题"},
		{"Rtr Security Threats", "", "Corryndo Jaya", "注册商安全告示"},
		{"Privacy Policy", "", "Any", "页面条款标题"},
		{"Domain Operations", "", "PT Karya Utama Globalindo", "域名运维通道"},
		{"Erinda Supplier", "", "Erinda Seafood Supplier", "店名+业务描述词"},

		// 3) 模板占位
		{"Aston Doe", "", "Toko Brilian Jaya Prima", "模板占位名"},
		{"John Doe", "", "Any", "模板占位名"},

		// 4) 店名回声 / 注册商
		{"Doxionte cafe (Pemilik)", "", "Doxionte cafe", "店名+角色后缀"},
		{"Cafe Ade", "", "Cafe Ade", "与店名相同"},
		{"Abuse Complaints", "abuse-complaints@squarespace.com", "Tuang Coffee", "注册商投诉通道"},

		// 5) 主体名当人名
		{"PT Tiara Kencana", "", "Any", "公司主体"},
		{"Toko Grosir Jaya", "", "Any", "店铺名"},
	}

	for _, tc := range noise {
		d := DecisionMaker{Name: tc.name, Email: tc.email}
		if QualifiesAsDecisionMaker(d, tc.business) {
			t.Errorf("noise passed the gate: %q (%s)", tc.name, tc.why)
		}
	}
}

func TestPersonGateKeepsRealPeople(t *testing.T) {
	real := []struct {
		name     string
		email    string
		business string
	}{
		{"Hermann Krone", "", "DISTRIBUTOR ALAT INDUSTRI"},
		{"Dwiky Wijaya", "", "Purnama Chandra"},
		{"F. Tirto Koesnadi", "", "PT. Tiara Kencana"},
		{"Rina Irrawati", "", "Distributor British Propolis"},
		{"Eko Marwansyah", "", "PT. Hampton Muda Berkarya"},
		{"Anggit Kurniawan Nasution", "", "PT. Hampton Muda Berkarya"},
		{"Feriska Febrina", "", "PT. Happy Pet Indonesia"},
		{"Feny Fiona Marlina", "", "PT. Happy Pet Indonesia"},
		{"Charity Danar", "", "PT. Happy Pet Indonesia"},
		{"Dani Adirama", "", "PT. ADI RAYA MANDIRI"},
		{"Enristia P", "enristiap@gmail.com", "Aquatic Cafe"},
		{"Edward Tirtanata", "", "Kopi Kenangan"},
		{"Djoko Susanto", "", "Alfamart"},
		// 公网复跑（雅加达批发商，半径 3km）真实留下的人名
		{"Romlan Dumar", "", "BUANA KARPET JAKARTA"},
		{"Sri Mulyati", "", "Alaidrous Indonesia"},
		{"Intan Arfandy", "", "Alaidrous Indonesia"},
		{"Linda Anggrea", "", "Alaidrous Indonesia"},
		// 店名括注里写明的店主
		{"Siti Hodijah", "", "Distributor OSB Jakarta (Siti Hodijah)"},
	}

	for _, tc := range real {
		d := DecisionMaker{Name: tc.name, Email: tc.email}
		if !QualifiesAsDecisionMaker(d, tc.business) {
			t.Errorf("real person rejected: %q", tc.name)
		}
	}
}

// NameDerivedFromEmail 只是弱证据标记，不参与丢弃判定；这里固定它的语义。
func TestNameDerivedFromEmailHandlesSeparators(t *testing.T) {
	cases := []struct {
		name, email string
		want        bool
	}{
		{"Siljlpanjangkpbaru Jkt", "siljlpanjangkpbaru.jkt@store.sils.co.id", true},
		{"Sarina Yance", "sarina.yance@deugro.com", true},
		{"Enristia P", "enristiap@gmail.com", true},
		{"John Smith", "j.smith@corp.com", false},
		{"Hermann Krone", "info@distributoralatindustri.com", false},
		{"Anything", "", false},
	}

	for _, tc := range cases {
		if got := NameDerivedFromEmail(tc.name, tc.email); got != tc.want {
			t.Errorf("NameDerivedFromEmail(%q, %q) = %v, want %v", tc.name, tc.email, got, tc.want)
		}
	}
}

func TestSanitizeDemotesNoiseToChannel(t *testing.T) {
	place := Place{Title: "DanDan Jl Panjang Kp Baru", Phone: "+6282111206399"}
	in := []DecisionMaker{
		{
			Name: "Siljlpanjangkpbaru Jkt", Title: "Contact",
			Email: "siljlpanjangkpbaru.jkt@store.sils.co.id", Confidence: "high",
		},
		{Name: "Feriska Febrina", Title: "GM", Phone: "+62 21 5278567"},
	}

	out := sanitizeDecisionMakers(in, place)

	var sawNoiseName, sawRealPerson bool
	for _, d := range out {
		if d.Name == "Siljlpanjangkpbaru Jkt" {
			sawNoiseName = true
		}
		if d.Name == "Feriska Febrina" {
			sawRealPerson = true
		}
	}

	if sawNoiseName {
		t.Error("email-derived name should be demoted to an unnamed channel")
	}
	if !sawRealPerson {
		t.Error("real person should survive sanitization")
	}
	if countNamedPeople(out) != 1 {
		t.Errorf("countNamedPeople = %d, want 1", countNamedPeople(out))
	}
}

func TestAvatarsNeverFabricated(t *testing.T) {
	makers := []DecisionMaker{
		{Name: "Feriska Febrina", Email: "feriska@happypet.co.id"},
		{Name: "Charity Danar"},
	}

	enrichDecisionMakerAvatars(makers)

	for _, d := range makers {
		if d.Avatar != "" {
			t.Errorf("avatar should stay empty without a real profile image, got %q", d.Avatar)
		}
	}
}
