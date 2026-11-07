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

	want := []ChatMessage{
		{ID: "a", Offset: 1500 * time.Millisecond, Author: "@ana", Text: "hello :smile:!", Kind: ChatText},
		{ID: "b", Offset: 2 * time.Second, Author: "@bo", Text: "$5.00 thanks", Kind: ChatPaid},
		{ID: "c", Offset: 2500 * time.Millisecond, Author: "@cy", Text: "Welcome to Club!", Kind: ChatMember},
	}
	if len(page.Messages) != len(want) {
		t.Fatalf("got %d messages, want %d: %+v", len(page.Messages), len(want), page.Messages)
	}
	for i, message := range want {
		if page.Messages[i] != message {
			t.Errorf("message %d = %+v, want %+v", i, page.Messages[i], message)
		}
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
