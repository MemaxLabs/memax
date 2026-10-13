-- Revert 069: v2_plan_rows
--
-- The V2 plans' limits go, and Dream's sweep reads no plan again.

REVOKE SELECT (personal_plan_id) ON public.users FROM memax_v2_dream_sweeper;
REVOKE ALL ON public.effective_plan_cache, public.hub_subscriptions FROM memax_v2_dream_sweeper;

ALTER TABLE public.plans
    DROP COLUMN activity_retention_days,
    DROP COLUMN dream_nightly_spaces,
    DROP COLUMN dream_cadence,
    DROP COLUMN v2_ask_limit,
    DROP COLUMN agent_kind_limit,
    DROP COLUMN project_space_limit,
    DROP COLUMN v2_tier;
