package youtube

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// The chat replay of a past stream is a list of messages, each with the time
// of the video where it appeared. YouTube's own replay window shows a message
// when the player reports that time, so a window without a player stays empty.
// ytwatch asks for the messages itself and shows them at the time of mpv.
const (
	chatURL = "https://www.youtube.com/youtubei/v1/live_chat/get_live_chat_replay?prettyPrint=false"

	// chatClientVersion is sent as the version of the web client. YouTube
	// accepts old versions for this request.
	chatClientVersion = "2.20240101.00.00"

	// Limits for what one reply may give. The text comes from strangers.
	maxChatReply   = 8 * 1024 * 1024
	maxChatPerPage = 500
	maxChatText    = 500
	maxChatAuthor  = 60
)

// ChatKind tells the kinds of chat message apart.
type ChatKind int

const (
	ChatText   ChatKind = iota // a normal message
	ChatPaid                   // a Super Chat: Text starts with the amount
	ChatMember                 // someone became a member
)

// ChatMessage is one message of a chat replay.
type ChatMessage struct {
	ID     string
	Offset time.Duration // where in the video the message appeared
	Author string
	Text   string
	Kind   ChatKind
}

// ChatPage is one reply of the chat replay: the messages of a few seconds of
// the video.
type ChatPage struct {
	Messages []ChatMessage
	// Next is the token for the messages after this page, or "" at the end.
	Next string
}

type chatRun struct {
	Text  string `json:"text"`
	Emoji struct {
		ID        string   `json:"emojiId"`
		Shortcuts []string `json:"shortcuts"`
	} `json:"emoji"`
}

type chatText struct {
	SimpleText string    `json:"simpleText"`
	Runs       []chatRun `json:"runs"`
}

func (t chatText) String() string {
	if t.SimpleText != "" {
		return t.SimpleText
	}

	var b strings.Builder
	for _, run := range t.Runs {
		switch {
		case run.Text != "":
			b.WriteString(run.Text)
		case len(run.Emoji.Shortcuts) > 0:
			b.WriteString(run.Emoji.Shortcuts[0])
		case run.Emoji.ID != "":
			b.WriteString(run.Emoji.ID)
		}
	}

	return b.String()
}

type chatRenderer struct {
	ID        string   `json:"id"`
	Author    chatText `json:"authorName"`
	Message   chatText `json:"message"`
	Amount    chatText `json:"purchaseAmountText"`
	HeaderSub chatText `json:"headerSubtext"`
}

type chatReply struct {
	Contents struct {
		Chat struct {
			Actions []struct {
				Replay struct {
					Offset  string `json:"videoOffsetTimeMsec"`
					Actions []struct {
						Add struct {
							Item struct {
								Text   chatRenderer `json:"liveChatTextMessageRenderer"`
								Paid   chatRenderer `json:"liveChatPaidMessageRenderer"`
								Member chatRenderer `json:"liveChatMembershipItemRenderer"`
							} `json:"item"`
						} `json:"addChatItemAction"`
					} `json:"actions"`
				} `json:"replayChatItemAction"`
			} `json:"actions"`
			Continuations []struct {
				Replay struct {
					Continuation string `json:"continuation"`
				} `json:"liveChatReplayContinuationData"`
			} `json:"continuations"`
		} `json:"liveChatContinuation"`
	} `json:"continuationContents"`
}

// FetchChat reads one page of the chat replay. token is the token of the watch
// page (ChatReplayToken) or the Next of an earlier page. With seek set, the
// page starts near offset: use it for the first page and after the video
// jumped. Without it, the page follows the one the token came from.
func FetchChat(ctx context.Context, token string, offset time.Duration, seek bool) (ChatPage, error) {
	if !validChatToken(token) {
		return ChatPage{}, fmt.Errorf("invalid chat token")
	}

	request := map[string]any{
		"context": map[string]any{
			"client": map[string]string{"clientName": "WEB", "clientVersion": chatClientVersion},
		},
		"continuation": token,
	}
	if seek {
		request["currentPlayerState"] = map[string]string{
			"playerOffsetMs": strconv.FormatInt(offset.Milliseconds(), 10),
		}
	}

	body, err := json.Marshal(request)
	if err != nil {
		return ChatPage{}, err
	}

	data, err := postChat(ctx, body)
	if err != nil {
		return ChatPage{}, err
	}

	return parseChatReply(data)
}

// parseChatReply reads the messages and the next token of a reply.
func parseChatReply(data []byte) (ChatPage, error) {
	var reply chatReply
	if err := json.Unmarshal(data, &reply); err != nil {
		return ChatPage{}, fmt.Errorf("read chat reply: %w", err)
	}

	var page ChatPage
	for _, action := range reply.Contents.Chat.Actions {
		offset, err := strconv.ParseInt(action.Replay.Offset, 10, 64)
		if err != nil || offset < 0 {
			continue
		}

		for _, inner := range action.Replay.Actions {
			if len(page.Messages) >= maxChatPerPage {
				break
			}

			if message, ok := chatMessage(inner.Add.Item.Text, inner.Add.Item.Paid, inner.Add.Item.Member); ok {
				message.Offset = time.Duration(offset) * time.Millisecond
				page.Messages = append(page.Messages, message)
			}
		}
	}

	for _, next := range reply.Contents.Chat.Continuations {
		if token := next.Replay.Continuation; validChatToken(token) {
			page.Next = token
			break
		}
	}

	return page, nil
}

// chatMessage turns the first renderer that is set into a message. The other
// kinds of chat item (ticker, placeholder, notices) are not shown.
func chatMessage(text, paid, member chatRenderer) (ChatMessage, bool) {
	switch {
	case text.ID != "":
		return ChatMessage{
			ID:     text.ID,
			Author: cleanChat(text.Author.String(), maxChatAuthor),
			Text:   cleanChat(text.Message.String(), maxChatText),
			Kind:   ChatText,
		}, true

	case paid.ID != "":
		message := strings.TrimSpace(paid.Amount.String() + " " + paid.Message.String())
		return ChatMessage{
			ID:     paid.ID,
			Author: cleanChat(paid.Author.String(), maxChatAuthor),
			Text:   cleanChat(message, maxChatText),
			Kind:   ChatPaid,
		}, true

	case member.ID != "":
		return ChatMessage{
			ID:     member.ID,
			Author: cleanChat(member.Author.String(), maxChatAuthor),
			Text:   cleanChat(member.HeaderSub.String(), maxChatText),
			Kind:   ChatMember,
		}, true
	}

	return ChatMessage{}, false
}

// cleanChat makes text from a stranger safe to print in a terminal. Control
// characters would let it send escape sequences to the terminal, and the
// direction and zero-width marks would let it reorder or hide what is shown.
func cleanChat(s string, limit int) string {
	var b strings.Builder
	count := 0

	for _, r := range s {
		switch {
		case r == '\n' || r == '\r' || r == '\t':
			r = ' '
		case r < 0x20 || r == 0x7f || (r >= 0x80 && r < 0xa0):
			continue
		case r == 0xfeff || (r >= 0x200b && r <= 0x200f) ||
			(r >= 0x202a && r <= 0x202e) || (r >= 0x2060 && r <= 0x2064) ||
			(r >= 0x2066 && r <= 0x2069):
			continue
		}

		if count == limit {
			break
		}

		b.WriteRune(r)
		count++
	}

	return strings.TrimSpace(b.String())
}

func validChatToken(token string) bool {
	return token != "" && len(token) <= maxChatTokenLen && chatTokenPattern.MatchString(token)
}

// postChat sends a chat request. It is a variable so that tests do not use the
// network.
var postChat = func(ctx context.Context, body []byte) ([]byte, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, chatURL, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	request.Header.Set("User-Agent", "Mozilla/5.0 (X11; Linux x86_64; rv:130.0) Gecko/20100101 Firefox/130.0")
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Cookie", "SOCS=CAI")

	client := &http.Client{Timeout: 15 * time.Second}
	response, err := client.Do(request)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()

	if response.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("youtube returned %s", response.Status)
	}

	return io.ReadAll(io.LimitReader(response.Body, maxChatReply))
}
