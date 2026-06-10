package queue

import (
	"context"
	"testing"
	"time"

	sdkmodel "github.com/MemaxLabs/memax-go-agent-sdk/model"
	"github.com/google/uuid"

	"github.com/MemaxLabs/memax/packages/server/internal/model"
)

func TestChatSDKSessionStorePreloadsPriorTranscriptOnly(t *testing.T) {
	t.Parallel()

	chatSession := &model.ChatSession{
		ID:        uuid.NewString(),
		CreatedAt: time.Now().UTC(),
	}
	history := []model.ChatMessage{
		{
			ID:       uuid.NewString(),
			Sequence: 1,
			Role:     model.ChatMessageRoleUser,
			Status:   model.ChatMessageStatusCompleted,
			Content:  "first question",
		},
		{
			ID:       uuid.NewString(),
			Sequence: 2,
			Role:     model.ChatMessageRoleAssistant,
			Status:   model.ChatMessageStatusCompleted,
			Content:  "first answer",
		},
		{
			ID:       uuid.NewString(),
			Sequence: 3,
			Role:     model.ChatMessageRoleUser,
			Status:   model.ChatMessageStatusCompleted,
			Content:  "current question",
		},
		{
			ID:       uuid.NewString(),
			Sequence: 4,
			Role:     model.ChatMessageRoleAssistant,
			Status:   model.ChatMessageStatusQueued,
			Content:  "",
		},
	}

	store := newChatSDKSessionStore(chatSession, history, 3)
	got, err := store.Messages(context.Background(), chatSession.ID)
	if err != nil {
		t.Fatalf("Messages: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("len(messages) = %d, want 2", len(got))
	}
	if got[0].Role != sdkmodel.RoleUser || got[0].PlainText() != "first question" {
		t.Fatalf("first message = %#v, want prior user question", got[0])
	}
	if got[1].Role != sdkmodel.RoleAssistant || got[1].PlainText() != "first answer" {
		t.Fatalf("second message = %#v, want prior assistant answer", got[1])
	}
}

func TestChatSDKSessionStoreAppendsCurrentRunMessages(t *testing.T) {
	t.Parallel()

	chatSession := &model.ChatSession{ID: uuid.NewString(), CreatedAt: time.Now().UTC()}
	store := newChatSDKSessionStore(chatSession, nil, 0)

	if _, err := store.Get(context.Background(), chatSession.ID); err != nil {
		t.Fatalf("Get: %v", err)
	}
	if err := store.Append(context.Background(), chatSession.ID, sdkmodel.Message{
		Role: sdkmodel.RoleUser,
		Content: []sdkmodel.ContentBlock{
			{Type: sdkmodel.ContentText, Text: "current question"},
		},
	}); err != nil {
		t.Fatalf("Append: %v", err)
	}
	got, err := store.Messages(context.Background(), chatSession.ID)
	if err != nil {
		t.Fatalf("Messages: %v", err)
	}
	if len(got) != 1 || got[0].PlainText() != "current question" {
		t.Fatalf("messages = %#v, want current question", got)
	}
	if got[0].ID == "" {
		t.Fatalf("appended message ID is empty")
	}
}
