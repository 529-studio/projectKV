package telegrambot

import (
	"fmt"
	"html"
	"strings"
	"time"
)

// Command is a parsed bot command. An empty Name means "not a command for this
// bot" (plain text, empty text, or a command addressed to another username).
type Command struct {
	Name string
	Arg  string
}

// Record is one stored subscription as seen by the bot package.
type Record struct {
	TargetType string
	TargetID   string
	CreatedAt  time.Time
}

// Labeler turns a Target into the citizen-facing name of that scope.
// Taxonomy satisfies it; the package stays free of catalog imports.
type Labeler interface {
	Label(Target) string
}

// ParseCommand extracts a command name and argument from message text.
//
// Rules (spec §2.4):
//   - the text must start with '/';
//   - an "@BotUsername" suffix is stripped, case-insensitively, but only when it
//     addresses this bot — a command for another bot yields an empty Command;
//   - command names are matched case-insensitively; the argument keeps its case
//     because base64url slugs are case-sensitive.
func ParseCommand(text, botUsername string) Command {
	trimmed := strings.TrimSpace(text)
	if !strings.HasPrefix(trimmed, "/") {
		return Command{}
	}

	head, rest := trimmed, ""
	if idx := strings.IndexAny(trimmed, " \t\r\n"); idx >= 0 {
		head, rest = trimmed[:idx], trimmed[idx:]
	}

	name := strings.TrimPrefix(head, "/")
	if at := strings.IndexByte(name, '@'); at >= 0 {
		addressed := strings.TrimSpace(name[at+1:])
		name = name[:at]
		bot := strings.TrimPrefix(strings.TrimSpace(botUsername), "@")
		if bot != "" && !strings.EqualFold(addressed, bot) {
			return Command{}
		}
	}

	name = strings.ToLower(strings.TrimSpace(name))
	if name == "" {
		return Command{}
	}
	return Command{Name: name, Arg: strings.TrimSpace(rest)}
}

// RenderWelcome is the /start reply without a payload: welcome copy, the active
// subscription count, three example deep links and the /help hint.
func RenderWelcome(activeCount int, botUsername string) string {
	bot := strings.TrimPrefix(strings.TrimSpace(botUsername), "@")

	type example struct {
		target Target
		vi     string
		en     string
	}
	examples := []example{
		{Target{Type: targetTypeAll}, "Toàn thành phố", "City-wide"},
		{Target{Type: targetTypeZone, ID: "Khu vực Nội thành TP.HCM"}, "Khu vực Nội thành TP.HCM", "Inner-city zone"},
		{Target{Type: targetTypeWard, ID: "Bến Nghé"}, "Phường Bến Nghé", "Bến Nghé Ward"},
	}

	viLinks := make([]string, 0, len(examples))
	enLinks := make([]string, 0, len(examples))
	for _, ex := range examples {
		payload, err := EncodePayload(ex.target)
		if err != nil {
			continue
		}
		link := deepLink(bot, payload)
		viLinks = append(viLinks, "- "+anchor(link, escapeHTML(ex.vi)))
		enLinks = append(enLinks, "- "+anchor(link, escapeHTML(ex.en)))
	}

	vi := []string{
		"Chào mừng bạn đến với Warded Alert.",
		fmt.Sprintf("Số đăng ký đang hoạt động: %d.", activeCount),
		"",
		"Ví dụ đăng ký nhanh:",
	}
	vi = append(vi, viLinks...)
	vi = append(vi, "", "Gõ /help để xem hướng dẫn.")

	en := []string{
		"Welcome to Warded Alert.",
		fmt.Sprintf("Active subscriptions: %d.", activeCount),
		"",
		"Quick examples:",
	}
	en = append(en, enLinks...)
	en = append(en, "", "Send /help for guidance.")

	return strings.Join(vi, "\n") + "\n\n" + strings.Join(en, "\n")
}

// RenderHelp lists the available commands and the web subscription page.
func RenderHelp(webURL string) string {
	link := ""
	if strings.TrimSpace(webURL) != "" {
		escaped := escapeHTML(strings.TrimSpace(webURL))
		link = anchor(escaped, escaped)
	}

	vi := []string{
		"Warded Alert - Trợ giúp",
		"- /status: xem các đăng ký đang hoạt động",
		"- /stop: huỷ toàn bộ đăng ký",
		"- /unsubscribe: huỷ toàn bộ đăng ký",
	}
	en := []string{
		"Warded Alert - Help",
		"- /status: view your active subscriptions",
		"- /stop: remove all subscriptions",
		"- /unsubscribe: remove all subscriptions",
	}
	if link != "" {
		vi = append(vi, "Đăng ký trên web: "+link)
		en = append(en, "Subscribe on the web: "+link)
	}

	return strings.Join(vi, "\n") + "\n\n" + strings.Join(en, "\n")
}

// RenderSubscribed confirms a new subscription and names its scope.
func RenderSubscribed(t Target, label string) string {
	viLabel, enLabel := scopeLabels(t, label)
	return fmt.Sprintf(
		"Đã đăng ký thành công.\nPhạm vi: %s\nGõ /status để xem danh sách, /stop để huỷ.\n\n"+
			"Subscription confirmed.\nScope: %s\nSend /status to review, /stop to unsubscribe.",
		escapeHTML(viLabel), escapeHTML(enLabel),
	)
}

// RenderAlreadySubscribed replies to a duplicate /start without touching the DB.
func RenderAlreadySubscribed(t Target, label string) string {
	viLabel, enLabel := scopeLabels(t, label)
	return fmt.Sprintf(
		"Phạm vi này đã được đăng ký: %s\nKhông có thay đổi nào. Gõ /status để xem danh sách.\n\n"+
			"This scope is already active: %s\nNo changes were made. Send /status to review your subscriptions.",
		escapeHTML(viLabel), escapeHTML(enLabel),
	)
}

// RenderStatus lists the active subscriptions, or reports that there are none.
func RenderStatus(subs []Record, labeler Labeler) string {
	if len(subs) == 0 {
		return "Bạn chưa có đăng ký nào. Gõ /start để bắt đầu.\n\n" +
			"You have no active subscriptions. Send /start to get started."
	}

	vi := []string{fmt.Sprintf("Đăng ký đang hoạt động (%d):", len(subs))}
	en := []string{fmt.Sprintf("Active subscriptions (%d):", len(subs))}
	for _, sub := range subs {
		target := Target{Type: sub.TargetType, ID: sub.TargetID}
		label := ""
		if labeler != nil {
			label = strings.TrimSpace(labeler.Label(target))
		}
		if label == "" {
			label = sub.TargetID
		}
		switch sub.TargetType {
		case targetTypeAll:
			vi = append(vi, "- Toàn thành phố")
			en = append(en, "- City-wide")
		case targetTypeZone:
			vi = append(vi, "- Khu vực: "+escapeHTML(label))
			en = append(en, "- Zone: "+escapeHTML(label))
		case targetTypeWard:
			vi = append(vi, "- Phường / Xã: "+escapeHTML(label))
			en = append(en, "- Ward / Commune: "+escapeHTML(label))
		case targetTypeChokepoint:
			vi = append(vi, "- Điểm ngập: "+escapeHTML(label))
			en = append(en, "- Chokepoint: "+escapeHTML(label))
		default:
			vi = append(vi, "- "+escapeHTML(label))
			en = append(en, "- "+escapeHTML(label))
		}
	}

	return strings.Join(vi, "\n") + "\n\n" + strings.Join(en, "\n")
}

// RenderStopped reports how many subscriptions /stop or /unsubscribe removed.
// Zero is a valid outcome, not an error.
func RenderStopped(removed int) string {
	if removed < 0 {
		removed = 0
	}
	vi := fmt.Sprintf("Đã huỷ %d đăng ký.\nGõ /start để đăng ký lại.", removed)
	en := fmt.Sprintf("Removed %d %s.\nSend /start to subscribe again.", removed, plural(removed, "subscription", "subscriptions"))
	return vi + "\n\n" + en
}

// RenderUnknownTarget answers an undecodable or non-taxonomy deep-link target.
// No row is written and the raw value is escaped and truncated.
func RenderUnknownTarget(raw string) string {
	value := escapeHTML(truncateRunes(strings.TrimSpace(raw), 64))
	if value == "" {
		value = "(empty)"
	}
	return fmt.Sprintf(
		"Không tìm thấy phạm vi: %s\nLiên kết có thể đã cũ. Gõ /help để xem hướng dẫn.\n\n"+
			"Target not found: %s\nThe link may be outdated. Send /help for guidance.",
		value, value,
	)
}

// RenderLimitReached refuses the subscription that would exceed the per-chat cap.
func RenderLimitReached(max int) string {
	return fmt.Sprintf(
		"Bạn đã đạt giới hạn %d đăng ký cho mỗi cuộc trò chuyện.\n"+
			"Gõ /stop để huỷ toàn bộ trước khi thêm phạm vi mới.\n\n"+
			"You have reached the limit of %d subscriptions per chat.\n"+
			"Send /stop to remove all before adding a new scope.",
		max, max,
	)
}

// RenderPrivateChatOnly answers messages from groups and channels. Nothing is
// ever persisted for a non-private chat (spec D4, E9).
func RenderPrivateChatOnly(botUsername string) string {
	bot := strings.TrimPrefix(strings.TrimSpace(botUsername), "@")
	return "Bot này chỉ trả lời trong cuộc trò chuyện riêng.\n" +
		"Mở trò chuyện riêng: https://t.me/" + bot + "\n\n" +
		"This bot only replies in private chats.\n" +
		"Open a direct chat: https://t.me/" + bot
}

// RenderSlowDown is the single reply allowed per chat per minute once the
// per-chat bucket is exhausted. Later messages in the same minute stay silent.
func RenderSlowDown() string {
	return "Bạn đang gửi lệnh quá nhanh. Vui lòng thử lại sau một phút.\n\n" +
		"You are sending commands too quickly. Please try again in a minute."
}

// RenderDeliveryFailed is logged-and-replied when persistence or delivery of a
// command fails, without leaking internal detail to the citizen.
func RenderDeliveryFailed() string {
	return "Không xử lý được lệnh này. Vui lòng thử lại sau.\n\n" +
		"This command could not be processed. Please try again later."
}

// scopeLabels returns the VI and EN names for a scope. Proper nouns (zone,
// ward, camera names) are identical in both languages; "all" is translated.
func scopeLabels(t Target, label string) (string, string) {
	if t.Type == targetTypeAll {
		return "Toàn thành phố", "City-wide"
	}
	value := strings.TrimSpace(label)
	if value == "" {
		value = t.ID
	}
	return value, value
}

func deepLink(botUsername, payload string) string {
	return "https://t.me/" + botUsername + "?start=" + payload
}

// anchor renders an HTML link. Both arguments must already be HTML-escaped.
func anchor(href, text string) string {
	return `<a href="` + href + `">` + text + `</a>`
}

func escapeHTML(v string) string {
	return html.EscapeString(v)
}

func truncateRunes(v string, max int) string {
	if max <= 0 {
		return ""
	}
	runes := []rune(v)
	if len(runes) <= max {
		return v
	}
	return string(runes[:max])
}

func plural(n int, singular, pluralForm string) string {
	if n == 1 {
		return singular
	}
	return pluralForm
}
