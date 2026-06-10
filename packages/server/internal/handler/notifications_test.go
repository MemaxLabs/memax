package handler

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/MemaxLabs/memax/packages/server/internal/model"
)

func TestAuthorizeResolveActionTopicMergeRequiresTopicWrite(t *testing.T) {
	s := newHubsTestStore()
	h := NewNotificationsHandler(s, nil)
	now := time.Now()
	hubID := "11111111-1111-1111-1111-111111111111"
	userID := "22222222-2222-2222-2222-222222222222"

	if err := s.CreateHub(&model.Hub{
		ID:                     hubID,
		Name:                   "Team",
		Slug:                   "team",
		HubType:                "team",
		OwnerID:                "owner",
		AllowContributorTopics: false,
		AllowContributorDreams: true,
		CreatedAt:              now,
		UpdatedAt:              now,
	}); err != nil {
		t.Fatal(err)
	}
	if err := s.AddHubMember(hubID, userID, "contributor"); err != nil {
		t.Fatal(err)
	}

	payload, err := json.Marshal(model.ReviewTopicMergePayload{
		TargetTopic: model.ReviewTopicRef{ID: "target", Name: "Target"},
		SourceTopics: []model.ReviewTopicRef{
			{ID: "source", Name: "Source"},
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	err = h.authorizeResolveAction(context.Background(), userID, []string{hubID}, false, &model.Notification{
		HubID:   hubID,
		Kind:    model.NotificationKindReviewTopicMerge,
		Payload: payload,
	}, "merge")
	if !errors.Is(err, errNotificationActionForbidden) {
		t.Fatalf("expected forbidden, got %v", err)
	}
}

func TestAuthorizeResolveActionTopicRestructureAllowsContributorWithTopicWrite(t *testing.T) {
	s := newHubsTestStore()
	h := NewNotificationsHandler(s, nil)
	now := time.Now()
	hubID := "11111111-1111-1111-1111-111111111112"
	userID := "22222222-2222-2222-2222-222222222223"

	if err := s.CreateHub(&model.Hub{
		ID:                     hubID,
		Name:                   "Team",
		Slug:                   "team-2",
		HubType:                "team",
		OwnerID:                "owner",
		AllowContributorTopics: true,
		AllowContributorDreams: true,
		CreatedAt:              now,
		UpdatedAt:              now,
	}); err != nil {
		t.Fatal(err)
	}
	if err := s.AddHubMember(hubID, userID, "contributor"); err != nil {
		t.Fatal(err)
	}

	payload, err := json.Marshal(model.ReviewTopicRestructurePayload{
		ParentTopic: model.ReviewTopicRef{ID: "parent", Name: "Parent"},
		ChildTopic:  model.ReviewTopicRef{ID: "child", Name: "Child"},
	})
	if err != nil {
		t.Fatal(err)
	}

	if err := h.authorizeResolveAction(context.Background(), userID, []string{hubID}, false, &model.Notification{
		HubID:   hubID,
		Kind:    model.NotificationKindReviewTopicRestructure,
		Payload: payload,
	}, "apply"); err != nil {
		t.Fatalf("expected apply allowed, got %v", err)
	}
}

func TestAuthorizeResolveActionContradictionRequiresDeletePermission(t *testing.T) {
	s := newHubsTestStore()
	h := NewNotificationsHandler(s, nil)
	now := time.Now()
	hubID := "11111111-1111-1111-1111-111111111113"
	userID := "22222222-2222-2222-2222-222222222224"
	otherOwnerID := "33333333-3333-3333-3333-333333333333"

	if err := s.CreateHub(&model.Hub{
		ID:                      hubID,
		Name:                    "Team",
		Slug:                    "team-3",
		HubType:                 "team",
		OwnerID:                 "owner",
		AllowContributorTopics:  true,
		AllowContributorDreams:  true,
		ContributorDeletePolicy: "own",
		CreatedAt:               now,
		UpdatedAt:               now,
	}); err != nil {
		t.Fatal(err)
	}
	if err := s.AddHubMember(hubID, userID, "contributor"); err != nil {
		t.Fatal(err)
	}
	if err := s.AddHubMember(hubID, otherOwnerID, "contributor"); err != nil {
		t.Fatal(err)
	}
	if err := s.CreateMemory(&model.Memory{
		ID:              "aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa",
		HubID:           hubID,
		OwnerID:         otherOwnerID,
		Title:           "B",
		Content:         "content",
		ContentType:     "markdown",
		ContentHash:     "hash-b",
		Kind:            model.MemoryKindSemantic,
		Stability:       model.MemoryStabilityEvolving,
		RetrievalWeight: 1,
		Source:          "test",
		State:           "active",
		CreatedAt:       now,
		UpdatedAt:       now,
		AccessedAt:      now,
	}); err != nil {
		t.Fatal(err)
	}

	payload, err := json.Marshal(model.ReviewContradictionPayload{
		MemoryA: model.ReviewMemoryRef{ID: "bbbbbbbb-bbbb-bbbb-bbbb-bbbbbbbbbbbb", Title: "A"},
		MemoryB: model.ReviewMemoryRef{ID: "aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa", Title: "B"},
	})
	if err != nil {
		t.Fatal(err)
	}

	err = h.authorizeResolveAction(context.Background(), userID, []string{hubID}, false, &model.Notification{
		HubID:   hubID,
		Kind:    model.NotificationKindReviewContradiction,
		Payload: payload,
	}, "keep_a")
	if !errors.Is(err, errNotificationActionForbidden) {
		t.Fatalf("expected forbidden, got %v", err)
	}
}

func TestAuthorizeResolveActionContradictionAllowsDismissWithoutDeletePermission(t *testing.T) {
	s := newHubsTestStore()
	h := NewNotificationsHandler(s, nil)

	if err := h.authorizeResolveAction(context.Background(), "user", nil, false, &model.Notification{
		HubID: "hub",
		Kind:  model.NotificationKindReviewContradiction,
	}, "dismiss"); err != nil {
		t.Fatalf("expected dismiss allowed, got %v", err)
	}
}
