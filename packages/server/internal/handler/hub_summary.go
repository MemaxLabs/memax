package handler

import (
	"fmt"
	"hash/fnv"
	"net/http"
	"time"

	"github.com/MemaxLabs/memax/packages/server/internal/model"
)

const (
	hubInboxOverflowThreshold = 20
	hubReturnAfterAbsenceDays = 7
	hubMembersPreviewLimit    = 3
)

func getHubTimeBucket(now time.Time) model.HubTimeBucket {
	hour := now.Hour()
	switch {
	case hour < 5:
		return model.HubTimeBucketDeepNight
	case hour < 12:
		return model.HubTimeBucketMorning
	case hour < 14:
		return model.HubTimeBucketNoon
	case hour < 18:
		return model.HubTimeBucketAfternoon
	case hour < 22:
		return model.HubTimeBucketEvening
	default:
		return model.HubTimeBucketLateNight
	}
}

func pickHubGreeting(keys []model.HubGreetingKey, seed string) model.HubGreetingKey {
	if len(keys) == 0 {
		return model.HubGreetingKeyMorningCleanA
	}
	if len(keys) == 1 {
		return keys[0]
	}
	hasher := fnv.New32a()
	_, _ = hasher.Write([]byte(seed))
	return keys[int(hasher.Sum32())%len(keys)]
}

func resolveHubHeaderState(
	stats model.HubSummaryStats,
	lastVisit *model.HubVisit,
	now time.Time,
) model.HubHeaderState {
	bucket := getHubTimeBucket(now)
	isFirstTime := stats.Memories == 0 && stats.Topics == 0
	if isFirstTime {
		return model.HubHeaderStateFirstTime
	}
	if stats.PendingReview > 0 {
		return model.HubHeaderStateReviewNeeded
	}
	if stats.DreamTopics > 0 {
		return model.HubHeaderStateDreamDeltas
	}
	if stats.Inbox >= hubInboxOverflowThreshold {
		return model.HubHeaderStateInboxOverflow
	}
	if lastVisit != nil && now.Sub(lastVisit.LastVisitedAt) > hubReturnAfterAbsenceDays*24*time.Hour {
		return model.HubHeaderStateReturnAfterAbsence
	}
	if stats.TeamActivity > 0 && bucket == model.HubTimeBucketMorning {
		return model.HubHeaderStateTeamActivity
	}
	return model.HubHeaderStateClean
}

func resolveHubGreetingKey(
	hubType string,
	state model.HubHeaderState,
	bucket model.HubTimeBucket,
	seed string,
) model.HubGreetingKey {
	switch state {
	case model.HubHeaderStateFirstTime:
		return pickHubGreeting([]model.HubGreetingKey{
			model.HubGreetingKeyFirstTimeA,
			model.HubGreetingKeyFirstTimeB,
		}, seed)
	case model.HubHeaderStateReviewNeeded:
		if hubType == "team" {
			return pickHubGreeting([]model.HubGreetingKey{
				model.HubGreetingKeyTeamReviewA,
				model.HubGreetingKeyTeamReviewB,
			}, seed)
		}
		return pickHubGreeting([]model.HubGreetingKey{
			model.HubGreetingKeyReviewNeededA,
			model.HubGreetingKeyReviewNeededB,
		}, seed)
	case model.HubHeaderStateDreamDeltas:
		if bucket == model.HubTimeBucketMorning {
			return pickHubGreeting([]model.HubGreetingKey{
				model.HubGreetingKeyMorningDreamA,
				model.HubGreetingKeyMorningDreamB,
			}, seed)
		}
		return pickHubGreeting([]model.HubGreetingKey{
			model.HubGreetingKeyDreamDeltasA,
			model.HubGreetingKeyDreamDeltasB,
		}, seed)
	case model.HubHeaderStateInboxOverflow:
		return pickHubGreeting([]model.HubGreetingKey{
			model.HubGreetingKeyInboxOverflowA,
			model.HubGreetingKeyInboxOverflowB,
		}, seed)
	case model.HubHeaderStateReturnAfterAbsence:
		return pickHubGreeting([]model.HubGreetingKey{
			model.HubGreetingKeyReturnAfterAbsenceA,
			model.HubGreetingKeyReturnAfterAbsenceB,
		}, seed)
	case model.HubHeaderStateTeamActivity:
		return pickHubGreeting([]model.HubGreetingKey{
			model.HubGreetingKeyTeamMorningA,
			model.HubGreetingKeyTeamMorningB,
		}, seed)
	default:
		switch bucket {
		case model.HubTimeBucketDeepNight, model.HubTimeBucketLateNight:
			return pickHubGreeting([]model.HubGreetingKey{
				model.HubGreetingKeyDeepNightCleanA,
				model.HubGreetingKeyDeepNightCleanB,
			}, seed)
		case model.HubTimeBucketMorning:
			return pickHubGreeting([]model.HubGreetingKey{
				model.HubGreetingKeyMorningCleanA,
				model.HubGreetingKeyMorningCleanB,
			}, seed)
		case model.HubTimeBucketAfternoon:
			return pickHubGreeting([]model.HubGreetingKey{
				model.HubGreetingKeyAfternoonCleanA,
				model.HubGreetingKeyAfternoonCleanB,
			}, seed)
		case model.HubTimeBucketEvening:
			return pickHubGreeting([]model.HubGreetingKey{
				model.HubGreetingKeyTimeEveningA,
				model.HubGreetingKeyTimeEveningB,
			}, seed)
		case model.HubTimeBucketNoon:
			return pickHubGreeting([]model.HubGreetingKey{
				model.HubGreetingKeyTimeNoonA,
				model.HubGreetingKeyTimeNoonB,
			}, seed)
		default:
			return pickHubGreeting([]model.HubGreetingKey{
				model.HubGreetingKeyTimeAfternoonA,
				model.HubGreetingKeyTimeAfternoonB,
			}, seed)
		}
	}
}

func resolveHubSummaryTime(r *http.Request, userID string, s *HubsHandler) time.Time {
	now := time.Now().UTC()
	if tz, ok := r.Context().Value(timezoneKey).(string); ok && tz != "" {
		if loc, err := time.LoadLocation(tz); err == nil {
			return now.In(loc)
		}
	}
	if prefs, err := s.store.GetUserPreferences(userID); err == nil && prefs != nil {
		if tz, ok := prefs.Settings["timezone"].(string); ok && tz != "" {
			if loc, err := time.LoadLocation(tz); err == nil {
				return now.In(loc)
			}
		}
	}
	return now
}

func hubGreetingParams(name string, stats model.HubSummaryStats) map[string]string {
	params := map[string]string{"name": name}
	if stats.PendingReview > 0 {
		params["n"] = fmt.Sprintf("%d", stats.PendingReview)
	} else if stats.DreamTopics > 0 {
		params["n"] = fmt.Sprintf("%d", stats.DreamTopics)
	} else if stats.Inbox > 0 {
		params["n"] = fmt.Sprintf("%d", stats.Inbox)
	} else if stats.TeamActivity > 0 {
		params["n"] = fmt.Sprintf("%d", stats.TeamActivity)
	}
	return params
}
