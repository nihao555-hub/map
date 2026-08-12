package outreach

import "strings"

// Intent is the current buying-intent assessment of a contact. Score is
// 0-100; Label is one of the Chinese labels below because that is what the
// operator reads in the workspace UI.
type Intent struct {
	Score  int    `json:"score"`
	Label  string `json:"label"`
	Reason string `json:"reason"`
}

// Intent labels.
const (
	IntentHigh    = "高意向"
	IntentMedium  = "中意向"
	IntentLow     = "低意向"
	IntentNone    = "无意向"
	IntentPending = "待观察"
	IntentInvalid = "无效邮箱"
)

func validIntentLabel(label string) bool {
	switch label {
	case IntentHigh, IntentMedium, IntentLow, IntentNone:
		return true
	default:
		return false
	}
}

var (
	highIntentSignals = []string{
		"price", "pricing", "quote", "quotation", "moq", "sample", "samples",
		"catalog", "catalogue", "price list", "lead time", "how much",
		"schedule a call", "book a call", "let's talk", "call me", "meeting",
		"报价", "价格", "样品", "起订量", "目录", "货期", "打样", "约个时间",
	}
	mediumIntentSignals = []string{
		"interested", "tell me more", "more information", "more details",
		"send me", "how does", "how do you", "what exactly", "next month",
		"later this", "contact me in", "reach out in", "circle back",
		"感兴趣", "详细", "了解一下", "介绍", "下个月", "过段时间", "再联系",
	}
	lowIntentSignals = []string{
		"not the right person", "wrong person", "forward", "forwarded",
		"maybe later", "not right now", "not at the moment", "busy",
		"不是负责人", "转给", "暂时不", "现在不", "以后再说",
	}
	noIntentSignals = []string{
		"not interested", "no thanks", "no thank you", "stop contacting",
		"remove me", "unsubscribe", "do not contact", "spam",
		"不感兴趣", "不需要", "别再", "退订", "垃圾邮件",
	}
	autoReplySignals = []string{
		"out of office", "auto-reply", "automatic reply", "autoreply",
		"on vacation", "annual leave", "maternity leave",
		"自动回复", "休假", "不在办公室",
	}
)

// HeuristicIntent scores an inbound message without AI. It is intentionally
// conservative: uncertain replies land in the middle so a human reviews them.
func HeuristicIntent(kind, subject, body string) Intent {
	switch kind {
	case InboundKindBounce:
		return Intent{Score: 0, Label: IntentInvalid, Reason: "邮件被退回，地址无效或被拒收"}
	case InboundKindUnsubscribe:
		return Intent{Score: 0, Label: IntentNone, Reason: "客户明确要求退订"}
	}

	text := strings.ToLower(subject + "\n" + unquotedReply(body))

	if containsAny(text, autoReplySignals) {
		return Intent{Score: 40, Label: IntentPending, Reason: "自动回复/休假邮件，等真人回复后再评估"}
	}

	if containsAny(text, noIntentSignals) {
		return Intent{Score: 10, Label: IntentNone, Reason: "回复中出现明确拒绝用语"}
	}

	if containsAny(text, highIntentSignals) {
		return Intent{Score: 85, Label: IntentHigh, Reason: "回复中询问价格/样品/会谈等购买信号"}
	}

	if containsAny(text, mediumIntentSignals) {
		return Intent{Score: 65, Label: IntentMedium, Reason: "客户表达兴趣或索取更多信息"}
	}

	if containsAny(text, lowIntentSignals) {
		return Intent{Score: 35, Label: IntentLow, Reason: "客户婉拒或转介他人"}
	}

	return Intent{Score: 60, Label: IntentMedium, Reason: "客户已回复，内容需人工判断"}
}

// StatusIntent derives a display intent for contacts that have not been
// scored from an actual reply yet.
func StatusIntent(status string) Intent {
	switch status {
	case ContactStatusBounced:
		return Intent{Score: 0, Label: IntentInvalid, Reason: "邮件被退回"}
	case ContactStatusUnsubscribed:
		return Intent{Score: 0, Label: IntentNone, Reason: "客户已退订"}
	case ContactStatusFailed:
		return Intent{Score: 5, Label: IntentInvalid, Reason: "多次发送失败"}
	case ContactStatusCompleted:
		return Intent{Score: 15, Label: IntentLow, Reason: "序列已发完，客户未回复"}
	case ContactStatusReplied:
		return Intent{Score: 60, Label: IntentMedium, Reason: "客户已回复，等待评估"}
	default:
		return Intent{Score: 40, Label: IntentPending, Reason: "尚未回复，继续观察"}
	}
}
