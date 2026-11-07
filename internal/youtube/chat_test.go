package youtube

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"
)

const chatReplyJSON = `{"continuationContents":{"liveChatContinuation":{
"actions":[
 {"replayChatItemAction":{"videoOffsetTimeMsec":"1500","actions":[{"addChatItemAction":{"item":{
   "liveChatTextMessageRenderer":{"id":"a","authorName":{"simpleText":"@ana"},
   "message":{"runs":[{"text":"hello "},{"emoji":{"emojiId":"x","shortcuts":[":smile:"]}},{"text":"!"}]}}}}}]}},
 {"replayChatItemAction":{"videoOffsetTimeMsec":"2000","actions":[{"addChatItemAction":{"item":{
   "liveChatPaidMessageRenderer":{"id":"b","authorName":{"simpleText":"@bo"},
   "purchaseAmountText":{"simpleText":"$5.00"},"message":{"runs":[{"text":"thanks"}]}}}}}]}},
 {"replayChatItemAction":{"videoOffsetTimeMsec":"2500","actions":[{"addChatItemAction":{"item":{
   "liveChatMembershipItemRenderer":{"id":"c","authorName":{"simpleText":"@cy"},
   "headerSubtext":{"runs":[{"text":"Welcome to "},{"text":"Club"},{"text":"!"}]}}}}}]}},
 {"replayChatItemAction":{"videoOffsetTimeMsec":"3000","actions":[{"addChatItemAction":{"item":{
   "liveChatPlaceholderItemRenderer":{"id":"d"}}}},{"addLiveChatTickerItemAction":{}}]}},
 {"replayChatItemAction":{"videoOffsetTimeMsec":"oops","actions":[{"addChatItemAction":{"item":{
   "liveChatTextMessageRenderer":{"id":"e","message":{"simpleText":"no time"}}}}}]}},
 {"clickTrackingParams":"x"}
],
"continuations":[{"liveChatReplayContinuationData":{"continuation":"op2w0w-_next%3D"}}]}}}`

func TestParseChatReplyReadsTheKindsItShows(t *testing.T) {
	page, err := parseChatReply([]byte(chatReplyJSON))
	if err != nil {
		t.Fatal(err)
	}

	type brief struct {
		id, author, text, sub string
		offset                time.Duration
		kind                  ChatKind
	}
	want := []brief{
		{"a", "@ana", "hello :smile:!", "", 1500 * time.Millisecond, ChatText},
		{"b", "@bo", "thanks", "", 2 * time.Second, ChatPaid},
		{"c", "@cy", "", "Welcome to Club!", 2500 * time.Millisecond, ChatMember},
	}
	if len(page.Messages) != len(want) {
		t.Fatalf("got %d messages, want %d: %+v", len(page.Messages), len(want), page.Messages)
	}
	for i, w := range want {
		m := page.Messages[i]
		got := brief{m.ID, m.Author, m.Text, m.Sub, m.Offset, m.Kind}
		if got != w {
			t.Errorf("message %d = %+v, want %+v", i, got, w)
		}
	}
	if page.Messages[1].Amount != "$5.00" {
		t.Errorf("the amount of a Super Chat: %q", page.Messages[1].Amount)
	}
	if page.Next != "op2w0w-_next%3D" {
		t.Errorf("Next = %q", page.Next)
	}
}

func TestParseChatReplyEndsWithoutANextToken(t *testing.T) {
	page, err := parseChatReply([]byte(`{"continuationContents":{"liveChatContinuation":{"actions":[]}}}`))
	if err != nil || len(page.Messages) != 0 || page.Next != "" {
		t.Fatalf("page = %+v, err = %v", page, err)
	}

	if _, err := parseChatReply([]byte("<html>not json")); err == nil {
		t.Error("a reply that is not JSON must be an error")
	}
}

func TestParseChatReplyRefusesAnOddNextToken(t *testing.T) {
	reply := `{"continuationContents":{"liveChatContinuation":{"continuations":[` +
		`{"liveChatReplayContinuationData":{"continuation":"a b&c"}}]}}}`
	if page, _ := parseChatReply([]byte(reply)); page.Next != "" {
		t.Errorf("an odd token must not be used, got %q", page.Next)
	}
}

func TestCleanChatRemovesWhatCouldAttackTheTerminal(t *testing.T) {
	for name, tc := range map[string]struct{ in, want string }{
		"escape sequence": {"hi \x1b[31mred\x1b]0;title\x07", "hi [31mred]0;title"},
		"newlines":        {"a\nb\r\nc\td", "a b  c d"},
		"bidi override":   {"abc\u202edef\u2066", "abcdef"},
		"zero width":      {"a\u200bb\ufeffc", "abc"},
		"c1 control":      {"a\u009bb", "ab"},
		"plain":           {"  Olá, mundo  ", "Olá, mundo"},
	} {
		if got := cleanChat(tc.in, 100); got != tc.want {
			t.Errorf("%s: cleanChat(%q) = %q, want %q", name, tc.in, got, tc.want)
		}
	}

	if got := cleanChat(strings.Repeat("é", 1000), 10); got != strings.Repeat("é", 10) {
		t.Errorf("the text must be cut to the limit, got %d runes", len([]rune(got)))
	}
}

func TestFetchChatAsksForTheOffsetOnlyWhenSeeking(t *testing.T) {
	old := postChat
	t.Cleanup(func() { postChat = old })

	var bodies []map[string]any
	postChat = func(_ context.Context, body []byte) ([]byte, error) {
		var parsed map[string]any
		if err := json.Unmarshal(body, &parsed); err != nil {
			t.Fatal(err)
		}
		bodies = append(bodies, parsed)

		return []byte(chatReplyJSON), nil
	}

	page, err := FetchChat(context.Background(), "tok-1", 90*time.Second, true)
	if err != nil || len(page.Messages) != 3 {
		t.Fatalf("page = %+v, err = %v", page, err)
	}
	if _, err := FetchChat(context.Background(), "tok-2", 0, false); err != nil {
		t.Fatal(err)
	}

	state, _ := bodies[0]["currentPlayerState"].(map[string]any)
	if bodies[0]["continuation"] != "tok-1" || state["playerOffsetMs"] != "90000" {
		t.Errorf("a seek must send the token and the offset: %v", bodies[0])
	}
	if _, has := bodies[1]["currentPlayerState"]; has || bodies[1]["continuation"] != "tok-2" {
		t.Errorf("following a page must not send an offset: %v", bodies[1])
	}
}

func TestFetchChatRefusesAnInvalidTokenBeforeTheRequest(t *testing.T) {
	old := postChat
	t.Cleanup(func() { postChat = old })
	postChat = func(context.Context, []byte) ([]byte, error) {
		t.Error("no request may be sent for an invalid token")
		return nil, nil
	}

	for _, token := range []string{"", "a b", "a&b", strings.Repeat("a", maxChatTokenLen+1)} {
		if _, err := FetchChat(context.Background(), token, 0, true); err == nil {
			t.Errorf("token %q should be refused", token)
		}
	}
}

// richReply is shaped like the real replies: a profile picture, badges, a
// standard emoji and an emoji of the channel (both are images), and the colors
// of a Super Chat. The pictures are from the hosts YouTube uses.
const richReply = `{"continuationContents":{"liveChatContinuation":{"actions":[{"replayChatItemAction":{"videoOffsetTimeMsec":"1000","actions":[{"addChatItemAction":{"item":{"liveChatTextMessageRenderer":{"id":"m1","authorName":{"simpleText":"@mod"},"authorPhoto":{"thumbnails":[{"url":"https://yt4.ggpht.com/p=s32-c-k","width":32},{"url":"https://yt4.ggpht.com/p=s64-c-k","width":64}]},"authorBadges":[{"liveChatAuthorBadgeRenderer":{"icon":{"iconType":"MODERATOR"},"tooltip":"Moderator"}},{"liveChatAuthorBadgeRenderer":{"customThumbnail":{"thumbnails":[{"url":"https://yt3.ggpht.com/b=s16-c-k","width":16},{"url":"https://yt3.ggpht.com/b=s32-c-k","width":32}]},"tooltip":"Member (2 years)"}},{"liveChatAuthorBadgeRenderer":{"icon":{"iconType":"SOMETHING_NEW"}}}],"message":{"runs":[{"text":"hi "},{"emoji":{"emojiId":"x","shortcuts":[":collision:"],"image":{"thumbnails":[{"url":"https://fonts.gstatic.com/s/e/notoemoji/15.1/1f4a5/72.png","width":0}]}}},{"text":" and "},{"emoji":{"emojiId":"UC/abc","shortcuts":[":face-blue:"],"image":{"thumbnails":[{"url":"https://yt3.ggpht.com/e=w24","width":24},{"url":"https://yt3.ggpht.com/e=w48","width":48}]}}}]}}}}}]}},{"replayChatItemAction":{"videoOffsetTimeMsec":"2000","actions":[{"addChatItemAction":{"item":{"liveChatTextMessageRenderer":{"id":"m2","authorName":{"simpleText":"@member"},"authorBadges":[{"liveChatAuthorBadgeRenderer":{"customThumbnail":{"thumbnails":[{"url":"https://yt3.ggpht.com/b=s16-c-k","width":16}]},"tooltip":"New member"}}],"message":{"runs":[{"text":"member here"}]}}}}}]}},{"replayChatItemAction":{"videoOffsetTimeMsec":"3000","actions":[{"addChatItemAction":{"item":{"liveChatPaidMessageRenderer":{"id":"m3","authorName":{"simpleText":"@rich"},"purchaseAmountText":{"simpleText":"$9.99"},"headerBackgroundColor":4294947584,"headerTextColor":3741319168,"bodyBackgroundColor":4294953512,"bodyTextColor":3741319168,"message":{"runs":[{"text":"keep it up"}]}}}}}]}},{"replayChatItemAction":{"videoOffsetTimeMsec":"4000","actions":[{"addChatItemAction":{"item":{"liveChatSponsorshipsGiftPurchaseAnnouncementRenderer":{"id":"m4","header":{"liveChatSponsorshipsHeaderRenderer":{"authorName":{"simpleText":"@gifter"},"primaryText":{"runs":[{"text":"Sent "},{"text":"5"},{"text":" gift memberships"}]},"authorBadges":[{"liveChatAuthorBadgeRenderer":{"icon":{"iconType":"OWNER"},"tooltip":"Owner"}}]}}}}}}]}}]}}}`

func TestParseChatReplyReadsPhotosBadgesAndEmojiImages(t *testing.T) {
	page, err := parseChatReply([]byte(richReply))
	if err != nil || len(page.Messages) != 4 {
		t.Fatalf("messages = %d, err = %v", len(page.Messages), err)
	}

	m := page.Messages[0]
	if m.Photo != "https://yt4.ggpht.com/p=s64-c-k" {
		t.Errorf("the biggest picture is wanted, got %q", m.Photo)
	}
	if m.Role != "moderator" || len(m.Badges) != 2 || m.Badges[0].Kind != "moderator" ||
		m.Badges[1].Kind != "member" || m.Badges[1].Image != "https://yt3.ggpht.com/b=s32-c-k" || m.Badges[1].Label != "Member (2 years)" {
		t.Errorf("a moderator who is a member (an unknown badge is left out): %+v", m.Badges)
	}
	if len(m.Runs) != 4 || m.Runs[0].Text != "hi " || m.Runs[1].Image != "https://fonts.gstatic.com/s/e/notoemoji/15.1/1f4a5/72.png" ||
		m.Runs[1].Alt != ":collision:" || m.Runs[3].Image != "https://yt3.ggpht.com/e=w48" || m.Runs[3].Alt != ":face-blue:" {
		t.Errorf("runs = %+v", m.Runs)
	}
	if m.Text != "hi :collision: and :face-blue:" {
		t.Errorf("plain text = %q", m.Text)
	}

	if page.Messages[1].Role != "member" || page.Messages[1].Badges[0].Label != "New member" {
		t.Errorf("a member: %+v", page.Messages[1])
	}

	paid := page.Messages[2]
	if paid.Kind != ChatPaid || paid.Amount != "$9.99" || paid.Colors.HeaderBackground != "rgba(255,179,0,1.00)" ||
		paid.Colors.HeaderText != "rgba(0,0,0,0.87)" || paid.Colors.BodyBackground != "rgba(255,202,40,1.00)" {
		t.Errorf("the colors of a Super Chat: %+v", paid)
	}

	gift := page.Messages[3]
	if gift.Kind != ChatGift || gift.Author != "@gifter" || gift.Sub != "Sent 5 gift memberships" || gift.Role != "owner" {
		t.Errorf("a gift: %+v", gift)
	}
}

func TestChatImageAllowedOnlyForYouTubesImageServers(t *testing.T) {
	for _, good := range []string{
		"https://yt3.ggpht.com/abc=s32-c-k",
		"https://yt4.ggpht.com/a/b?c=d",
		"https://fonts.gstatic.com/s/e/notoemoji/15.1/1f4a5/72.png",
		"https://YT3.GGPHT.COM/x",
		"https://yt3.ggpht.com:443/x",
	} {
		if !ChatImageAllowed(good) {
			t.Errorf("%q should be allowed", good)
		}
	}

	for _, bad := range []string{
		"",
		"http://yt3.ggpht.com/x", // not https
		"https://ggpht.com/x",    // not a host of the images
		"https://evil.example/x",
		"https://yt3.ggpht.com.evil.example/x", // the name only starts like it
		"https://evilggpht.com/x",              // ends with the name without the dot
		"https://yt3.ggpht.com@evil.example/x", // the real host is after the @
		"https://user:pw@yt3.ggpht.com/x",
		"https://yt3.ggpht.com:8443/x",
		"https://www.gstatic.com/x", // only fonts.gstatic.com
		"javascript:alert(1)",
		"data:image/png;base64,AAAA",
		"//yt3.ggpht.com/x",
		"https://yt3.ggpht.com/" + strings.Repeat("a", 600),
		"https://yt3.ggpht.com/ x",
	} {
		if ChatImageAllowed(bad) {
			t.Errorf("%q must not be allowed", bad)
		}
	}
}

func TestParseChatReplyDropsAPictureFromAnotherPlace(t *testing.T) {
	reply := `{"continuationContents":{"liveChatContinuation":{"actions":[{"replayChatItemAction":{"videoOffsetTimeMsec":"1","actions":[
	 {"addChatItemAction":{"item":{"liveChatTextMessageRenderer":{"id":"x","authorName":{"simpleText":"@a"},
	  "authorPhoto":{"thumbnails":[{"url":"https://evil.example/track.png","width":64}]},
	  "authorBadges":[{"liveChatAuthorBadgeRenderer":{"customThumbnail":{"thumbnails":[{"url":"http://yt3.ggpht.com/b","width":16}]},"tooltip":"Member"}}],
	  "message":{"runs":[{"emoji":{"emojiId":"e","shortcuts":[":e:"],"image":{"thumbnails":[{"url":"https://evil.example/e.png"}]}}}]}}}}}]}}]}}}`

	page, err := parseChatReply([]byte(reply))
	if err != nil || len(page.Messages) != 1 {
		t.Fatalf("messages = %d, err = %v", len(page.Messages), err)
	}
	m := page.Messages[0]
	if m.Photo != "" || m.Badges[0].Image != "" || m.Runs[0].Image != "" || m.Runs[0].Alt != ":e:" {
		t.Errorf("only the text may remain of what is not on YouTube's servers: %+v", m)
	}
}

func TestParseChatReplyKeepsTheSpacesBetweenPieces(t *testing.T) {
	reply := `{"continuationContents":{"liveChatContinuation":{"actions":[{"replayChatItemAction":{"videoOffsetTimeMsec":"1","actions":[
	 {"addChatItemAction":{"item":{"liveChatTextMessageRenderer":{"id":"x","message":{"runs":[
	  {"text":"  one "},{"emoji":{"emojiId":"e","shortcuts":[":e:"]}},{"text":" two  "}]}}}}}]}}]}}}`

	page, err := parseChatReply([]byte(reply))
	if err != nil || len(page.Messages) != 1 {
		t.Fatalf("messages = %d, err = %v", len(page.Messages), err)
	}
	runs := page.Messages[0].Runs
	if len(runs) != 3 || runs[0].Text != "one " || runs[2].Text != " two" {
		t.Errorf("runs = %q", runs)
	}
}

func TestChatColorFromAnARGBNumber(t *testing.T) {
	for argb, want := range map[uint32]string{
		0:          "",
		0xFFFF0000: "rgba(255,0,0,1.00)",
		0x80102030: "rgba(16,32,48,0.50)",
		0xDF000000: "rgba(0,0,0,0.87)",
	} {
		if got := chatColor(argb); got != want {
			t.Errorf("chatColor(%#x) = %q, want %q", argb, got, want)
		}
	}
}
