package youtube

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"slices"
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
	maxChatRuns    = 80
	maxChatBadges  = 4
)

// ChatKind tells the kinds of chat message apart.
type ChatKind int

const (
	ChatText   ChatKind = iota // a normal message
	ChatPaid                   // a Super Chat: Amount and Colors are set
	ChatMember                 // someone became a member: Sub says what
	ChatGift                   // someone gave memberships: Sub says what
)

// ChatRun is a piece of a message: text, or an emoji that is drawn as an image.
type ChatRun struct {
	Text  string // the text, when it is a piece of text
	Image string // the image of an emoji, a URL that ChatImageAllowed accepts
	Alt   string // what to show instead of the image: ":smile:" or the emoji
}

// ChatBadge is a mark next to the name of the author.
type ChatBadge struct {
	Kind  string // "member", "moderator", "verified" or "owner"
	Label string // for example "Member (2 years)"
	Image string // the picture of a member badge, or ""
}

// ChatColors are the colors of a Super Chat, as CSS colors.
type ChatColors struct {
	HeaderBackground, HeaderText, BodyBackground, BodyText string
}

// ChatMessage is one message of a chat replay.
type ChatMessage struct {
	ID     string
	Offset time.Duration // where in the video the message appeared
	Author string
	Photo  string      // the profile picture, a URL that ChatImageAllowed accepts
	Role   string      // "owner", "moderator", "member", "verified" or ""
	Badges []ChatBadge //
	Runs   []ChatRun   // the message
	Text   string      // the message as plain text
	Amount string      // of a Super Chat
	Sub    string      // what a new member or a gift says
	Colors ChatColors  // of a Super Chat
	Kind   ChatKind
}

// ChatPage is one reply of the chat replay: the messages of a few seconds of
// the video.
type ChatPage struct {
	Messages []ChatMessage
	// Next is the token for the messages after this page, or "" at the end.
	Next string
}

type chatThumbnail struct {
	URL    string `json:"url"`
	Width  int    `json:"width"`
	Height int    `json:"height"`
}

type chatImage struct {
	Thumbnails []chatThumbnail `json:"thumbnails"`
}

type chatRun struct {
	Text  string `json:"text"`
	Emoji struct {
		ID        string    `json:"emojiId"`
		Shortcuts []string  `json:"shortcuts"`
		Image     chatImage `json:"image"`
	} `json:"emoji"`
}

type chatText struct {
	SimpleText string    `json:"simpleText"`
	Runs       []chatRun `json:"runs"`
}

// String is the text as one string: emoji are their shortcuts.
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

type chatBadgeRenderer struct {
	Renderer struct {
		Icon struct {
			Type string `json:"iconType"`
		} `json:"icon"`
		Custom  chatImage `json:"customThumbnail"`
		Tooltip string    `json:"tooltip"`
	} `json:"liveChatAuthorBadgeRenderer"`
}

type chatRenderer struct {
	ID            string              `json:"id"`
	Author        chatText            `json:"authorName"`
	Photo         chatImage           `json:"authorPhoto"`
	Badges        []chatBadgeRenderer `json:"authorBadges"`
	Message       chatText            `json:"message"`
	Amount        chatText            `json:"purchaseAmountText"`
	HeaderSub     chatText            `json:"headerSubtext"`
	HeaderPrimary chatText            `json:"headerPrimaryText"`
	HeaderBG      uint32              `json:"headerBackgroundColor"`
	HeaderText    uint32              `json:"headerTextColor"`
	BodyBG        uint32              `json:"bodyBackgroundColor"`
	BodyText      uint32              `json:"bodyTextColor"`
}

// chatGift is the announcement of memberships that someone gave. Its author
// and text are in the header.
type chatGift struct {
	ID     string `json:"id"`
	Header struct {
		Renderer struct {
			Author  chatText            `json:"authorName"`
			Photo   chatImage           `json:"authorPhoto"`
			Primary chatText            `json:"primaryText"`
			Badges  []chatBadgeRenderer `json:"authorBadges"`
		} `json:"liveChatSponsorshipsHeaderRenderer"`
	} `json:"header"`
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
								Gift   chatGift     `json:"liveChatSponsorshipsGiftPurchaseAnnouncementRenderer"`
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

			item := inner.Add.Item
			if message, ok := chatMessage(item.Text, item.Paid, item.Member, item.Gift); ok {
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
func chatMessage(text, paid, member chatRenderer, gift chatGift) (ChatMessage, bool) {
	switch {
	case text.ID != "":
		return chatBuild(text, ChatText), true

	case paid.ID != "":
		message := chatBuild(paid, ChatPaid)
		message.Amount = cleanChat(paid.Amount.String(), maxChatAuthor)
		message.Colors = ChatColors{
			HeaderBackground: chatColor(paid.HeaderBG), HeaderText: chatColor(paid.HeaderText),
			BodyBackground: chatColor(paid.BodyBG), BodyText: chatColor(paid.BodyText),
		}

		return message, true

	case member.ID != "":
		message := chatBuild(member, ChatMember)
		// "New member" or "Member for 2 months", then "Welcome to X!".
		var parts []string
		for _, part := range []string{member.HeaderPrimary.String(), member.HeaderSub.String()} {
			if part = strings.TrimSpace(part); part != "" {
				parts = append(parts, part)
			}
		}
		message.Sub = cleanChat(strings.Join(parts, " · "), maxChatText)

		return message, true

	case gift.ID != "":
		header := gift.Header.Renderer
		message := chatBuild(chatRenderer{
			ID: gift.ID, Author: header.Author, Photo: header.Photo, Badges: header.Badges,
		}, ChatGift)
		message.Sub = cleanChat(header.Primary.String(), maxChatText)

		return message, true
	}

	return ChatMessage{}, false
}

// chatBuild fills what every kind of message has.
func chatBuild(r chatRenderer, kind ChatKind) ChatMessage {
	message := ChatMessage{
		ID:     r.ID,
		Author: cleanChat(r.Author.String(), maxChatAuthor),
		Photo:  chatPhoto(r.Photo),
		Kind:   kind,
	}

	message.Badges, message.Role = chatBadges(r.Badges)
	message.Runs = chatRuns(r.Message)
	message.Text = cleanChat(r.Message.String(), maxChatText)

	return message
}

// chatBadges reads the badges of an author, and the role they give. An owner
// outranks a moderator, who outranks a member.
func chatBadges(badges []chatBadgeRenderer) ([]ChatBadge, string) {
	var out []ChatBadge
	rank := map[string]int{"owner": 4, "moderator": 3, "member": 2, "verified": 1}
	role := ""

	for _, badge := range badges {
		renderer := badge.Renderer

		var kind string
		switch strings.ToUpper(renderer.Icon.Type) {
		case "OWNER":
			kind = "owner"
		case "MODERATOR":
			kind = "moderator"
		case "VERIFIED":
			kind = "verified"
		default:
			if len(renderer.Custom.Thumbnails) == 0 {
				continue // a mark that ytwatch does not know how to draw
			}
			kind = "member"
		}

		if len(out) < maxChatBadges {
			out = append(out, ChatBadge{
				Kind: kind, Label: cleanChat(renderer.Tooltip, maxChatAuthor), Image: chatImageURL(renderer.Custom),
			})
		}
		if rank[kind] > rank[role] {
			role = kind
		}
	}

	return out, role
}

// chatRuns turns the text of a message into pieces that are text or an emoji.
func chatRuns(t chatText) []ChatRun {
	if t.SimpleText != "" {
		t.Runs = []chatRun{{Text: t.SimpleText}}
	}

	var runs []ChatRun
	left := maxChatText

	for _, run := range t.Runs {
		if len(runs) >= maxChatRuns || left <= 0 {
			break
		}

		switch {
		case run.Text != "":
			text := cleanRun(run.Text, left)
			left -= len([]rune(text))
			if text != "" {
				runs = append(runs, ChatRun{Text: text})
			}

		default:
			alt := run.Emoji.ID
			if len(run.Emoji.Shortcuts) > 0 {
				alt = run.Emoji.Shortcuts[0]
			}
			if alt = cleanRun(alt, maxChatAuthor); alt == "" {
				continue
			}

			left -= 2
			runs = append(runs, ChatRun{Image: chatImageURL(run.Emoji.Image), Alt: alt})
		}
	}

	// The spaces at the two ends of the message are not part of it.
	if len(runs) > 0 && runs[0].Image == "" {
		runs[0].Text = strings.TrimLeft(runs[0].Text, " ")
	}
	if last := len(runs) - 1; last >= 0 && runs[last].Image == "" {
		runs[last].Text = strings.TrimRight(runs[last].Text, " ")
	}

	return slices.DeleteFunc(runs, func(r ChatRun) bool { return r.Image == "" && r.Text == "" && r.Alt == "" })
}

// chatPhoto is the profile picture of an author.
func chatPhoto(image chatImage) string { return chatImageURL(image) }

// chatImageURL picks the biggest picture of image that ChatImageAllowed accepts.
func chatImageURL(image chatImage) string {
	best, bestWidth := "", -1
	for _, thumbnail := range image.Thumbnails {
		if thumbnail.Width > bestWidth && ChatImageAllowed(thumbnail.URL) {
			best, bestWidth = thumbnail.URL, thumbnail.Width
		}
	}

	return best
}

// ChatImageHosts are the places that chat pictures may come from, as the
// sources of a Content-Security-Policy: profile pictures, member badges and the
// emoji of a channel are on YouTube's image servers, and the standard emoji on
// Google's font server. Nothing else is shown.
var ChatImageHosts = []string{"https://*.ggpht.com", "https://fonts.gstatic.com"}

// ChatImageAllowed reports whether raw is a picture that may be shown: an https
// address on one of ChatImageHosts, with nothing that hides another place in
// it. The address comes from a stranger's profile, and the browser loads it.
func ChatImageAllowed(raw string) bool {
	if len(raw) == 0 || len(raw) > 512 {
		return false
	}

	// A space, a quote or a bracket has no place in these addresses.
	for _, r := range raw {
		if r <= ' ' || r == 0x7f || strings.ContainsRune("\"'<>\\`", r) {
			return false
		}
	}

	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "https" || u.User != nil || (u.Port() != "" && u.Port() != "443") {
		return false
	}

	host := strings.ToLower(u.Hostname())

	return host == "fonts.gstatic.com" || (strings.HasSuffix(host, ".ggpht.com") && len(host) > len(".ggpht.com"))
}

// chatColor turns the ARGB number of a color into a CSS color, or "" when it
// is not set.
func chatColor(argb uint32) string {
	if argb == 0 {
		return ""
	}

	alpha := float64(argb>>24) / 255

	return fmt.Sprintf("rgba(%d,%d,%d,%.2f)", argb>>16&0xff, argb>>8&0xff, argb&0xff, alpha)
}

// cleanRun is cleanChat for a piece of a message: it keeps the spaces at the
// ends, which separate it from the pieces next to it.
func cleanRun(s string, limit int) string {
	return cleanChatKeep(s, limit, false)
}

// cleanChat makes text from a stranger safe to print in a terminal. Control
// characters would let it send escape sequences to the terminal, and the
// direction and zero-width marks would let it reorder or hide what is shown.
func cleanChat(s string, limit int) string { return cleanChatKeep(s, limit, true) }

func cleanChatKeep(s string, limit int, trim bool) string {
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

	if trim {
		return strings.TrimSpace(b.String())
	}

	return b.String()
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
