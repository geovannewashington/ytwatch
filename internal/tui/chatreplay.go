package tui

import (
	"time"

	"github.com/geovannewashington/ytwatch/internal/chat"
	"github.com/geovannewashington/ytwatch/internal/media"
	"github.com/geovannewashington/ytwatch/internal/player"
)

// chatLinger is how long the chat page stays served after mpv ended, so the
// open window can say so instead of showing a broken connection.
const chatLinger = 15 * time.Second

// openChatWindow is a variable so that tests do not start a browser.
var openChatWindow = player.OpenChatWindow

// openChatReplay shows the chat replay of a past stream that is playing in the
// chat window, like the live chat. The page follows the time of the video in
// mpv (see package chat). Nothing happens when the video has no chat replay,
// mpv has no control socket, or the chat is turned off.
func (m *Model) openChatReplay(v media.Video, socket string) error {
	if !m.cfg.Chat.Enabled || socket == "" || !v.HasChatReplay() || m.chatOpened[v.ID] {
		return nil
	}

	session := chat.NewSession(chat.Options{Title: sanitize(v.Title), Token: v.ChatReplayToken, Socket: socket})

	server, err := chat.Serve(session)
	if err != nil {
		return err
	}

	if err := openChatWindow(server.URL, m.cfg); err != nil {
		server.Close()
		return err
	}

	m.chats[v.ID] = session

	ctx := m.ctx
	go func() {
		session.Run(ctx)

		select {
		case <-ctx.Done():
		case <-time.After(chatLinger):
		}
		server.Close()
	}()

	m.chatOpened[v.ID] = true

	return nil
}
