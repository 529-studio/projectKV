package telegrambot

import (
	"strings"
	"testing"
	"time"
	"unicode"
)

const testBotUsername = "WardedAlertBot"

// containsEmoji reports any pictographic / emoji / presentation codepoint.
// Go's unicode tables do not expose Extended_Pictographic, so the guard unions
// the blocks the property covers plus the two emoji presentation controls.
func containsEmoji(s string) (rune, bool) {
	for _, r := range s {
		if unicode.Is(unicode.So, r) && r > 0x00A9 {
			return r, true
		}
		switch {
		case r >= 0x1F000 && r <= 0x1FAFF, // emoji, supplementary symbols
			r >= 0x2600 && r <= 0x27BF, // misc symbols, dingbats
			r >= 0x2B00 && r <= 0x2BFF, // arrows/stars used as icons
			r >= 0x2190 && r <= 0x21FF, // arrows (not emoji, but forbidden ornament)
			r == 0xFE0F,                // variation selector-16
			r == 0x200D:                // zero-width joiner
			return r, true
		}
	}
	return 0, false
}

func assertNoEmoji(t *testing.T, name, text string) {
	t.Helper()
	if r, found := containsEmoji(text); found {
		t.Fatalf("Rule 1 violation: %s contains U+%04X (%q) in:\n%s", name, r, r, text)
	}
	if strings.Contains(text, "**") || strings.Contains(text, "__") {
		t.Fatalf("Rule 1 violation: %s uses markdown emphasis", name)
	}
	for _, forbidden := range []string{"━", "│", "┌", "└", "├"} {
		if strings.Contains(text, forbidden) {
			t.Fatalf("Rule 1 violation: %s uses box drawing %q", name, forbidden)
		}
	}
}

func assertBilingual(t *testing.T, name, text, viMarker, enMarker string) {
	t.Helper()
	if !strings.Contains(text, viMarker) {
		t.Errorf("%s missing Vietnamese marker %q:\n%s", name, viMarker, text)
	}
	if !strings.Contains(text, enMarker) {
		t.Errorf("%s missing English marker %q:\n%s", name, enMarker, text)
	}
	if !strings.Contains(text, "\n\n") {
		t.Errorf("%s must separate the VI and EN blocks with a blank line:\n%s", name, text)
	}
	if strings.Index(text, viMarker) > strings.Index(text, enMarker) {
		t.Errorf("%s must put the Vietnamese block first:\n%s", name, text)
	}
}

func TestParseCommand(t *testing.T) {
	cases := []struct {
		name string
		text string
		bot  string
		want Command
	}{
		{"start", "/start", testBotUsername, Command{Name: "start"}},
		{"start with payload", "/start sub_all", testBotUsername, Command{Name: "start", Arg: "sub_all"}},
		{"start keeps slug case", "/start sub_zone_QsOsbmggRMawxqFuZw", testBotUsername,
			Command{Name: "start", Arg: "sub_zone_QsOsbmggRMawxqFuZw"}},
		{"help", "/help", testBotUsername, Command{Name: "help"}},
		{"status", "/status", testBotUsername, Command{Name: "status"}},
		{"stop", "/stop", testBotUsername, Command{Name: "stop"}},
		{"unsubscribe", "/unsubscribe", testBotUsername, Command{Name: "unsubscribe"}},
		{"addressed to this bot", "/status@WardedAlertBot", testBotUsername, Command{Name: "status"}},
		{"addressed case-insensitively", "/status@wardedalertbot", testBotUsername, Command{Name: "status"}},
		{"addressed with leading at", "/status@WardedAlertBot", "@WardedAlertBot", Command{Name: "status"}},
		{"command case-insensitive", "/STaTus", testBotUsername, Command{Name: "status"}},
		{"addressed and argument", "/start@WardedAlertBot sub_ward_QuG6v24gTmdow6k", testBotUsername,
			Command{Name: "start", Arg: "sub_ward_QuG6v24gTmdow6k"}},
		{"addressed to another bot", "/status@OtherBot", testBotUsername, Command{}},
		{"unknown command", "/frobnicate", testBotUsername, Command{Name: "frobnicate"}},
		{"plain text", "hello there", testBotUsername, Command{}},
		{"empty", "", testBotUsername, Command{}},
		{"bare slash", "/", testBotUsername, Command{}},
		{"leading whitespace", "   /help  ", testBotUsername, Command{Name: "help"}},
		{"newline separated argument", "/start\nsub_all", testBotUsername, Command{Name: "start", Arg: "sub_all"}},
		{"non-command with slash inside", "http://example.com", testBotUsername, Command{}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := ParseCommand(tc.text, tc.bot); got != tc.want {
				t.Fatalf("ParseCommand(%q, %q) = %+v, want %+v", tc.text, tc.bot, got, tc.want)
			}
		})
	}
}

func TestRenderWelcome(t *testing.T) {
	out := RenderWelcome(2, testBotUsername)

	assertBilingual(t, "RenderWelcome", out, "Chào mừng bạn đến với Warded Alert.", "Welcome to Warded Alert.")
	if !strings.Contains(out, "Số đăng ký đang hoạt động: 2.") || !strings.Contains(out, "Active subscriptions: 2.") {
		t.Errorf("RenderWelcome must report the active count in both languages:\n%s", out)
	}
	if !strings.Contains(out, "/help") {
		t.Errorf("RenderWelcome must point at /help:\n%s", out)
	}
	for _, payload := range []string{"sub_all", "sub_zone_", "sub_ward_"} {
		if got := strings.Count(out, "?start="+payload); got != 2 {
			t.Errorf("RenderWelcome must carry one %q example per language, got %d:\n%s", payload, got, out)
		}
	}
	// Three examples per language block, VI first.
	if got := strings.Count(out, "https://t.me/"+testBotUsername+"?start="); got != 6 {
		t.Errorf("RenderWelcome must carry 3 example deep links in each language, got %d:\n%s", got, out)
	}
	assertNoEmoji(t, "RenderWelcome", out)
}

func TestRenderHelp(t *testing.T) {
	out := RenderHelp("https://warded.529studio.site")
	assertBilingual(t, "RenderHelp", out, "Warded Alert - Trợ giúp", "Warded Alert - Help")
	for _, cmd := range []string{"/status", "/stop", "/unsubscribe"} {
		if strings.Count(out, cmd) < 2 {
			t.Errorf("RenderHelp must document %s in both languages:\n%s", cmd, out)
		}
	}
	if !strings.Contains(out, `href="https://warded.529studio.site"`) {
		t.Errorf("RenderHelp must link the web subscription page:\n%s", out)
	}
	assertNoEmoji(t, "RenderHelp", out)

	// An empty URL drops the link instead of rendering an empty anchor.
	if empty := RenderHelp("   "); strings.Contains(empty, "href=") {
		t.Errorf("RenderHelp with an empty URL must not emit a link:\n%s", empty)
	}
}

func TestRenderSubscribed(t *testing.T) {
	out := RenderSubscribed(Target{Type: targetTypeWard, ID: "Bến Nghé"}, "Bến Nghé")
	assertBilingual(t, "RenderSubscribed", out, "Đã đăng ký thành công.", "Subscription confirmed.")
	if strings.Count(out, "Bến Nghé") != 2 {
		t.Errorf("RenderSubscribed must name the scope in both blocks:\n%s", out)
	}
	if !strings.Contains(out, "/status") || !strings.Contains(out, "/stop") {
		t.Errorf("RenderSubscribed must mention /status and /stop:\n%s", out)
	}
	assertNoEmoji(t, "RenderSubscribed", out)

	// "all" is translated in both languages; a proper noun is shared.
	all := RenderSubscribed(Target{Type: targetTypeAll}, "")
	if !strings.Contains(all, "Toàn thành phố") || !strings.Contains(all, "City-wide") {
		t.Errorf("RenderSubscribed(all) must translate the city-wide scope:\n%s", all)
	}
	assertNoEmoji(t, "RenderSubscribed(all)", all)
}

func TestRenderAlreadySubscribed(t *testing.T) {
	out := RenderAlreadySubscribed(Target{Type: targetTypeZone, ID: "Quận 7 - Nhà Bè"}, "Quận 7 - Nhà Bè")
	assertBilingual(t, "RenderAlreadySubscribed", out,
		"Phạm vi này đã được đăng ký", "This scope is already active")
	if !strings.Contains(out, "Quận 7 - Nhà Bè") {
		t.Errorf("RenderAlreadySubscribed must name the scope:\n%s", out)
	}
	if strings.Contains(out, "Đã đăng ký thành công") {
		t.Errorf("RenderAlreadySubscribed must not claim a new subscription:\n%s", out)
	}
	assertNoEmoji(t, "RenderAlreadySubscribed", out)
}

type stubLabeler struct {
	labels map[string]string
	calls  int
}

func (s *stubLabeler) Label(t Target) string {
	s.calls++
	if v, ok := s.labels[t.Type+"|"+t.ID]; ok {
		return v
	}
	return t.ID
}

func TestRenderStatus(t *testing.T) {
	labeler := &stubLabeler{labels: map[string]string{
		"zone|Quận 7 - Nhà Bè":  "Quận 7 - Nhà Bè",
		"ward|Bến Nghé":         "Bến Nghé",
		"chokepoint|cam-vvk-01": "Võ Văn Kiệt - Ký Con",
	}}
	created := time.Date(2026, 11, 12, 10, 0, 0, 0, time.UTC)
	subs := []Record{
		{TargetType: targetTypeAll, CreatedAt: created},
		{TargetType: targetTypeZone, TargetID: "Quận 7 - Nhà Bè", CreatedAt: created},
		{TargetType: targetTypeWard, TargetID: "Bến Nghé", CreatedAt: created},
		{TargetType: targetTypeChokepoint, TargetID: "cam-vvk-01", CreatedAt: created},
	}

	out := RenderStatus(subs, labeler)
	assertBilingual(t, "RenderStatus", out, "Đăng ký đang hoạt động (4):", "Active subscriptions (4):")
	for _, want := range []string{
		"Toàn thành phố", "City-wide",
		"Khu vực: Quận 7 - Nhà Bè", "Zone: Quận 7 - Nhà Bè",
		"Phường / Xã: Bến Nghé", "Ward / Commune: Bến Nghé",
		"Điểm ngập: Võ Văn Kiệt - Ký Con", "Chokepoint: Võ Văn Kiệt - Ký Con",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("RenderStatus missing %q:\n%s", want, out)
		}
	}
	if labeler.calls < len(subs) {
		t.Errorf("RenderStatus must label every record, got %d calls", labeler.calls)
	}
	assertNoEmoji(t, "RenderStatus", out)

	empty := RenderStatus(nil, labeler)
	assertBilingual(t, "RenderStatus(empty)", empty, "Bạn chưa có đăng ký nào.", "You have no active subscriptions.")
	assertNoEmoji(t, "RenderStatus(empty)", empty)

	// A nil labeler falls back to the stored id instead of panicking.
	fallback := RenderStatus([]Record{{TargetType: targetTypeWard, TargetID: "Thảo Điền"}}, nil)
	if !strings.Contains(fallback, "Thảo Điền") {
		t.Errorf("RenderStatus without a labeler must print the stored id:\n%s", fallback)
	}
}

func TestRenderStopped(t *testing.T) {
	cases := []struct {
		removed int
		vi      string
		en      string
	}{
		{0, "Đã huỷ 0 đăng ký.", "Removed 0 subscriptions."},
		{1, "Đã huỷ 1 đăng ký.", "Removed 1 subscription."},
		{3, "Đã huỷ 3 đăng ký.", "Removed 3 subscriptions."},
		{-2, "Đã huỷ 0 đăng ký.", "Removed 0 subscriptions."},
	}
	for _, tc := range cases {
		out := RenderStopped(tc.removed)
		if !strings.Contains(out, tc.vi) || !strings.Contains(out, tc.en) {
			t.Errorf("RenderStopped(%d) missing %q / %q:\n%s", tc.removed, tc.vi, tc.en, out)
		}
		assertNoEmoji(t, "RenderStopped", out)
	}
}

func TestRenderUnknownTarget(t *testing.T) {
	out := RenderUnknownTarget("sub_zone_quận 7")
	assertBilingual(t, "RenderUnknownTarget", out, "Không tìm thấy phạm vi:", "Target not found:")
	if !strings.Contains(out, "/help") {
		t.Errorf("RenderUnknownTarget must point at /help:\n%s", out)
	}
	assertNoEmoji(t, "RenderUnknownTarget", out)

	long := RenderUnknownTarget(strings.Repeat("á", 200))
	if strings.Contains(long, strings.Repeat("á", 100)) {
		t.Errorf("RenderUnknownTarget must truncate an over-long raw value")
	}
	empty := RenderUnknownTarget("   ")
	if !strings.Contains(empty, "(empty)") {
		t.Errorf("RenderUnknownTarget must render a placeholder for an empty value:\n%s", empty)
	}
}

func TestRenderLimitReached(t *testing.T) {
	out := RenderLimitReached(20)
	assertBilingual(t, "RenderLimitReached", out, "giới hạn 20 đăng ký", "limit of 20 subscriptions")
	if !strings.Contains(out, "/stop") {
		t.Errorf("RenderLimitReached must point at /stop:\n%s", out)
	}
	assertNoEmoji(t, "RenderLimitReached", out)
}

func TestRenderPrivateChatOnly(t *testing.T) {
	out := RenderPrivateChatOnly("@" + testBotUsername)
	assertBilingual(t, "RenderPrivateChatOnly", out, "chỉ trả lời trong cuộc trò chuyện riêng", "only replies in private chats")
	if !strings.Contains(out, "https://t.me/"+testBotUsername) {
		t.Errorf("RenderPrivateChatOnly must give the DM link:\n%s", out)
	}
	assertNoEmoji(t, "RenderPrivateChatOnly", out)
}

func TestRenderSlowDown(t *testing.T) {
	out := RenderSlowDown()
	assertBilingual(t, "RenderSlowDown", out, "Vui lòng thử lại sau một phút.", "try again in a minute")
	assertNoEmoji(t, "RenderSlowDown", out)
}

func TestRenderDeliveryFailed(t *testing.T) {
	out := RenderDeliveryFailed()
	assertBilingual(t, "RenderDeliveryFailed", out, "Vui lòng thử lại sau.", "Please try again later.")
	assertNoEmoji(t, "RenderDeliveryFailed", out)
}

// TestRenderersHTMLEscaping guards spec §2.7 / E13: every interpolated value is
// escaped, so a malicious ward or camera name can never break out of the HTML.
func TestRenderersHTMLEscaping(t *testing.T) {
	payload := `A & B <script>alert("x")</script>`

	cases := []struct {
		name string
		out  string
	}{
		{"RenderSubscribed", RenderSubscribed(Target{Type: targetTypeWard, ID: payload}, payload)},
		{"RenderAlreadySubscribed", RenderAlreadySubscribed(Target{Type: targetTypeWard, ID: payload}, payload)},
		{"RenderStatus", RenderStatus([]Record{{TargetType: targetTypeWard, TargetID: payload}}, nil)},
		{"RenderUnknownTarget", RenderUnknownTarget(payload)},
		{"RenderHelp", RenderHelp(`https://example.com/?a=1&b=<x>`)},
	}

	for _, tc := range cases {
		if strings.Contains(tc.out, "<script>") {
			t.Errorf("%s did not escape a <script> payload:\n%s", tc.name, tc.out)
		}
		if strings.Contains(tc.out, `alert("x")`) {
			t.Errorf("%s did not escape double quotes:\n%s", tc.name, tc.out)
		}
		if !strings.Contains(tc.out, "&amp;") {
			t.Errorf("%s did not escape an ampersand:\n%s", tc.name, tc.out)
		}
		if !strings.Contains(tc.out, "&lt;") {
			t.Errorf("%s did not escape a less-than sign:\n%s", tc.name, tc.out)
		}
		assertNoEmoji(t, tc.name, tc.out)
	}

	if !strings.Contains(cases[4].out, "a=1&amp;b=&lt;x&gt;") {
		t.Errorf("RenderHelp must escape the web URL used in href and text:\n%s", cases[4].out)
	}
}

// TestAllRenderersAreEmojiFree walks every renderer once more through a single
// table so a new renderer cannot be added without joining the guard.
func TestAllRenderersAreEmojiFree(t *testing.T) {
	outputs := map[string]string{
		"RenderWelcome":           RenderWelcome(0, testBotUsername),
		"RenderHelp":              RenderHelp(defaultWebURL),
		"RenderSubscribed":        RenderSubscribed(Target{Type: targetTypeAll}, ""),
		"RenderAlreadySubscribed": RenderAlreadySubscribed(Target{Type: targetTypeZone, ID: "Bình Dương"}, "Bình Dương"),
		"RenderStatus":            RenderStatus([]Record{{TargetType: targetTypeZone, TargetID: "Bình Dương"}}, nil),
		"RenderStopped":           RenderStopped(0),
		"RenderUnknownTarget":     RenderUnknownTarget("sub_bogus"),
		"RenderLimitReached":      RenderLimitReached(20),
		"RenderPrivateChatOnly":   RenderPrivateChatOnly(testBotUsername),
		"RenderSlowDown":          RenderSlowDown(),
		"RenderDeliveryFailed":    RenderDeliveryFailed(),
	}
	for name, out := range outputs {
		if strings.TrimSpace(out) == "" {
			t.Fatalf("%s returned an empty string", name)
		}
		assertNoEmoji(t, name, out)
		if strings.Contains(out, "<b>") || strings.Contains(out, "<i>") {
			t.Errorf("%s must stay monochromatic (no inline emphasis):\n%s", name, out)
		}
	}
}
