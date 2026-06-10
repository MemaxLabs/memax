package handler

import (
	"encoding/json"
	"testing"
)

func TestValidateCampaignKindAndContentEmailAnnouncementRequiresAllFields(t *testing.T) {
	cases := []struct {
		name    string
		content string
		wantOK  bool
	}{
		{
			name:    "valid",
			content: `{"subject":"Hi","body_html":"<p>hi</p>","body_text":"hi"}`,
			wantOK:  true,
		},
		{
			name:    "missing subject",
			content: `{"body_html":"<p>hi</p>","body_text":"hi"}`,
			wantOK:  false,
		},
		{
			name:    "missing body_html",
			content: `{"subject":"Hi","body_text":"hi"}`,
			wantOK:  false,
		},
		{
			name:    "missing body_text",
			content: `{"subject":"Hi","body_html":"<p>hi</p>"}`,
			wantOK:  false,
		},
		{
			name:    "full document body_html rejected",
			content: `{"subject":"Hi","body_html":"<!DOCTYPE html><html><body>hi</body></html>","body_text":"hi"}`,
			wantOK:  false,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			msg := validateCampaignKindAndContent("email_announcement", json.RawMessage(c.content))
			if c.wantOK && msg != "" {
				t.Fatalf("expected OK, got error: %s", msg)
			}
			if !c.wantOK && msg == "" {
				t.Fatalf("expected rejection, got OK")
			}
		})
	}
}

func TestValidateChannelKindCompat(t *testing.T) {
	cases := []struct {
		channel, kind string
		wantOK        bool
	}{
		{"inapp", "system_notice", true},
		{"inapp", "gift_invite_link", true},
		{"email", "email_announcement", true},
		{"email", "system_notice", false},      // wrong channel for in-app kind
		{"inapp", "email_announcement", false}, // wrong channel for email kind
	}
	for _, c := range cases {
		t.Run(c.channel+"_"+c.kind, func(t *testing.T) {
			msg := validateChannelKindCompat(c.channel, c.kind)
			if c.wantOK && msg != "" {
				t.Fatalf("expected OK, got: %s", msg)
			}
			if !c.wantOK && msg == "" {
				t.Fatalf("expected rejection, got OK")
			}
		})
	}
}
