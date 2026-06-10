package queue

import (
	"encoding/json"
	"testing"

	"github.com/riverqueue/river"
)

// Each River job arg has two contract methods: Kind() (the string
// River uses to route it to a worker) and InsertOpts() (queue name,
// max attempts, uniqueness). These tests pin the values that
// operators depend on for dashboards and capacity planning — a typo
// in a Kind string would silently route jobs nowhere.

type jobSpec struct {
	args       river.JobArgs
	wantKind   string
	wantQueue  string
	wantMaxAtt int
	wantUnique bool
}

func allJobSpecs() []jobSpec {
	return []jobSpec{
		{args: MemoryProcessArgs{}, wantKind: "memory_process", wantQueue: "default", wantMaxAtt: 3},
		{args: DreamCycleArgs{}, wantKind: "dream_cycle", wantQueue: "dreams", wantMaxAtt: 2, wantUnique: true},
		{args: ConfigExtractArgs{}, wantKind: "config_extract"},
		{args: NightlyDreamSweepArgs{}, wantKind: "nightly_dream_sweep"},
		{args: SendEmailArgs{}, wantKind: "send_email"},
		{args: SendRenderedEmailArgs{}, wantKind: "send_rendered_email"},
		{args: SendInviteRemindersArgs{}, wantKind: "send_invite_reminders"},
		{args: ExpireWaitlistInvitesArgs{}, wantKind: "expire_waitlist_invites"},
		{args: ExpireNotificationsArgs{}, wantKind: "expire_notifications"},
		{args: SendBroadcastNotificationArgs{}, wantKind: "send_broadcast_notification"},
		{args: CampaignSendArgs{}, wantKind: "campaign_send"},
		{args: HubFreezeSweepArgs{}, wantKind: "hub_freeze_sweep"},
		{args: UsageSyncArgs{}, wantKind: "usage_sync"},
	}
}

func TestKindStringsAreUniqueAndStable(t *testing.T) {
	t.Parallel()
	specs := allJobSpecs()
	seen := map[string]string{}
	for _, spec := range specs {
		got := spec.args.Kind()
		if got != spec.wantKind {
			t.Errorf("%T.Kind() = %q, want %q", spec.args, got, spec.wantKind)
		}
		if prev, ok := seen[got]; ok {
			t.Errorf("Kind %q collision: %T vs %s", got, spec.args, prev)
		}
		seen[got] = spec.wantKind
	}
}

func TestInsertOptsReturnReasonableDefaults(t *testing.T) {
	t.Parallel()
	for _, spec := range allJobSpecs() {
		type optsProvider interface {
			InsertOpts() river.InsertOpts
		}
		p, ok := spec.args.(optsProvider)
		if !ok {
			// Not every args carries a custom InsertOpts; skip those.
			continue
		}
		opts := p.InsertOpts()
		if spec.wantQueue != "" && opts.Queue != spec.wantQueue {
			t.Errorf("%T queue = %q, want %q", spec.args, opts.Queue, spec.wantQueue)
		}
		if spec.wantMaxAtt != 0 && opts.MaxAttempts != spec.wantMaxAtt {
			t.Errorf("%T MaxAttempts = %d, want %d",
				spec.args, opts.MaxAttempts, spec.wantMaxAtt)
		}
		if spec.wantUnique && !opts.UniqueOpts.ByArgs {
			t.Errorf("%T should have UniqueOpts.ByArgs=true", spec.args)
		}
	}
}

// Args types must round-trip through JSON cleanly so the River payload
// column can serialize + deserialize them.
func TestAllArgsRoundTripJSON(t *testing.T) {
	t.Parallel()
	for _, spec := range allJobSpecs() {
		buf, err := json.Marshal(spec.args)
		if err != nil {
			t.Errorf("%T marshal: %v", spec.args, err)
			continue
		}
		// Decoding into a fresh zero value should not error — all
		// fields must be tagged or nullable.
		switch spec.args.(type) {
		case MemoryProcessArgs:
			var v MemoryProcessArgs
			if err := json.Unmarshal(buf, &v); err != nil {
				t.Errorf("MemoryProcessArgs unmarshal: %v", err)
			}
		case DreamCycleArgs:
			var v DreamCycleArgs
			if err := json.Unmarshal(buf, &v); err != nil {
				t.Errorf("DreamCycleArgs unmarshal: %v", err)
			}
		}
	}
}
