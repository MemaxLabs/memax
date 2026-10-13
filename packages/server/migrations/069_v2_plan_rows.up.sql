-- 069: v2_plan_rows (plan 25 §12 3.4, D9)
--
-- The V2 plans' limits, as columns of V1's plan rows, so the plans
-- registry (internal/plans) loads them with everything else and each row
-- says which V2 plan it is. V1's plans map onto Free, Pro and Team and are
-- grandfathered (plan 25 §10): personal_free and hub_free_team are Free;
-- early_access, pro and pro_plus are Pro; hub_team is Team; hub_enterprise
-- is Enterprise. The inactive legacy ids get their scoped twin's values.
--
--   v2_tier                  free | pro | team | enterprise: which V2 plan
--                            the row is, for words and "what the next plan
--                            adds"
--   project_space_limit      project spaces a person may own (-1: no limit;
--                            D9: Free 1, besides the personal space)
--   agent_kind_limit         distinct agent kinds a person may connect
--                            (claude-code, codex, cursor...; -1: no limit;
--                            D9: Free 3)
--   v2_ask_limit             asks a month (-1: no limit; D9: Free 50, Pro
--                            and Team 1,000). V1's ask_limit stays V1's: its
--                            meter counts V1's Ask with it
--   dream_cadence            weekly | nightly (D9: weekly on Free)
--   dream_nightly_spaces     how many of an owner's busiest spaces dream
--                            nightly on a nightly plan (-1: all; Pro 5)
--   activity_retention_days  how far back Activity's reads and compiles go
--                            (-1: as far as they're kept; D9: Free 30, Pro
--                            90, Team a year)
--
-- seat_billed (V1's column) is already the Team rule: members are billed
-- seats and viewers are free, so on a plan that doesn't bill seats only
-- the owner keeps (policy.Plan.Seat).
--
-- Nothing is enforced by these rows alone: policy refuses on them only
-- when V2_PLAN_LIMITS=enforce (internal/v2plans), off during the alpha.

ALTER TABLE public.plans
    ADD COLUMN v2_tier text NOT NULL DEFAULT 'free'
        CONSTRAINT plans_v2_tier_check CHECK (v2_tier IN ('free', 'pro', 'team', 'enterprise')),
    ADD COLUMN project_space_limit integer NOT NULL DEFAULT 1
        CONSTRAINT plans_project_space_limit_check CHECK (project_space_limit >= -1),
    ADD COLUMN agent_kind_limit integer NOT NULL DEFAULT 3
        CONSTRAINT plans_agent_kind_limit_check CHECK (agent_kind_limit >= -1),
    ADD COLUMN v2_ask_limit integer NOT NULL DEFAULT 50
        CONSTRAINT plans_v2_ask_limit_check CHECK (v2_ask_limit >= -1),
    ADD COLUMN dream_cadence text NOT NULL DEFAULT 'weekly'
        CONSTRAINT plans_dream_cadence_check CHECK (dream_cadence IN ('weekly', 'nightly')),
    ADD COLUMN dream_nightly_spaces integer NOT NULL DEFAULT 0
        CONSTRAINT plans_dream_nightly_spaces_check CHECK (dream_nightly_spaces >= -1),
    ADD COLUMN activity_retention_days integer NOT NULL DEFAULT 30
        CONSTRAINT plans_activity_retention_days_check CHECK (activity_retention_days >= -1 AND activity_retention_days <> 0);

-- Free keeps the defaults: personal_free, free, hub_free_team.
UPDATE public.plans
   SET v2_tier = 'pro', project_space_limit = -1, agent_kind_limit = -1, v2_ask_limit = 1000,
       dream_cadence = 'nightly', dream_nightly_spaces = 5, activity_retention_days = 90
 WHERE id IN ('personal_early_access', 'early_access', 'personal_pro', 'pro', 'personal_pro_plus', 'pro_plus');

UPDATE public.plans
   SET v2_tier = 'team', project_space_limit = -1, agent_kind_limit = -1, v2_ask_limit = 1000,
       dream_cadence = 'nightly', dream_nightly_spaces = -1, activity_retention_days = 365
 WHERE id IN ('hub_team', 'team');

UPDATE public.plans
   SET v2_tier = 'enterprise', project_space_limit = -1, agent_kind_limit = -1, v2_ask_limit = -1,
       dream_cadence = 'nightly', dream_nightly_spaces = -1, activity_retention_days = -1
 WHERE id IN ('hub_enterprise', 'enterprise');

-- Dream's sweep schedules each space on its plan (the better of its team's
-- subscription and its owner's plan): which plan rows those are, and
-- nothing else of the person or the subscription.
GRANT SELECT (personal_plan_id) ON public.users TO memax_v2_dream_sweeper;
GRANT SELECT (user_id, effective_plan) ON public.effective_plan_cache TO memax_v2_dream_sweeper;
GRANT SELECT (hub_id, plan_id, status) ON public.hub_subscriptions TO memax_v2_dream_sweeper;
