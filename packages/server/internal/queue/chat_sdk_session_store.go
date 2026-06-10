package queue

import (
	"context"
	"fmt"
	"sync"
	"time"

	sdkmodel "github.com/MemaxLabs/memax-go-agent-sdk/model"
	sdksession "github.com/MemaxLabs/memax-go-agent-sdk/session"
	"github.com/google/uuid"

	"github.com/MemaxLabs/memax/packages/server/internal/model"
)

const chatSDKTranscriptWindow = 80

type chatSDKSessionStore struct {
	mu       sync.Mutex
	session  sdksession.Session
	messages []sdkmodel.Message
}

func newChatSDKSessionStore(chatSession *model.ChatSession, history []model.ChatMessage, beforeSequence int) *chatSDKSessionStore {
	session := sdksession.Session{ID: chatSession.ID, CreatedAt: chatSession.CreatedAt}
	if session.CreatedAt.IsZero() {
		session.CreatedAt = time.Now().UTC()
	}

	messages := make([]sdkmodel.Message, 0, len(history))
	for _, msg := range history {
		if beforeSequence > 0 && msg.Sequence >= beforeSequence {
			continue
		}
		sdkMsg, ok := chatMessageToSDKMessage(msg)
		if ok {
			messages = append(messages, sdkMsg)
		}
	}

	return &chatSDKSessionStore{
		session:  session,
		messages: sdkmodel.CloneMessages(messages),
	}
}

func (s *chatSDKSessionStore) Create(context.Context) (sdksession.Session, error) {
	return sdksession.Session{}, fmt.Errorf("chat SDK session store requires an existing chat session id")
}

func (s *chatSDKSessionStore) Get(_ context.Context, id string) (sdksession.Session, error) {
	if id != s.session.ID {
		return sdksession.Session{}, fmt.Errorf("unknown session: %s", id)
	}
	return s.session, nil
}

func (s *chatSDKSessionStore) Append(_ context.Context, id string, msg sdkmodel.Message) error {
	if id != s.session.ID {
		return fmt.Errorf("unknown session: %s", id)
	}
	if msg.ID == "" {
		msg.ID = uuid.NewString()
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.messages = append(s.messages, sdkmodel.CloneMessage(msg))
	return nil
}

func (s *chatSDKSessionStore) Messages(_ context.Context, id string) ([]sdkmodel.Message, error) {
	if id != s.session.ID {
		return nil, fmt.Errorf("unknown session: %s", id)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return sdkmodel.CloneMessages(s.messages), nil
}

func chatMessageToSDKMessage(msg model.ChatMessage) (sdkmodel.Message, bool) {
	if msg.Content == "" {
		return sdkmodel.Message{}, false
	}

	var role sdkmodel.Role
	switch msg.Role {
	case model.ChatMessageRoleUser:
		role = sdkmodel.RoleUser
	case model.ChatMessageRoleAssistant:
		if !isTerminalChatTranscriptStatus(msg.Status) {
			return sdkmodel.Message{}, false
		}
		role = sdkmodel.RoleAssistant
	default:
		return sdkmodel.Message{}, false
	}

	// Stamp the wall-clock time on each historical message so the
	// model can reason about WHEN things were said relative to the
	// "Current time" exposed in the system prompt. Absolute UTC is
	// unambiguous and cheap (~25 chars/message); the prompt's
	// transcript window is 80 messages so the overall overhead is
	// well under 1k extra tokens. Zero-time messages are stamped as
	// "unknown time" rather than skipping the prefix entirely so the
	// format stays uniform.
	content := msg.Content
	if !msg.CreatedAt.IsZero() {
		content = fmt.Sprintf("[%s] %s",
			msg.CreatedAt.UTC().Format("2006-01-02 15:04 UTC"),
			content)
	}

	return sdkmodel.Message{
		ID:   msg.ID,
		Role: role,
		Content: []sdkmodel.ContentBlock{
			{Type: sdkmodel.ContentText, Text: content},
		},
	}, true
}

func isTerminalChatTranscriptStatus(status string) bool {
	switch status {
	case model.ChatMessageStatusCompleted,
		model.ChatMessageStatusPartialFailed,
		model.ChatMessageStatusFailed,
		model.ChatMessageStatusCanceled:
		return true
	default:
		return false
	}
}
