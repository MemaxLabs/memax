-- Memax app schema baseline v1.
-- Re-squashed from migrations 001..002 (the previous 001 baseline
-- itself covered 001..019).
-- Generated via pg_dump against a fresh DB with all migrations
-- applied. River queue tables/types/functions are intentionally
-- excluded; they are managed by River migrations (internal/queue).
-- schema_migrations + schema_lock are excluded; golang-migrate
-- manages them.

--
-- Name: pg_trgm; Type: EXTENSION; Schema: -; Owner: -
--

CREATE EXTENSION IF NOT EXISTS pg_trgm WITH SCHEMA public;


--
-- Name: pgcrypto; Type: EXTENSION; Schema: -; Owner: -
--

CREATE EXTENSION IF NOT EXISTS pgcrypto WITH SCHEMA public;


--
-- Name: unaccent; Type: EXTENSION; Schema: -; Owner: -
--

CREATE EXTENSION IF NOT EXISTS unaccent WITH SCHEMA public;


--
-- Name: vector; Type: EXTENSION; Schema: -; Owner: -
--

CREATE EXTENSION IF NOT EXISTS vector WITH SCHEMA public;


--
-- Name: notification_audience; Type: TYPE; Schema: public; Owner: -
--

CREATE TYPE public.notification_audience AS ENUM (
    'hub',
    'hub_member',
    'user'
);


--
-- Name: notification_resolution; Type: TYPE; Schema: public; Owner: -
--

CREATE TYPE public.notification_resolution AS ENUM (
    'kept_a',
    'kept_b',
    'kept_both',
    'merged',
    'kept_separate',
    'applied',
    'kept',
    'dismissed',
    'accepted',
    'declined'
);


--
-- Name: chunks_search_columns_update(); Type: FUNCTION; Schema: public; Owner: -
--

CREATE FUNCTION public.chunks_search_columns_update() RETURNS trigger
    LANGUAGE plpgsql
    AS $$
BEGIN
  NEW.language := COALESCE(NULLIF(NEW.language, ''), 'und');
  NEW.search_config := COALESCE(NULLIF(NEW.search_config, ''), 'simple');
  NEW.tags_text := COALESCE(NEW.tags_text, '');
  NEW.metadata_text := COALESCE(NEW.metadata_text, '');
  NEW.search_text := immutable_unaccent(lower(trim(concat_ws(' ',
    COALESCE(NEW.heading_chain, ''),
    COALESCE(NEW.hint, ''),
    COALESCE(NEW.tags_text, ''),
    COALESCE(NEW.metadata_text, ''),
    COALESCE(NEW.content, '')
  ))));
  NEW.search_vector := to_tsvector(NEW.search_config::regconfig, COALESCE(NEW.search_text, ''));
  RETURN NEW;
END $$;


--
-- Name: enforce_topic_same_hub_parent(); Type: FUNCTION; Schema: public; Owner: -
--

CREATE FUNCTION public.enforce_topic_same_hub_parent() RETURNS trigger
    LANGUAGE plpgsql
    AS $$
DECLARE
    parent_hub_id uuid;
    mismatched_child_count integer;
BEGIN
    -- Case 1: child's own parent_id must share its hub_id.
    IF NEW.parent_id IS NOT NULL THEN
        SELECT hub_id INTO parent_hub_id FROM public.topics WHERE id = NEW.parent_id;
        IF parent_hub_id IS NULL THEN
            RAISE EXCEPTION 'topics.parent_id references non-existent topic %', NEW.parent_id
                USING ERRCODE = '23503';
        END IF;
        IF parent_hub_id <> NEW.hub_id THEN
            RAISE EXCEPTION 'topics.parent_id (hub=%) must be in the same hub as the child (hub=%)',
                parent_hub_id, NEW.hub_id USING ERRCODE = '23514';
        END IF;
    END IF;

    -- Case 2: parent row's hub_id is changing. Reject if any child
    -- still points at this row from a different hub — that would
    -- leave the invariant violated from the child's perspective.
    -- Only relevant on UPDATE; INSERT has no existing children to
    -- worry about.
    IF TG_OP = 'UPDATE' AND NEW.hub_id IS DISTINCT FROM OLD.hub_id THEN
        SELECT count(*) INTO mismatched_child_count
        FROM public.topics
        WHERE parent_id = NEW.id
          AND hub_id <> NEW.hub_id;
        IF mismatched_child_count > 0 THEN
            RAISE EXCEPTION 'cannot change topics.hub_id while % child topic(s) in a different hub still reference this row',
                mismatched_child_count USING ERRCODE = '23514';
        END IF;
    END IF;

    RETURN NEW;
END;
$$;


--
-- Name: immutable_unaccent(text); Type: FUNCTION; Schema: public; Owner: -
--

CREATE FUNCTION public.immutable_unaccent(text) RETURNS text
    LANGUAGE sql IMMUTABLE PARALLEL SAFE
    AS $_$SELECT public.unaccent($1)$_$;


--
-- Name: admin_audit; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.admin_audit (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    resource_type text NOT NULL,
    resource_id text NOT NULL,
    action text NOT NULL,
    actor_id uuid,
    metadata jsonb DEFAULT '{}'::jsonb NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL
);


--
-- Name: admin_roles; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.admin_roles (
    user_id uuid NOT NULL,
    role text DEFAULT 'super_admin'::text NOT NULL,
    granted_by uuid,
    granted_at timestamp with time zone DEFAULT now() NOT NULL
);


--
-- Name: agent_config_sync_states; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.agent_config_sync_states (
    owner_id uuid NOT NULL,
    device_id text NOT NULL,
    agent text NOT NULL,
    file_path text NOT NULL,
    scope text NOT NULL,
    local_path text,
    last_seen_version integer NOT NULL,
    last_seen_hash text NOT NULL,
    suppressed boolean DEFAULT false NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL
);


--
-- Name: agent_config_tombstones; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.agent_config_tombstones (
    owner_id uuid NOT NULL,
    agent text NOT NULL,
    file_path text NOT NULL,
    scope text NOT NULL,
    version integer NOT NULL,
    deleted_at timestamp with time zone DEFAULT now() NOT NULL,
    deleted_content text,
    deleted_content_hash text,
    content_expires_at timestamp with time zone
);


--
-- Name: agent_configs; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.agent_configs (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    owner_id uuid NOT NULL,
    agent text NOT NULL,
    file_path text NOT NULL,
    scope text DEFAULT 'global'::text NOT NULL,
    content text NOT NULL,
    content_hash text NOT NULL,
    version integer DEFAULT 1 NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL
);


--
-- Name: agent_session_snapshots; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.agent_session_snapshots (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    session_id uuid,
    owner_id uuid NOT NULL,
    agent text NOT NULL,
    file_path text NOT NULL,
    scope text DEFAULT 'global'::text NOT NULL,
    device_id text NOT NULL,
    device_label text,
    content_hash text NOT NULL,
    object_key text NOT NULL,
    version_at integer NOT NULL,
    reason text DEFAULT 'divergence'::text NOT NULL,
    session_type text DEFAULT 'session'::text NOT NULL,
    filename text DEFAULT ''::text NOT NULL,
    content_type text DEFAULT 'application/octet-stream'::text NOT NULL,
    size_bytes bigint,
    content_expires_at timestamp with time zone,
    purged_at timestamp with time zone,
    created_at timestamp with time zone DEFAULT now() NOT NULL
);


--
-- Name: agent_session_sync_states; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.agent_session_sync_states (
    owner_id uuid NOT NULL,
    device_id text NOT NULL,
    agent text NOT NULL,
    file_path text NOT NULL,
    scope text NOT NULL,
    local_path text,
    last_seen_version integer NOT NULL,
    last_seen_hash text NOT NULL,
    suppressed boolean DEFAULT false NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL
);


--
-- Name: agent_session_tombstones; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.agent_session_tombstones (
    owner_id uuid NOT NULL,
    agent text NOT NULL,
    file_path text NOT NULL,
    scope text NOT NULL,
    version integer NOT NULL,
    deleted_at timestamp with time zone DEFAULT now() NOT NULL,
    session_type text,
    filename text,
    content_type text,
    size_bytes bigint,
    content_hash text,
    object_key text,
    content_expires_at timestamp with time zone
);


--
-- Name: agent_sessions; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.agent_sessions (
    id uuid NOT NULL,
    owner_id uuid NOT NULL,
    agent text NOT NULL,
    file_path text NOT NULL,
    scope text DEFAULT 'global'::text NOT NULL,
    session_type text DEFAULT 'session'::text NOT NULL,
    filename text NOT NULL,
    content_type text NOT NULL,
    size_bytes bigint DEFAULT 0 NOT NULL,
    content_hash text NOT NULL,
    object_key text NOT NULL,
    version integer DEFAULT 1 NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL
);


--
-- Name: api_keys; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.api_keys (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    user_id uuid NOT NULL,
    name text NOT NULL,
    key_hash text NOT NULL,
    prefix text NOT NULL,
    scopes text[] DEFAULT '{read,write}'::text[] NOT NULL,
    expires_at timestamp with time zone,
    last_used timestamp with time zone,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    hub_id uuid,
    agent_name text DEFAULT ''::text NOT NULL,
    hub_scope_mode text DEFAULT 'all_accessible'::text NOT NULL,
    hub_ids uuid[] DEFAULT ARRAY[]::uuid[] NOT NULL,
    default_permissions text[] DEFAULT ARRAY['memory:read'::text, 'memory:write'::text, 'topic:read'::text, 'dream:read'::text, 'hub:read'::text, 'hub:members:read'::text] NOT NULL,
    trust_level text DEFAULT 'elevated'::text NOT NULL,
    rate_limit_tier text,
    revoked_at timestamp with time zone,
    standalone boolean DEFAULT false NOT NULL
);


--
-- Name: audiences; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.audiences (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    name text NOT NULL,
    description text DEFAULT ''::text NOT NULL,
    rule_type text NOT NULL,
    rule_json jsonb DEFAULT '{}'::jsonb NOT NULL,
    created_by uuid NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    CONSTRAINT audiences_rule_type_check CHECK ((rule_type = ANY (ARRAY['all'::text, 'users'::text, 'hub'::text])))
);


--
-- Name: auth_codes; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.auth_codes (
    code text NOT NULL,
    user_id uuid NOT NULL,
    expires_at timestamp with time zone NOT NULL,
    used boolean DEFAULT false NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    code_challenge text DEFAULT ''::text NOT NULL,
    grant_id uuid,
    client_id text,
    redirect_uri text
);


--
-- Name: auth_identities; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.auth_identities (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    user_id uuid NOT NULL,
    provider text NOT NULL,
    provider_id text NOT NULL,
    provider_email text DEFAULT ''::text NOT NULL,
    provider_name text DEFAULT ''::text NOT NULL,
    provider_avatar text DEFAULT ''::text NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL
);


--
-- Name: billing_subscriptions; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.billing_subscriptions (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    user_id uuid NOT NULL,
    plan_id text NOT NULL,
    provider text DEFAULT 'admin'::text NOT NULL,
    provider_subscription_id text,
    provider_customer_id text,
    status text DEFAULT 'active'::text NOT NULL,
    current_period_start timestamp with time zone,
    current_period_end timestamp with time zone,
    cancelled_at timestamp with time zone,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    plan_scope text DEFAULT 'personal'::text NOT NULL,
    CONSTRAINT billing_subscriptions_plan_scope_check CHECK ((plan_scope = 'personal'::text))
);


--
-- Name: campaign_deliveries; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.campaign_deliveries (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    campaign_id uuid NOT NULL,
    batch_id text NOT NULL,
    recipient_user_id uuid,
    recipient_email text NOT NULL,
    status text DEFAULT 'queued'::text NOT NULL,
    provider_message_id text DEFAULT ''::text NOT NULL,
    error_code text DEFAULT ''::text NOT NULL,
    error_message text DEFAULT ''::text NOT NULL,
    queued_at timestamp with time zone DEFAULT now() NOT NULL,
    sent_at timestamp with time zone,
    delivered_at timestamp with time zone,
    opened_at timestamp with time zone,
    bounced_at timestamp with time zone,
    complained_at timestamp with time zone,
    CONSTRAINT campaign_deliveries_status_check CHECK ((status = ANY (ARRAY['queued'::text, 'sent'::text, 'delivered'::text, 'bounced'::text, 'complained'::text, 'failed'::text, 'suppressed'::text])))
);


--
-- Name: campaign_templates; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.campaign_templates (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    slug text NOT NULL,
    name text NOT NULL,
    description text DEFAULT ''::text NOT NULL,
    subject text DEFAULT ''::text NOT NULL,
    body_html text DEFAULT ''::text NOT NULL,
    body_text text DEFAULT ''::text NOT NULL,
    is_archived boolean DEFAULT false NOT NULL,
    created_by uuid,
    updated_by uuid,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    CONSTRAINT campaign_templates_name_chk CHECK ((char_length(btrim(name)) > 0)),
    CONSTRAINT campaign_templates_slug_chk CHECK ((slug ~ '^[a-z0-9][a-z0-9_-]{1,63}$'::text))
);


--
-- Name: campaigns; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.campaigns (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    name text NOT NULL,
    channel text DEFAULT 'inapp'::text NOT NULL,
    kind text NOT NULL,
    status text DEFAULT 'draft'::text NOT NULL,
    audience_id uuid,
    audience_snapshot jsonb,
    content jsonb NOT NULL,
    scheduled_at timestamp with time zone,
    send_started_at timestamp with time zone,
    send_finished_at timestamp with time zone,
    sent_count integer DEFAULT 0 NOT NULL,
    failed_count integer DEFAULT 0 NOT NULL,
    batch_id text,
    error_message text,
    created_by uuid NOT NULL,
    updated_by uuid,
    cancelled_by uuid,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    CONSTRAINT campaigns_channel_check CHECK ((channel = ANY (ARRAY['inapp'::text, 'email'::text]))),
    CONSTRAINT campaigns_kind_check CHECK ((kind = ANY (ARRAY['system_notice'::text, 'gift_invite_link'::text, 'email_announcement'::text]))),
    CONSTRAINT campaigns_status_check CHECK ((status = ANY (ARRAY['draft'::text, 'scheduled'::text, 'sending'::text, 'sent'::text, 'cancelled'::text, 'failed'::text])))
);


--
-- Name: chunks; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.chunks (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    memory_id uuid NOT NULL,
    content text DEFAULT ''::text NOT NULL,
    heading_chain text DEFAULT ''::text NOT NULL,
    chunk_index integer DEFAULT 0 NOT NULL,
    token_count integer DEFAULT 0 NOT NULL,
    kind text DEFAULT 'semantic'::text NOT NULL,
    stability text DEFAULT 'evolving'::text NOT NULL,
    retrieval_weight double precision DEFAULT 1.0 NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    embedding public.vector(1024),
    search_text text,
    project_repo text DEFAULT ''::text NOT NULL,
    hint text DEFAULT ''::text NOT NULL,
    language text DEFAULT 'und'::text NOT NULL,
    search_config text DEFAULT 'simple'::text NOT NULL,
    search_vector tsvector,
    tags_text text DEFAULT ''::text NOT NULL,
    metadata_text text DEFAULT ''::text NOT NULL,
    CONSTRAINT chunks_kind_check CHECK ((kind = ANY (ARRAY['episodic'::text, 'semantic'::text, 'procedural'::text, 'rationale'::text]))),
    CONSTRAINT chunks_stability_check CHECK ((stability = ANY (ARRAY['volatile'::text, 'evolving'::text, 'stable'::text])))
);


--
-- Name: comms_audit; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.comms_audit (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    resource_type text NOT NULL,
    resource_id uuid NOT NULL,
    action text NOT NULL,
    actor_id uuid,
    metadata jsonb DEFAULT '{}'::jsonb NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    CONSTRAINT comms_audit_resource_type_check CHECK ((resource_type = ANY (ARRAY['campaign'::text, 'audience'::text])))
);


--
-- Name: connected_agents; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.connected_agents (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    owner_id uuid NOT NULL,
    agent_name text NOT NULL,
    display_name text DEFAULT ''::text NOT NULL,
    icon text DEFAULT ''::text NOT NULL,
    status text DEFAULT 'active'::text NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL
);


--
-- Name: dream_actions; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.dream_actions (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    run_id uuid NOT NULL,
    action_type text NOT NULL,
    source_memory_ids text[] DEFAULT '{}'::text[] NOT NULL,
    result_memory_id text,
    reason text DEFAULT ''::text NOT NULL,
    similarity double precision,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    from_topic_id uuid,
    to_topic_id uuid
);


--
-- Name: dream_runs; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.dream_runs (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    owner_id uuid NOT NULL,
    status text DEFAULT 'running'::text NOT NULL,
    started_at timestamp with time zone DEFAULT now() NOT NULL,
    finished_at timestamp with time zone,
    memories_scanned integer DEFAULT 0 NOT NULL,
    duplicates_merged integer DEFAULT 0 NOT NULL,
    contradictions_found integer DEFAULT 0 NOT NULL,
    memories_archived integer DEFAULT 0 NOT NULL,
    report text DEFAULT ''::text NOT NULL,
    memories_organized integer DEFAULT 0 NOT NULL,
    topics_restructured integer DEFAULT 0 NOT NULL,
    phase_metrics jsonb DEFAULT '{}'::jsonb NOT NULL,
    phase_budgets jsonb DEFAULT '{}'::jsonb NOT NULL,
    hub_id uuid NOT NULL,
    mode text DEFAULT 'maintenance'::text NOT NULL,
    last_heartbeat_at timestamp with time zone
);


--
-- Name: effective_plan_cache; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.effective_plan_cache (
    user_id uuid NOT NULL,
    effective_plan text NOT NULL,
    source text DEFAULT 'personal'::text NOT NULL,
    computed_at timestamp with time zone DEFAULT now() NOT NULL
);


--
-- Name: email_brand_settings; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.email_brand_settings (
    id text DEFAULT 'singleton'::text NOT NULL,
    logo_url text DEFAULT ''::text NOT NULL,
    logo_alt text DEFAULT 'memax'::text NOT NULL,
    product_name text DEFAULT 'memax'::text NOT NULL,
    footer_html text DEFAULT '<p>memax — memory for your AI agents</p>'::text NOT NULL,
    footer_text text DEFAULT 'memax — memory for your AI agents'::text NOT NULL,
    primary_color text DEFAULT '#111111'::text NOT NULL,
    background_color text DEFAULT '#fafafa'::text NOT NULL,
    surface_color text DEFAULT '#ffffff'::text NOT NULL,
    border_color text DEFAULT '#e5e5e5'::text NOT NULL,
    muted_color text DEFAULT '#888888'::text NOT NULL,
    body_color text DEFAULT '#444444'::text NOT NULL,
    support_email text DEFAULT ''::text NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_by uuid,
    company_name text DEFAULT ''::text NOT NULL,
    company_address text DEFAULT ''::text NOT NULL,
    privacy_url text DEFAULT ''::text NOT NULL,
    terms_url text DEFAULT ''::text NOT NULL,
    CONSTRAINT email_brand_settings_singleton CHECK ((id = 'singleton'::text))
);


--
-- Name: email_template_override_revisions; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.email_template_override_revisions (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    name text NOT NULL,
    action text NOT NULL,
    subject text NOT NULL,
    html text NOT NULL,
    text text NOT NULL,
    notes text DEFAULT ''::text NOT NULL,
    editor_kind text DEFAULT 'html'::text NOT NULL,
    editor_state jsonb DEFAULT '{}'::jsonb NOT NULL,
    created_by uuid,
    created_at timestamp with time zone DEFAULT now() NOT NULL
);


--
-- Name: email_template_overrides; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.email_template_overrides (
    name text NOT NULL,
    subject text NOT NULL,
    html text NOT NULL,
    text text NOT NULL,
    notes text DEFAULT ''::text NOT NULL,
    editor_kind text DEFAULT 'html'::text NOT NULL,
    editor_state jsonb DEFAULT '{}'::jsonb NOT NULL,
    updated_by uuid,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    draft_subject text DEFAULT ''::text NOT NULL,
    draft_html text DEFAULT ''::text NOT NULL,
    draft_text text DEFAULT ''::text NOT NULL,
    published_at timestamp with time zone
);


--
-- Name: hub_invites; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.hub_invites (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    hub_id uuid NOT NULL,
    token text NOT NULL,
    invited_by uuid NOT NULL,
    role text DEFAULT 'contributor'::text NOT NULL,
    expires_at timestamp with time zone NOT NULL,
    accepted_by uuid,
    accepted_at timestamp with time zone,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    revoked_by uuid,
    revoked_at timestamp with time zone,
    invitee_user_id uuid,
    invitee_email text,
    email_enqueued_at timestamp with time zone
);


--
-- Name: hub_members; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.hub_members (
    hub_id uuid NOT NULL,
    user_id uuid NOT NULL,
    role text DEFAULT 'contributor'::text NOT NULL,
    joined_at timestamp with time zone DEFAULT now() NOT NULL
);


--
-- Name: hub_ownership_transfers; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.hub_ownership_transfers (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    hub_id uuid NOT NULL,
    initiated_by uuid NOT NULL,
    target_user_id uuid NOT NULL,
    accepted_at timestamp with time zone,
    cancelled_at timestamp with time zone,
    expires_at timestamp with time zone NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL
);


--
-- Name: hub_subscriptions; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.hub_subscriptions (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    hub_id uuid NOT NULL,
    plan_id text NOT NULL,
    seat_count integer DEFAULT 3 NOT NULL,
    provider text DEFAULT 'admin'::text NOT NULL,
    provider_subscription_id text,
    provider_customer_id text,
    status text DEFAULT 'active'::text NOT NULL,
    billing_user_id uuid NOT NULL,
    current_period_start timestamp with time zone,
    current_period_end timestamp with time zone,
    cancelled_at timestamp with time zone,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    plan_scope text DEFAULT 'hub'::text NOT NULL,
    over_limit_since timestamp with time zone,
    CONSTRAINT hub_subscriptions_plan_scope_check CHECK ((plan_scope = 'hub'::text))
);


--
-- Name: hub_visits; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.hub_visits (
    user_id uuid NOT NULL,
    hub_id uuid NOT NULL,
    first_visited_at timestamp with time zone DEFAULT now() NOT NULL,
    last_visited_at timestamp with time zone DEFAULT now() NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL
);


--
-- Name: hubs; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.hubs (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    name text NOT NULL,
    slug text NOT NULL,
    hub_type text DEFAULT 'personal'::text NOT NULL,
    owner_id uuid NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    plan text DEFAULT ''::text NOT NULL,
    allow_contributor_topics boolean DEFAULT true NOT NULL,
    allow_contributor_dreams boolean DEFAULT true NOT NULL,
    contributor_delete_policy text DEFAULT 'own'::text NOT NULL,
    icon text DEFAULT ''::text NOT NULL,
    accent text DEFAULT 'violet'::text NOT NULL,
    header_aurora_mode text,
    settings jsonb DEFAULT '{}'::jsonb NOT NULL,
    CONSTRAINT hubs_header_aurora_mode_chk CHECK (((header_aurora_mode IS NULL) OR (header_aurora_mode = ANY (ARRAY['none'::text, 'signature'::text, 'time'::text]))))
);


--
-- Name: memories; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.memories (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    hub_id uuid NOT NULL,
    owner_id uuid NOT NULL,
    title text DEFAULT ''::text NOT NULL,
    content text DEFAULT ''::text NOT NULL,
    content_type text DEFAULT 'text'::text NOT NULL,
    content_hash text DEFAULT ''::text NOT NULL,
    summary text DEFAULT ''::text NOT NULL,
    kind text DEFAULT 'semantic'::text NOT NULL,
    stability text DEFAULT 'evolving'::text NOT NULL,
    retrieval_weight double precision DEFAULT 1.0 NOT NULL,
    access_intents jsonb DEFAULT '{}'::jsonb NOT NULL,
    tags text[] DEFAULT '{}'::text[] NOT NULL,
    boundary text DEFAULT 'private'::text NOT NULL,
    state text DEFAULT 'active'::text NOT NULL,
    pinned boolean DEFAULT false NOT NULL,
    source text DEFAULT ''::text NOT NULL,
    source_path text DEFAULT ''::text NOT NULL,
    version integer DEFAULT 1 NOT NULL,
    access_count integer DEFAULT 0 NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    accessed_at timestamp with time zone DEFAULT now() NOT NULL,
    project_context jsonb DEFAULT '{}'::jsonb NOT NULL,
    hint text DEFAULT ''::text NOT NULL,
    batch_id text DEFAULT ''::text NOT NULL,
    original_file_ref text DEFAULT ''::text NOT NULL,
    source_agent text DEFAULT ''::text NOT NULL,
    hub_reason text,
    event_dates timestamp with time zone[] DEFAULT '{}'::timestamp with time zone[] NOT NULL,
    shown_count integer DEFAULT 0 NOT NULL,
    created_by_type text DEFAULT 'human'::text NOT NULL,
    created_by_slug text DEFAULT ''::text NOT NULL,
    created_by_display_name text DEFAULT ''::text NOT NULL,
    created_via text DEFAULT ''::text NOT NULL,
    initiation_type text DEFAULT 'unknown'::text NOT NULL,
    attribution_source text DEFAULT ''::text NOT NULL,
    assisted_by_agent text DEFAULT ''::text NOT NULL,
    CONSTRAINT memories_kind_check CHECK ((kind = ANY (ARRAY['episodic'::text, 'semantic'::text, 'procedural'::text, 'rationale'::text]))),
    CONSTRAINT memories_stability_check CHECK ((stability = ANY (ARRAY['volatile'::text, 'evolving'::text, 'stable'::text])))
);


--
-- Name: memory_attachments; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.memory_attachments (
    id uuid NOT NULL,
    memory_id uuid NOT NULL,
    owner_id uuid NOT NULL,
    kind text DEFAULT 'original'::text NOT NULL,
    filename text NOT NULL,
    content_type text NOT NULL,
    size_bytes bigint DEFAULT 0 NOT NULL,
    sha256 text DEFAULT ''::text NOT NULL,
    storage_key text NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    width integer,
    height integer,
    inline_eligible boolean DEFAULT false NOT NULL
);


--
-- Name: memory_topics; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.memory_topics (
    memory_id uuid NOT NULL,
    topic_id uuid NOT NULL,
    confidence real DEFAULT 0.5 NOT NULL,
    "position" integer DEFAULT 0 NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL
);


--
-- Name: notifications; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.notifications (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    audience public.notification_audience NOT NULL,
    hub_id uuid,
    recipient_user_id uuid,
    hub_member_role text,
    kind text NOT NULL,
    status text DEFAULT 'pending'::text NOT NULL,
    resolution public.notification_resolution,
    priority smallint DEFAULT 0 NOT NULL,
    source_kind text NOT NULL,
    source_id text,
    payload jsonb DEFAULT '{}'::jsonb NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    expires_at timestamp with time zone,
    resolved_at timestamp with time zone,
    seen_at timestamp with time zone,
    dream_run_id uuid,
    CONSTRAINT notifications_audience_routing CHECK ((((audience = ANY (ARRAY['hub'::public.notification_audience, 'hub_member'::public.notification_audience])) AND (hub_id IS NOT NULL) AND (recipient_user_id IS NULL)) OR ((audience = 'user'::public.notification_audience) AND (recipient_user_id IS NOT NULL)))),
    CONSTRAINT notifications_status_values CHECK ((status = ANY (ARRAY['pending'::text, 'resolved'::text, 'dismissed'::text, 'expired'::text])))
);


--
-- Name: oauth_authorization_requests; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.oauth_authorization_requests (
    id text NOT NULL,
    client_id text NOT NULL,
    client_name text DEFAULT ''::text NOT NULL,
    redirect_uri text NOT NULL,
    state text DEFAULT ''::text NOT NULL,
    code_challenge text NOT NULL,
    code_challenge_method text NOT NULL,
    requested_permissions text[] DEFAULT ARRAY[]::text[] NOT NULL,
    requested_scope text DEFAULT ''::text NOT NULL,
    resource text DEFAULT ''::text NOT NULL,
    user_id uuid,
    csrf_token text DEFAULT ''::text NOT NULL,
    expires_at timestamp with time zone NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL
);


--
-- Name: oauth_clients; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.oauth_clients (
    client_id text NOT NULL,
    client_name text DEFAULT ''::text NOT NULL,
    redirect_uris text[] DEFAULT ARRAY[]::text[] NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL
);


--
-- Name: oauth_grants; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.oauth_grants (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    user_id uuid NOT NULL,
    client_id text NOT NULL,
    agent_name text DEFAULT ''::text NOT NULL,
    hub_scope_mode text DEFAULT 'hub_allowlist'::text NOT NULL,
    hub_ids uuid[] DEFAULT ARRAY[]::uuid[] NOT NULL,
    default_permissions text[] DEFAULT ARRAY[]::text[] NOT NULL,
    trust_level text DEFAULT 'standard'::text NOT NULL,
    rate_limit_tier text,
    expires_at timestamp with time zone,
    revoked_at timestamp with time zone,
    last_used timestamp with time zone,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL
);


--
-- Name: oauth_states; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.oauth_states (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    state_hash text NOT NULL,
    provider text NOT NULL,
    flow text DEFAULT 'login'::text NOT NULL,
    user_id uuid,
    client_redirect text DEFAULT ''::text NOT NULL,
    nonce text DEFAULT ''::text NOT NULL,
    expires_at timestamp with time zone NOT NULL,
    consumed_at timestamp with time zone,
    created_at timestamp with time zone DEFAULT now() NOT NULL
);


--
-- Name: plan_migration_anomalies; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.plan_migration_anomalies (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    entity_type text NOT NULL,
    entity_id uuid NOT NULL,
    old_value text NOT NULL,
    proposed_value text NOT NULL,
    reason text NOT NULL,
    resolved_at timestamp with time zone,
    created_at timestamp with time zone DEFAULT now() NOT NULL
);


--
-- Name: plan_overrides; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.plan_overrides (
    user_id uuid NOT NULL,
    overrides jsonb DEFAULT '{}'::jsonb NOT NULL,
    reason text DEFAULT ''::text NOT NULL,
    set_by uuid,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL
);


--
-- Name: plans; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.plans (
    id text NOT NULL,
    display_name text NOT NULL,
    tier_order integer DEFAULT 0 NOT NULL,
    monthly_price_cents integer DEFAULT 0 NOT NULL,
    memory_limit integer DEFAULT 300 NOT NULL,
    push_limit integer DEFAULT 200 NOT NULL,
    recall_limit integer DEFAULT 500 NOT NULL,
    ask_limit integer DEFAULT 10 NOT NULL,
    ask_model text DEFAULT 'haiku'::text NOT NULL,
    dreams_enabled boolean DEFAULT false NOT NULL,
    review_inbox boolean DEFAULT false NOT NULL,
    max_team_hubs integer DEFAULT 0 NOT NULL,
    rate_limit_rpm integer DEFAULT 60 NOT NULL,
    rate_limit_heavy_rpm integer DEFAULT 10 NOT NULL,
    rate_limit_light_rpm integer DEFAULT 60 NOT NULL,
    features jsonb DEFAULT '{}'::jsonb NOT NULL,
    active boolean DEFAULT true NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    scope text DEFAULT 'personal'::text NOT NULL,
    entitlement_rank integer DEFAULT 0 NOT NULL,
    max_owned_free_team_hubs integer DEFAULT 0 NOT NULL,
    max_hub_members integer,
    seat_minimum integer DEFAULT 0 NOT NULL,
    seat_billed boolean DEFAULT false NOT NULL,
    max_attachment_bytes bigint DEFAULT 5242880 NOT NULL,
    storage_bytes_limit bigint DEFAULT 536870912 NOT NULL,
    CONSTRAINT plans_scope_check CHECK ((scope = ANY (ARRAY['personal'::text, 'hub'::text])))
);


--
-- Name: reviews; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.reviews (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    owner_id uuid NOT NULL,
    review_type text NOT NULL,
    status text DEFAULT 'pending'::text NOT NULL,
    memory_ids text[] DEFAULT '{}'::text[] NOT NULL,
    dream_run_id uuid,
    title text DEFAULT ''::text NOT NULL,
    description text DEFAULT ''::text NOT NULL,
    similarity double precision,
    resolution text,
    resolved_at timestamp with time zone,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    review_key text,
    hub_id uuid NOT NULL,
    payload jsonb
);


--
-- Name: sessions; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.sessions (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    user_id uuid NOT NULL,
    refresh_token text NOT NULL,
    expires_at timestamp with time zone NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    agent_name text DEFAULT ''::text NOT NULL,
    grant_id uuid
);


--
-- Name: topic_visits; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.topic_visits (
    user_id uuid NOT NULL,
    topic_id uuid NOT NULL,
    hub_id uuid NOT NULL,
    first_visited_at timestamp with time zone DEFAULT now() NOT NULL,
    last_visited_at timestamp with time zone DEFAULT now() NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL
);


--
-- Name: topics; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.topics (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    owner_id uuid NOT NULL,
    hub_id uuid NOT NULL,
    parent_id uuid,
    name character varying(100) NOT NULL,
    description text DEFAULT ''::text NOT NULL,
    icon character varying(30) DEFAULT 'folder'::character varying NOT NULL,
    "position" integer DEFAULT 0 NOT NULL,
    pinned boolean DEFAULT false NOT NULL,
    user_modified boolean DEFAULT false NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL
);


--
-- Name: usage; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.usage (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    user_id uuid NOT NULL,
    period_start date NOT NULL,
    period_end date NOT NULL,
    push_count integer DEFAULT 0 NOT NULL,
    recall_count integer DEFAULT 0 NOT NULL,
    ask_count integer DEFAULT 0 NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL
);


--
-- Name: usage_events; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.usage_events (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    user_id uuid,
    hub_id uuid,
    operation text NOT NULL,
    source text DEFAULT ''::text NOT NULL,
    agent_name text DEFAULT ''::text NOT NULL,
    metadata jsonb DEFAULT '{}'::jsonb NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL
);


--
-- Name: user_preferences; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.user_preferences (
    user_id uuid NOT NULL,
    settings jsonb DEFAULT '{}'::jsonb NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL
);


--
-- Name: users; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.users (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    github_id bigint,
    email text DEFAULT ''::text NOT NULL,
    name text DEFAULT ''::text NOT NULL,
    avatar_url text DEFAULT ''::text NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    plan text,
    display_name text DEFAULT ''::text NOT NULL,
    can_create_hub boolean DEFAULT false NOT NULL,
    invite_id uuid,
    personal_plan_id text DEFAULT 'personal_free'::text NOT NULL,
    personal_plan_scope text DEFAULT 'personal'::text NOT NULL,
    email_opt_out_marketing boolean DEFAULT false NOT NULL,
    email_opt_out_token text DEFAULT ''::text NOT NULL,
    CONSTRAINT users_personal_plan_scope_check CHECK ((personal_plan_scope = 'personal'::text))
);


--
-- Name: waitlist; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.waitlist (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    email text NOT NULL,
    use_case text DEFAULT ''::text NOT NULL,
    ai_tools text[] DEFAULT '{}'::text[] NOT NULL,
    role text DEFAULT ''::text NOT NULL,
    status text DEFAULT 'pending'::text NOT NULL,
    wave integer,
    notes text DEFAULT ''::text NOT NULL,
    approved_by uuid,
    approved_at timestamp with time zone,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL
);


--
-- Name: waitlist_invites; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.waitlist_invites (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    waitlist_id uuid NOT NULL,
    email text NOT NULL,
    token text NOT NULL,
    status text DEFAULT 'active'::text NOT NULL,
    expires_at timestamp with time zone NOT NULL,
    used_at timestamp with time zone,
    used_by uuid,
    created_at timestamp with time zone DEFAULT now() NOT NULL
);


--
-- Name: admin_audit admin_audit_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.admin_audit
    ADD CONSTRAINT admin_audit_pkey PRIMARY KEY (id);


--
-- Name: admin_roles admin_roles_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.admin_roles
    ADD CONSTRAINT admin_roles_pkey PRIMARY KEY (user_id, role);


--
-- Name: agent_config_sync_states agent_config_sync_states_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.agent_config_sync_states
    ADD CONSTRAINT agent_config_sync_states_pkey PRIMARY KEY (owner_id, device_id, agent, file_path, scope);


--
-- Name: agent_config_tombstones agent_config_tombstones_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.agent_config_tombstones
    ADD CONSTRAINT agent_config_tombstones_pkey PRIMARY KEY (owner_id, agent, file_path, scope);


--
-- Name: agent_configs agent_configs_owner_id_agent_file_path_scope_key; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.agent_configs
    ADD CONSTRAINT agent_configs_owner_id_agent_file_path_scope_key UNIQUE (owner_id, agent, file_path, scope);


--
-- Name: agent_configs agent_configs_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.agent_configs
    ADD CONSTRAINT agent_configs_pkey PRIMARY KEY (id);


--
-- Name: agent_session_snapshots agent_session_snapshots_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.agent_session_snapshots
    ADD CONSTRAINT agent_session_snapshots_pkey PRIMARY KEY (id);


--
-- Name: agent_session_sync_states agent_session_sync_states_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.agent_session_sync_states
    ADD CONSTRAINT agent_session_sync_states_pkey PRIMARY KEY (owner_id, device_id, agent, file_path, scope);


--
-- Name: agent_session_tombstones agent_session_tombstones_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.agent_session_tombstones
    ADD CONSTRAINT agent_session_tombstones_pkey PRIMARY KEY (owner_id, agent, file_path, scope);


--
-- Name: agent_sessions agent_sessions_owner_id_agent_file_path_scope_key; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.agent_sessions
    ADD CONSTRAINT agent_sessions_owner_id_agent_file_path_scope_key UNIQUE (owner_id, agent, file_path, scope);


--
-- Name: agent_sessions agent_sessions_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.agent_sessions
    ADD CONSTRAINT agent_sessions_pkey PRIMARY KEY (id);


--
-- Name: api_keys api_keys_key_hash_key; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.api_keys
    ADD CONSTRAINT api_keys_key_hash_key UNIQUE (key_hash);


--
-- Name: api_keys api_keys_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.api_keys
    ADD CONSTRAINT api_keys_pkey PRIMARY KEY (id);


--
-- Name: audiences audiences_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.audiences
    ADD CONSTRAINT audiences_pkey PRIMARY KEY (id);


--
-- Name: auth_codes auth_codes_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.auth_codes
    ADD CONSTRAINT auth_codes_pkey PRIMARY KEY (code);


--
-- Name: auth_identities auth_identities_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.auth_identities
    ADD CONSTRAINT auth_identities_pkey PRIMARY KEY (id);


--
-- Name: auth_identities auth_identities_provider_provider_id_key; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.auth_identities
    ADD CONSTRAINT auth_identities_provider_provider_id_key UNIQUE (provider, provider_id);


--
-- Name: billing_subscriptions billing_subscriptions_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.billing_subscriptions
    ADD CONSTRAINT billing_subscriptions_pkey PRIMARY KEY (id);


--
-- Name: campaign_deliveries campaign_deliveries_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.campaign_deliveries
    ADD CONSTRAINT campaign_deliveries_pkey PRIMARY KEY (id);


--
-- Name: campaign_deliveries campaign_deliveries_unique_recipient; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.campaign_deliveries
    ADD CONSTRAINT campaign_deliveries_unique_recipient UNIQUE (campaign_id, recipient_email);


--
-- Name: campaign_templates campaign_templates_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.campaign_templates
    ADD CONSTRAINT campaign_templates_pkey PRIMARY KEY (id);


--
-- Name: campaign_templates campaign_templates_slug_key; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.campaign_templates
    ADD CONSTRAINT campaign_templates_slug_key UNIQUE (slug);


--
-- Name: campaigns campaigns_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.campaigns
    ADD CONSTRAINT campaigns_pkey PRIMARY KEY (id);


--
-- Name: chunks chunks_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.chunks
    ADD CONSTRAINT chunks_pkey PRIMARY KEY (id);


--
-- Name: comms_audit comms_audit_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.comms_audit
    ADD CONSTRAINT comms_audit_pkey PRIMARY KEY (id);


--
-- Name: connected_agents connected_agents_owner_id_agent_name_key; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.connected_agents
    ADD CONSTRAINT connected_agents_owner_id_agent_name_key UNIQUE (owner_id, agent_name);


--
-- Name: connected_agents connected_agents_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.connected_agents
    ADD CONSTRAINT connected_agents_pkey PRIMARY KEY (id);


--
-- Name: dream_actions dream_actions_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.dream_actions
    ADD CONSTRAINT dream_actions_pkey PRIMARY KEY (id);


--
-- Name: dream_runs dream_runs_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.dream_runs
    ADD CONSTRAINT dream_runs_pkey PRIMARY KEY (id);


--
-- Name: effective_plan_cache effective_plan_cache_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.effective_plan_cache
    ADD CONSTRAINT effective_plan_cache_pkey PRIMARY KEY (user_id);


--
-- Name: email_brand_settings email_brand_settings_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.email_brand_settings
    ADD CONSTRAINT email_brand_settings_pkey PRIMARY KEY (id);


--
-- Name: email_template_override_revisions email_template_override_revisions_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.email_template_override_revisions
    ADD CONSTRAINT email_template_override_revisions_pkey PRIMARY KEY (id);


--
-- Name: email_template_overrides email_template_overrides_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.email_template_overrides
    ADD CONSTRAINT email_template_overrides_pkey PRIMARY KEY (name);


--
-- Name: hub_invites hub_invites_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.hub_invites
    ADD CONSTRAINT hub_invites_pkey PRIMARY KEY (id);


--
-- Name: hub_invites hub_invites_token_key; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.hub_invites
    ADD CONSTRAINT hub_invites_token_key UNIQUE (token);


--
-- Name: hub_members hub_members_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.hub_members
    ADD CONSTRAINT hub_members_pkey PRIMARY KEY (hub_id, user_id);


--
-- Name: hub_ownership_transfers hub_ownership_transfers_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.hub_ownership_transfers
    ADD CONSTRAINT hub_ownership_transfers_pkey PRIMARY KEY (id);


--
-- Name: hub_subscriptions hub_subscriptions_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.hub_subscriptions
    ADD CONSTRAINT hub_subscriptions_pkey PRIMARY KEY (id);


--
-- Name: hub_visits hub_visits_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.hub_visits
    ADD CONSTRAINT hub_visits_pkey PRIMARY KEY (user_id, hub_id);


--
-- Name: hubs hubs_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.hubs
    ADD CONSTRAINT hubs_pkey PRIMARY KEY (id);


--
-- Name: hubs hubs_slug_key; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.hubs
    ADD CONSTRAINT hubs_slug_key UNIQUE (slug);


--
-- Name: memories memories_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.memories
    ADD CONSTRAINT memories_pkey PRIMARY KEY (id);


--
-- Name: memory_attachments memory_attachments_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.memory_attachments
    ADD CONSTRAINT memory_attachments_pkey PRIMARY KEY (id);


--
-- Name: memory_topics memory_topics_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.memory_topics
    ADD CONSTRAINT memory_topics_pkey PRIMARY KEY (memory_id, topic_id);


--
-- Name: notifications notifications_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.notifications
    ADD CONSTRAINT notifications_pkey PRIMARY KEY (id);


--
-- Name: notifications notifications_source_unique; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.notifications
    ADD CONSTRAINT notifications_source_unique UNIQUE (source_kind, source_id);


--
-- Name: oauth_authorization_requests oauth_authorization_requests_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.oauth_authorization_requests
    ADD CONSTRAINT oauth_authorization_requests_pkey PRIMARY KEY (id);


--
-- Name: oauth_clients oauth_clients_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.oauth_clients
    ADD CONSTRAINT oauth_clients_pkey PRIMARY KEY (client_id);


--
-- Name: oauth_grants oauth_grants_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.oauth_grants
    ADD CONSTRAINT oauth_grants_pkey PRIMARY KEY (id);


--
-- Name: oauth_states oauth_states_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.oauth_states
    ADD CONSTRAINT oauth_states_pkey PRIMARY KEY (id);


--
-- Name: oauth_states oauth_states_state_hash_key; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.oauth_states
    ADD CONSTRAINT oauth_states_state_hash_key UNIQUE (state_hash);


--
-- Name: plan_migration_anomalies plan_migration_anomalies_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.plan_migration_anomalies
    ADD CONSTRAINT plan_migration_anomalies_pkey PRIMARY KEY (id);


--
-- Name: plan_overrides plan_overrides_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.plan_overrides
    ADD CONSTRAINT plan_overrides_pkey PRIMARY KEY (user_id);


--
-- Name: plans plans_id_scope_unique; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.plans
    ADD CONSTRAINT plans_id_scope_unique UNIQUE (id, scope);


--
-- Name: plans plans_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.plans
    ADD CONSTRAINT plans_pkey PRIMARY KEY (id);


--
-- Name: reviews reviews_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.reviews
    ADD CONSTRAINT reviews_pkey PRIMARY KEY (id);


--
-- Name: sessions sessions_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.sessions
    ADD CONSTRAINT sessions_pkey PRIMARY KEY (id);


--
-- Name: sessions sessions_refresh_token_key; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.sessions
    ADD CONSTRAINT sessions_refresh_token_key UNIQUE (refresh_token);


--
-- Name: topic_visits topic_visits_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.topic_visits
    ADD CONSTRAINT topic_visits_pkey PRIMARY KEY (user_id, topic_id);


--
-- Name: topics topics_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.topics
    ADD CONSTRAINT topics_pkey PRIMARY KEY (id);


--
-- Name: usage_events usage_events_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.usage_events
    ADD CONSTRAINT usage_events_pkey PRIMARY KEY (id);


--
-- Name: usage usage_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.usage
    ADD CONSTRAINT usage_pkey PRIMARY KEY (id);


--
-- Name: usage usage_user_id_period_start_key; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.usage
    ADD CONSTRAINT usage_user_id_period_start_key UNIQUE (user_id, period_start);


--
-- Name: user_preferences user_preferences_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.user_preferences
    ADD CONSTRAINT user_preferences_pkey PRIMARY KEY (user_id);


--
-- Name: users users_github_id_key; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.users
    ADD CONSTRAINT users_github_id_key UNIQUE (github_id);


--
-- Name: users users_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.users
    ADD CONSTRAINT users_pkey PRIMARY KEY (id);


--
-- Name: waitlist_invites waitlist_invites_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.waitlist_invites
    ADD CONSTRAINT waitlist_invites_pkey PRIMARY KEY (id);


--
-- Name: waitlist_invites waitlist_invites_token_key; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.waitlist_invites
    ADD CONSTRAINT waitlist_invites_token_key UNIQUE (token);


--
-- Name: waitlist waitlist_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.waitlist
    ADD CONSTRAINT waitlist_pkey PRIMARY KEY (id);


--
-- Name: audiences_created_at_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX audiences_created_at_idx ON public.audiences USING btree (created_at DESC);


--
-- Name: campaign_deliveries_campaign_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX campaign_deliveries_campaign_idx ON public.campaign_deliveries USING btree (campaign_id, status);


--
-- Name: campaign_deliveries_provider_msg_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX campaign_deliveries_provider_msg_idx ON public.campaign_deliveries USING btree (provider_message_id) WHERE (provider_message_id <> ''::text);


--
-- Name: campaign_templates_active_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX campaign_templates_active_idx ON public.campaign_templates USING btree (updated_at DESC) WHERE (is_archived = false);


--
-- Name: campaigns_audience_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX campaigns_audience_idx ON public.campaigns USING btree (audience_id) WHERE (audience_id IS NOT NULL);


--
-- Name: campaigns_created_at_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX campaigns_created_at_idx ON public.campaigns USING btree (created_at DESC);


--
-- Name: campaigns_status_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX campaigns_status_idx ON public.campaigns USING btree (status);


--
-- Name: comms_audit_actor_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX comms_audit_actor_idx ON public.comms_audit USING btree (actor_id, created_at DESC) WHERE (actor_id IS NOT NULL);


--
-- Name: comms_audit_resource_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX comms_audit_resource_idx ON public.comms_audit USING btree (resource_type, resource_id, created_at DESC);


--
-- Name: idx_admin_audit_actor; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_admin_audit_actor ON public.admin_audit USING btree (actor_id, created_at DESC) WHERE (actor_id IS NOT NULL);


--
-- Name: idx_admin_audit_resource; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_admin_audit_resource ON public.admin_audit USING btree (resource_type, resource_id, created_at DESC);


--
-- Name: idx_agent_config_sync_states_owner_device; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_agent_config_sync_states_owner_device ON public.agent_config_sync_states USING btree (owner_id, device_id);


--
-- Name: idx_agent_config_tombstones_owner; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_agent_config_tombstones_owner ON public.agent_config_tombstones USING btree (owner_id);


--
-- Name: idx_agent_configs_owner; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_agent_configs_owner ON public.agent_configs USING btree (owner_id);


--
-- Name: idx_agent_configs_owner_agent; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_agent_configs_owner_agent ON public.agent_configs USING btree (owner_id, agent);


--
-- Name: idx_agent_configs_owner_scope; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_agent_configs_owner_scope ON public.agent_configs USING btree (owner_id, scope);


--
-- Name: idx_agent_session_sync_states_owner_device; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_agent_session_sync_states_owner_device ON public.agent_session_sync_states USING btree (owner_id, device_id);


--
-- Name: idx_agent_session_tombstones_owner; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_agent_session_tombstones_owner ON public.agent_session_tombstones USING btree (owner_id);


--
-- Name: idx_agent_sessions_owner; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_agent_sessions_owner ON public.agent_sessions USING btree (owner_id);


--
-- Name: idx_agent_sessions_owner_agent; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_agent_sessions_owner_agent ON public.agent_sessions USING btree (owner_id, agent);


--
-- Name: idx_agent_sessions_owner_scope; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_agent_sessions_owner_scope ON public.agent_sessions USING btree (owner_id, scope);


--
-- Name: idx_api_keys_hash; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_api_keys_hash ON public.api_keys USING btree (key_hash);


--
-- Name: idx_api_keys_hub_scope_mode; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_api_keys_hub_scope_mode ON public.api_keys USING btree (hub_scope_mode);


--
-- Name: idx_api_keys_user; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_api_keys_user ON public.api_keys USING btree (user_id);


--
-- Name: idx_auth_identities_provider_email; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_auth_identities_provider_email ON public.auth_identities USING btree (provider_email) WHERE (provider_email <> ''::text);


--
-- Name: idx_auth_identities_user_id; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_auth_identities_user_id ON public.auth_identities USING btree (user_id);


--
-- Name: idx_billing_sub_active; Type: INDEX; Schema: public; Owner: -
--

CREATE UNIQUE INDEX idx_billing_sub_active ON public.billing_subscriptions USING btree (user_id) WHERE (status = 'active'::text);


--
-- Name: idx_chunks_embedding; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_chunks_embedding ON public.chunks USING hnsw (embedding public.vector_cosine_ops) WITH (m='16', ef_construction='64');


--
-- Name: idx_chunks_kind; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_chunks_kind ON public.chunks USING btree (kind);


--
-- Name: idx_chunks_memory; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_chunks_memory ON public.chunks USING btree (memory_id);


--
-- Name: idx_chunks_project_repo; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_chunks_project_repo ON public.chunks USING btree (project_repo) WHERE (project_repo <> ''::text);


--
-- Name: idx_chunks_search_trgm; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_chunks_search_trgm ON public.chunks USING gin (search_text public.gin_trgm_ops);


--
-- Name: idx_chunks_search_vector; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_chunks_search_vector ON public.chunks USING gin (search_vector) WHERE (search_vector IS NOT NULL);


--
-- Name: idx_connected_agents_owner; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_connected_agents_owner ON public.connected_agents USING btree (owner_id);


--
-- Name: idx_dream_actions_created_at_desc; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_dream_actions_created_at_desc ON public.dream_actions USING btree (created_at DESC);


--
-- Name: idx_dream_actions_run; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_dream_actions_run ON public.dream_actions USING btree (run_id);


--
-- Name: idx_dream_actions_source_memory_ids; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_dream_actions_source_memory_ids ON public.dream_actions USING gin (source_memory_ids);


--
-- Name: idx_dream_runs_hub; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_dream_runs_hub ON public.dream_runs USING btree (hub_id, started_at DESC);


--
-- Name: idx_dream_runs_one_active_per_hub; Type: INDEX; Schema: public; Owner: -
--

CREATE UNIQUE INDEX idx_dream_runs_one_active_per_hub ON public.dream_runs USING btree (hub_id) WHERE (status = 'running'::text);


--
-- Name: idx_email_template_override_revisions_name_created_at; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_email_template_override_revisions_name_created_at ON public.email_template_override_revisions USING btree (name, created_at DESC);


--
-- Name: idx_email_template_overrides_updated_at; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_email_template_overrides_updated_at ON public.email_template_overrides USING btree (updated_at DESC);


--
-- Name: idx_hub_invites_email; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_hub_invites_email ON public.hub_invites USING btree (hub_id, invitee_email) WHERE (invitee_email IS NOT NULL);


--
-- Name: idx_hub_invites_hub; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_hub_invites_hub ON public.hub_invites USING btree (hub_id);


--
-- Name: idx_hub_invites_invitee_pending; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_hub_invites_invitee_pending ON public.hub_invites USING btree (invitee_user_id) WHERE ((invitee_user_id IS NOT NULL) AND (accepted_by IS NULL) AND (revoked_at IS NULL));


--
-- Name: idx_hub_invites_token; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_hub_invites_token ON public.hub_invites USING btree (token) WHERE ((accepted_by IS NULL) AND (revoked_at IS NULL));


--
-- Name: idx_hub_members_admin_keyset; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_hub_members_admin_keyset ON public.hub_members USING btree (hub_id, joined_at, user_id);


--
-- Name: idx_hub_members_user; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_hub_members_user ON public.hub_members USING btree (user_id);


--
-- Name: idx_hub_ownership_transfers_hub; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_hub_ownership_transfers_hub ON public.hub_ownership_transfers USING btree (hub_id);


--
-- Name: idx_hub_ownership_transfers_target; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_hub_ownership_transfers_target ON public.hub_ownership_transfers USING btree (target_user_id);


--
-- Name: idx_hub_sub_active; Type: INDEX; Schema: public; Owner: -
--

CREATE UNIQUE INDEX idx_hub_sub_active ON public.hub_subscriptions USING btree (hub_id) WHERE (status = 'active'::text);


--
-- Name: idx_hub_sub_billing_user; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_hub_sub_billing_user ON public.hub_subscriptions USING btree (billing_user_id) WHERE (status = 'active'::text);


--
-- Name: idx_hub_visits_hub_last_visited; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_hub_visits_hub_last_visited ON public.hub_visits USING btree (hub_id, last_visited_at DESC);


--
-- Name: idx_hubs_admin_keyset; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_hubs_admin_keyset ON public.hubs USING btree (created_at DESC, id DESC) WHERE (hub_type = 'team'::text);


--
-- Name: idx_hubs_owner; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_hubs_owner ON public.hubs USING btree (owner_id);


--
-- Name: idx_memories_content_hash; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_memories_content_hash ON public.memories USING btree (content_hash);


--
-- Name: idx_memories_created; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_memories_created ON public.memories USING btree (created_at DESC);


--
-- Name: idx_memories_created_by_slug; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_memories_created_by_slug ON public.memories USING btree (owner_id, created_by_slug) WHERE (created_by_slug <> ''::text);


--
-- Name: idx_memories_failed_summary; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_memories_failed_summary ON public.memories USING btree (created_at DESC) WHERE ((state = 'active'::text) AND (summary ~~ 'Processing failed after%'::text));


--
-- Name: idx_memories_hub; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_memories_hub ON public.memories USING btree (hub_id);


--
-- Name: idx_memories_kind; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_memories_kind ON public.memories USING btree (kind);


--
-- Name: idx_memories_owner; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_memories_owner ON public.memories USING btree (owner_id);


--
-- Name: idx_memories_processing_age; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_memories_processing_age ON public.memories USING btree (created_at) WHERE (state = 'processing'::text);


--
-- Name: idx_memories_project; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_memories_project ON public.memories USING gin (project_context);


--
-- Name: idx_memories_source_agent; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_memories_source_agent ON public.memories USING btree (source_agent) WHERE (source_agent <> ''::text);


--
-- Name: idx_memories_source_path; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_memories_source_path ON public.memories USING btree (source_path) WHERE (source_path <> ''::text);


--
-- Name: idx_memories_stability; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_memories_stability ON public.memories USING btree (stability);


--
-- Name: idx_memories_state; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_memories_state ON public.memories USING btree (state);


--
-- Name: idx_memories_tags_gin; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_memories_tags_gin ON public.memories USING gin (tags);


--
-- Name: idx_memories_title_trgm; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_memories_title_trgm ON public.memories USING gin (public.immutable_unaccent(lower(title)) public.gin_trgm_ops);


--
-- Name: idx_memory_attachments_memory; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_memory_attachments_memory ON public.memory_attachments USING btree (memory_id);


--
-- Name: idx_memory_attachments_memory_sha; Type: INDEX; Schema: public; Owner: -
--

CREATE UNIQUE INDEX idx_memory_attachments_memory_sha ON public.memory_attachments USING btree (memory_id, sha256) WHERE (sha256 <> ''::text);


--
-- Name: idx_memory_attachments_owner; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_memory_attachments_owner ON public.memory_attachments USING btree (owner_id);


--
-- Name: idx_memory_topics_memory; Type: INDEX; Schema: public; Owner: -
--

CREATE UNIQUE INDEX idx_memory_topics_memory ON public.memory_topics USING btree (memory_id);


--
-- Name: idx_memory_topics_topic; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_memory_topics_topic ON public.memory_topics USING btree (topic_id);


--
-- Name: idx_notifications_dream_run_id; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_notifications_dream_run_id ON public.notifications USING btree (dream_run_id) WHERE (dream_run_id IS NOT NULL);


--
-- Name: idx_oauth_authorization_requests_expires; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_oauth_authorization_requests_expires ON public.oauth_authorization_requests USING btree (expires_at);


--
-- Name: idx_oauth_grants_active; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_oauth_grants_active ON public.oauth_grants USING btree (user_id, client_id) WHERE (revoked_at IS NULL);


--
-- Name: idx_oauth_grants_client; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_oauth_grants_client ON public.oauth_grants USING btree (client_id);


--
-- Name: idx_oauth_grants_user; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_oauth_grants_user ON public.oauth_grants USING btree (user_id);


--
-- Name: idx_oauth_states_expires; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_oauth_states_expires ON public.oauth_states USING btree (expires_at);


--
-- Name: idx_reviews_hub_pending; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_reviews_hub_pending ON public.reviews USING btree (hub_id) WHERE (status = 'pending'::text);


--
-- Name: idx_reviews_hub_pending_key; Type: INDEX; Schema: public; Owner: -
--

CREATE UNIQUE INDEX idx_reviews_hub_pending_key ON public.reviews USING btree (hub_id, review_key) WHERE ((status = 'pending'::text) AND (review_key IS NOT NULL));


--
-- Name: idx_reviews_hub_status; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_reviews_hub_status ON public.reviews USING btree (hub_id, status);


--
-- Name: idx_session_snapshots_expiry; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_session_snapshots_expiry ON public.agent_session_snapshots USING btree (content_expires_at) WHERE ((purged_at IS NULL) AND (content_expires_at IS NOT NULL));


--
-- Name: idx_session_snapshots_owner; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_session_snapshots_owner ON public.agent_session_snapshots USING btree (owner_id, created_at DESC);


--
-- Name: idx_session_snapshots_session; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_session_snapshots_session ON public.agent_session_snapshots USING btree (session_id) WHERE (session_id IS NOT NULL);


--
-- Name: idx_sessions_refresh; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_sessions_refresh ON public.sessions USING btree (refresh_token);


--
-- Name: idx_sessions_user; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_sessions_user ON public.sessions USING btree (user_id);


--
-- Name: idx_topic_visits_hub_user; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_topic_visits_hub_user ON public.topic_visits USING btree (hub_id, user_id);


--
-- Name: idx_topic_visits_topic_last_visited; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_topic_visits_topic_last_visited ON public.topic_visits USING btree (topic_id, last_visited_at DESC);


--
-- Name: idx_topics_hub; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_topics_hub ON public.topics USING btree (hub_id);


--
-- Name: idx_topics_parent; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_topics_parent ON public.topics USING btree (parent_id);


--
-- Name: idx_topics_root_unique_hub; Type: INDEX; Schema: public; Owner: -
--

CREATE UNIQUE INDEX idx_topics_root_unique_hub ON public.topics USING btree (hub_id, lower((name)::text)) WHERE (parent_id IS NULL);


--
-- Name: idx_topics_unique_name_hub; Type: INDEX; Schema: public; Owner: -
--

CREATE UNIQUE INDEX idx_topics_unique_name_hub ON public.topics USING btree (hub_id, parent_id, lower((name)::text)) WHERE (parent_id IS NOT NULL);


--
-- Name: idx_usage_events_op_created; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_usage_events_op_created ON public.usage_events USING btree (operation, created_at);


--
-- Name: idx_usage_events_user_created; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_usage_events_user_created ON public.usage_events USING btree (user_id, created_at) WHERE (user_id IS NOT NULL);


--
-- Name: idx_usage_events_user_op_created; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_usage_events_user_op_created ON public.usage_events USING btree (user_id, operation, created_at) WHERE (user_id IS NOT NULL);


--
-- Name: idx_usage_user_period; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_usage_user_period ON public.usage USING btree (user_id, period_start DESC);


--
-- Name: idx_users_email; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_users_email ON public.users USING btree (email) WHERE (email <> ''::text);


--
-- Name: idx_users_email_unique; Type: INDEX; Schema: public; Owner: -
--

CREATE UNIQUE INDEX idx_users_email_unique ON public.users USING btree (lower(btrim(email))) WHERE (btrim(email) <> ''::text);


--
-- Name: idx_users_github_id; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_users_github_id ON public.users USING btree (github_id);


--
-- Name: idx_waitlist_created_at; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_waitlist_created_at ON public.waitlist USING btree (created_at);


--
-- Name: idx_waitlist_email_unique; Type: INDEX; Schema: public; Owner: -
--

CREATE UNIQUE INDEX idx_waitlist_email_unique ON public.waitlist USING btree (lower(btrim(email)));


--
-- Name: idx_waitlist_invites_expires_at; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_waitlist_invites_expires_at ON public.waitlist_invites USING btree (expires_at);


--
-- Name: idx_waitlist_invites_status; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_waitlist_invites_status ON public.waitlist_invites USING btree (status);


--
-- Name: idx_waitlist_invites_token; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_waitlist_invites_token ON public.waitlist_invites USING btree (token);


--
-- Name: idx_waitlist_status; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_waitlist_status ON public.waitlist USING btree (status);


--
-- Name: notifications_expires_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX notifications_expires_idx ON public.notifications USING btree (expires_at) WHERE ((status = 'pending'::text) AND (expires_at IS NOT NULL));


--
-- Name: notifications_hub_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX notifications_hub_idx ON public.notifications USING btree (hub_id, status, created_at DESC) WHERE (hub_id IS NOT NULL);


--
-- Name: notifications_recipient_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX notifications_recipient_idx ON public.notifications USING btree (recipient_user_id, status, created_at DESC) WHERE (recipient_user_id IS NOT NULL);


--
-- Name: users_email_opt_out_token_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE UNIQUE INDEX users_email_opt_out_token_idx ON public.users USING btree (email_opt_out_token) WHERE (email_opt_out_token <> ''::text);


--
-- Name: chunks trg_chunks_search_columns; Type: TRIGGER; Schema: public; Owner: -
--

CREATE TRIGGER trg_chunks_search_columns BEFORE INSERT OR UPDATE ON public.chunks FOR EACH ROW EXECUTE FUNCTION public.chunks_search_columns_update();


--
-- Name: topics trg_topics_same_hub_parent; Type: TRIGGER; Schema: public; Owner: -
--

CREATE TRIGGER trg_topics_same_hub_parent BEFORE INSERT OR UPDATE OF parent_id, hub_id ON public.topics FOR EACH ROW EXECUTE FUNCTION public.enforce_topic_same_hub_parent();


--
-- Name: admin_audit admin_audit_actor_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.admin_audit
    ADD CONSTRAINT admin_audit_actor_id_fkey FOREIGN KEY (actor_id) REFERENCES public.users(id) ON DELETE SET NULL;


--
-- Name: admin_roles admin_roles_granted_by_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.admin_roles
    ADD CONSTRAINT admin_roles_granted_by_fkey FOREIGN KEY (granted_by) REFERENCES public.users(id);


--
-- Name: admin_roles admin_roles_user_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.admin_roles
    ADD CONSTRAINT admin_roles_user_id_fkey FOREIGN KEY (user_id) REFERENCES public.users(id) ON DELETE CASCADE;


--
-- Name: agent_session_snapshots agent_session_snapshots_owner_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.agent_session_snapshots
    ADD CONSTRAINT agent_session_snapshots_owner_id_fkey FOREIGN KEY (owner_id) REFERENCES public.users(id) ON DELETE CASCADE;


--
-- Name: agent_session_snapshots agent_session_snapshots_session_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.agent_session_snapshots
    ADD CONSTRAINT agent_session_snapshots_session_id_fkey FOREIGN KEY (session_id) REFERENCES public.agent_sessions(id) ON DELETE SET NULL;


--
-- Name: api_keys api_keys_hub_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.api_keys
    ADD CONSTRAINT api_keys_hub_id_fkey FOREIGN KEY (hub_id) REFERENCES public.hubs(id) ON DELETE CASCADE;


--
-- Name: api_keys api_keys_user_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.api_keys
    ADD CONSTRAINT api_keys_user_id_fkey FOREIGN KEY (user_id) REFERENCES public.users(id) ON DELETE CASCADE;


--
-- Name: audiences audiences_created_by_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.audiences
    ADD CONSTRAINT audiences_created_by_fkey FOREIGN KEY (created_by) REFERENCES public.users(id) ON DELETE RESTRICT;


--
-- Name: auth_codes auth_codes_grant_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.auth_codes
    ADD CONSTRAINT auth_codes_grant_id_fkey FOREIGN KEY (grant_id) REFERENCES public.oauth_grants(id) ON DELETE CASCADE;


--
-- Name: auth_codes auth_codes_user_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.auth_codes
    ADD CONSTRAINT auth_codes_user_id_fkey FOREIGN KEY (user_id) REFERENCES public.users(id) ON DELETE CASCADE;


--
-- Name: auth_identities auth_identities_user_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.auth_identities
    ADD CONSTRAINT auth_identities_user_id_fkey FOREIGN KEY (user_id) REFERENCES public.users(id) ON DELETE CASCADE;


--
-- Name: billing_subscriptions billing_subscriptions_plan_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.billing_subscriptions
    ADD CONSTRAINT billing_subscriptions_plan_id_fkey FOREIGN KEY (plan_id) REFERENCES public.plans(id);


--
-- Name: billing_subscriptions billing_subscriptions_plan_scope_fk; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.billing_subscriptions
    ADD CONSTRAINT billing_subscriptions_plan_scope_fk FOREIGN KEY (plan_id, plan_scope) REFERENCES public.plans(id, scope);


--
-- Name: billing_subscriptions billing_subscriptions_user_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.billing_subscriptions
    ADD CONSTRAINT billing_subscriptions_user_id_fkey FOREIGN KEY (user_id) REFERENCES public.users(id) ON DELETE CASCADE;


--
-- Name: campaign_deliveries campaign_deliveries_campaign_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.campaign_deliveries
    ADD CONSTRAINT campaign_deliveries_campaign_fkey FOREIGN KEY (campaign_id) REFERENCES public.campaigns(id) ON DELETE CASCADE;


--
-- Name: campaign_deliveries campaign_deliveries_recipient_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.campaign_deliveries
    ADD CONSTRAINT campaign_deliveries_recipient_fkey FOREIGN KEY (recipient_user_id) REFERENCES public.users(id) ON DELETE SET NULL;


--
-- Name: campaign_templates campaign_templates_created_by_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.campaign_templates
    ADD CONSTRAINT campaign_templates_created_by_fkey FOREIGN KEY (created_by) REFERENCES public.users(id) ON DELETE SET NULL;


--
-- Name: campaign_templates campaign_templates_updated_by_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.campaign_templates
    ADD CONSTRAINT campaign_templates_updated_by_fkey FOREIGN KEY (updated_by) REFERENCES public.users(id) ON DELETE SET NULL;


--
-- Name: campaigns campaigns_audience_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.campaigns
    ADD CONSTRAINT campaigns_audience_id_fkey FOREIGN KEY (audience_id) REFERENCES public.audiences(id) ON DELETE SET NULL;


--
-- Name: campaigns campaigns_cancelled_by_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.campaigns
    ADD CONSTRAINT campaigns_cancelled_by_fkey FOREIGN KEY (cancelled_by) REFERENCES public.users(id) ON DELETE SET NULL;


--
-- Name: campaigns campaigns_created_by_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.campaigns
    ADD CONSTRAINT campaigns_created_by_fkey FOREIGN KEY (created_by) REFERENCES public.users(id) ON DELETE RESTRICT;


--
-- Name: campaigns campaigns_updated_by_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.campaigns
    ADD CONSTRAINT campaigns_updated_by_fkey FOREIGN KEY (updated_by) REFERENCES public.users(id) ON DELETE SET NULL;


--
-- Name: chunks chunks_memory_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.chunks
    ADD CONSTRAINT chunks_memory_id_fkey FOREIGN KEY (memory_id) REFERENCES public.memories(id) ON DELETE CASCADE;


--
-- Name: comms_audit comms_audit_actor_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.comms_audit
    ADD CONSTRAINT comms_audit_actor_id_fkey FOREIGN KEY (actor_id) REFERENCES public.users(id) ON DELETE SET NULL;


--
-- Name: connected_agents connected_agents_owner_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.connected_agents
    ADD CONSTRAINT connected_agents_owner_id_fkey FOREIGN KEY (owner_id) REFERENCES public.users(id) ON DELETE CASCADE;


--
-- Name: dream_actions dream_actions_from_topic_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.dream_actions
    ADD CONSTRAINT dream_actions_from_topic_id_fkey FOREIGN KEY (from_topic_id) REFERENCES public.topics(id) ON DELETE SET NULL;


--
-- Name: dream_actions dream_actions_run_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.dream_actions
    ADD CONSTRAINT dream_actions_run_id_fkey FOREIGN KEY (run_id) REFERENCES public.dream_runs(id) ON DELETE CASCADE;


--
-- Name: dream_actions dream_actions_to_topic_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.dream_actions
    ADD CONSTRAINT dream_actions_to_topic_id_fkey FOREIGN KEY (to_topic_id) REFERENCES public.topics(id) ON DELETE SET NULL;


--
-- Name: effective_plan_cache effective_plan_cache_effective_plan_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.effective_plan_cache
    ADD CONSTRAINT effective_plan_cache_effective_plan_fkey FOREIGN KEY (effective_plan) REFERENCES public.plans(id);


--
-- Name: effective_plan_cache effective_plan_cache_user_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.effective_plan_cache
    ADD CONSTRAINT effective_plan_cache_user_id_fkey FOREIGN KEY (user_id) REFERENCES public.users(id) ON DELETE CASCADE;


--
-- Name: email_brand_settings email_brand_settings_updated_by_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.email_brand_settings
    ADD CONSTRAINT email_brand_settings_updated_by_fkey FOREIGN KEY (updated_by) REFERENCES public.users(id) ON DELETE SET NULL;


--
-- Name: email_template_override_revisions email_template_override_revisions_created_by_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.email_template_override_revisions
    ADD CONSTRAINT email_template_override_revisions_created_by_fkey FOREIGN KEY (created_by) REFERENCES public.users(id) ON DELETE SET NULL;


--
-- Name: email_template_overrides email_template_overrides_updated_by_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.email_template_overrides
    ADD CONSTRAINT email_template_overrides_updated_by_fkey FOREIGN KEY (updated_by) REFERENCES public.users(id) ON DELETE SET NULL;


--
-- Name: dream_runs fk_dream_runs_hub; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.dream_runs
    ADD CONSTRAINT fk_dream_runs_hub FOREIGN KEY (hub_id) REFERENCES public.hubs(id) ON DELETE CASCADE;


--
-- Name: reviews fk_reviews_hub; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.reviews
    ADD CONSTRAINT fk_reviews_hub FOREIGN KEY (hub_id) REFERENCES public.hubs(id) ON DELETE CASCADE;


--
-- Name: hub_invites hub_invites_hub_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.hub_invites
    ADD CONSTRAINT hub_invites_hub_id_fkey FOREIGN KEY (hub_id) REFERENCES public.hubs(id) ON DELETE CASCADE;


--
-- Name: hub_invites hub_invites_invitee_user_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.hub_invites
    ADD CONSTRAINT hub_invites_invitee_user_id_fkey FOREIGN KEY (invitee_user_id) REFERENCES public.users(id) ON DELETE SET NULL;


--
-- Name: hub_members hub_members_hub_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.hub_members
    ADD CONSTRAINT hub_members_hub_id_fkey FOREIGN KEY (hub_id) REFERENCES public.hubs(id) ON DELETE CASCADE;


--
-- Name: hub_members hub_members_user_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.hub_members
    ADD CONSTRAINT hub_members_user_id_fkey FOREIGN KEY (user_id) REFERENCES public.users(id) ON DELETE CASCADE;


--
-- Name: hub_ownership_transfers hub_ownership_transfers_hub_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.hub_ownership_transfers
    ADD CONSTRAINT hub_ownership_transfers_hub_id_fkey FOREIGN KEY (hub_id) REFERENCES public.hubs(id) ON DELETE CASCADE;


--
-- Name: hub_ownership_transfers hub_ownership_transfers_initiated_by_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.hub_ownership_transfers
    ADD CONSTRAINT hub_ownership_transfers_initiated_by_fkey FOREIGN KEY (initiated_by) REFERENCES public.users(id) ON DELETE CASCADE;


--
-- Name: hub_ownership_transfers hub_ownership_transfers_target_user_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.hub_ownership_transfers
    ADD CONSTRAINT hub_ownership_transfers_target_user_id_fkey FOREIGN KEY (target_user_id) REFERENCES public.users(id) ON DELETE CASCADE;


--
-- Name: hub_subscriptions hub_subscriptions_billing_user_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.hub_subscriptions
    ADD CONSTRAINT hub_subscriptions_billing_user_id_fkey FOREIGN KEY (billing_user_id) REFERENCES public.users(id);


--
-- Name: hub_subscriptions hub_subscriptions_hub_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.hub_subscriptions
    ADD CONSTRAINT hub_subscriptions_hub_id_fkey FOREIGN KEY (hub_id) REFERENCES public.hubs(id) ON DELETE CASCADE;


--
-- Name: hub_subscriptions hub_subscriptions_plan_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.hub_subscriptions
    ADD CONSTRAINT hub_subscriptions_plan_id_fkey FOREIGN KEY (plan_id) REFERENCES public.plans(id);


--
-- Name: hub_subscriptions hub_subscriptions_plan_scope_fk; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.hub_subscriptions
    ADD CONSTRAINT hub_subscriptions_plan_scope_fk FOREIGN KEY (plan_id, plan_scope) REFERENCES public.plans(id, scope);


--
-- Name: hub_visits hub_visits_hub_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.hub_visits
    ADD CONSTRAINT hub_visits_hub_id_fkey FOREIGN KEY (hub_id) REFERENCES public.hubs(id) ON DELETE CASCADE;


--
-- Name: hub_visits hub_visits_user_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.hub_visits
    ADD CONSTRAINT hub_visits_user_id_fkey FOREIGN KEY (user_id) REFERENCES public.users(id) ON DELETE CASCADE;


--
-- Name: hubs hubs_owner_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.hubs
    ADD CONSTRAINT hubs_owner_id_fkey FOREIGN KEY (owner_id) REFERENCES public.users(id) ON DELETE CASCADE;


--
-- Name: memories memories_hub_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.memories
    ADD CONSTRAINT memories_hub_id_fkey FOREIGN KEY (hub_id) REFERENCES public.hubs(id) ON DELETE CASCADE;


--
-- Name: memory_attachments memory_attachments_memory_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.memory_attachments
    ADD CONSTRAINT memory_attachments_memory_id_fkey FOREIGN KEY (memory_id) REFERENCES public.memories(id) ON DELETE CASCADE;


--
-- Name: memory_topics memory_topics_memory_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.memory_topics
    ADD CONSTRAINT memory_topics_memory_id_fkey FOREIGN KEY (memory_id) REFERENCES public.memories(id) ON DELETE CASCADE;


--
-- Name: memory_topics memory_topics_topic_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.memory_topics
    ADD CONSTRAINT memory_topics_topic_id_fkey FOREIGN KEY (topic_id) REFERENCES public.topics(id) ON DELETE CASCADE;


--
-- Name: notifications notifications_hub_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.notifications
    ADD CONSTRAINT notifications_hub_fkey FOREIGN KEY (hub_id) REFERENCES public.hubs(id) ON DELETE CASCADE;


--
-- Name: notifications notifications_recipient_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.notifications
    ADD CONSTRAINT notifications_recipient_fkey FOREIGN KEY (recipient_user_id) REFERENCES public.users(id) ON DELETE CASCADE;


--
-- Name: oauth_authorization_requests oauth_authorization_requests_client_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.oauth_authorization_requests
    ADD CONSTRAINT oauth_authorization_requests_client_id_fkey FOREIGN KEY (client_id) REFERENCES public.oauth_clients(client_id) ON DELETE CASCADE;


--
-- Name: oauth_authorization_requests oauth_authorization_requests_user_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.oauth_authorization_requests
    ADD CONSTRAINT oauth_authorization_requests_user_id_fkey FOREIGN KEY (user_id) REFERENCES public.users(id) ON DELETE CASCADE;


--
-- Name: oauth_grants oauth_grants_client_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.oauth_grants
    ADD CONSTRAINT oauth_grants_client_id_fkey FOREIGN KEY (client_id) REFERENCES public.oauth_clients(client_id) ON DELETE CASCADE;


--
-- Name: oauth_grants oauth_grants_user_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.oauth_grants
    ADD CONSTRAINT oauth_grants_user_id_fkey FOREIGN KEY (user_id) REFERENCES public.users(id) ON DELETE CASCADE;


--
-- Name: oauth_states oauth_states_user_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.oauth_states
    ADD CONSTRAINT oauth_states_user_id_fkey FOREIGN KEY (user_id) REFERENCES public.users(id) ON DELETE CASCADE;


--
-- Name: plan_overrides plan_overrides_set_by_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.plan_overrides
    ADD CONSTRAINT plan_overrides_set_by_fkey FOREIGN KEY (set_by) REFERENCES public.users(id) ON DELETE SET NULL;


--
-- Name: plan_overrides plan_overrides_user_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.plan_overrides
    ADD CONSTRAINT plan_overrides_user_id_fkey FOREIGN KEY (user_id) REFERENCES public.users(id) ON DELETE CASCADE;


--
-- Name: reviews reviews_dream_run_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.reviews
    ADD CONSTRAINT reviews_dream_run_id_fkey FOREIGN KEY (dream_run_id) REFERENCES public.dream_runs(id) ON DELETE SET NULL;


--
-- Name: sessions sessions_grant_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.sessions
    ADD CONSTRAINT sessions_grant_id_fkey FOREIGN KEY (grant_id) REFERENCES public.oauth_grants(id) ON DELETE CASCADE;


--
-- Name: sessions sessions_user_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.sessions
    ADD CONSTRAINT sessions_user_id_fkey FOREIGN KEY (user_id) REFERENCES public.users(id) ON DELETE CASCADE;


--
-- Name: topic_visits topic_visits_hub_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.topic_visits
    ADD CONSTRAINT topic_visits_hub_id_fkey FOREIGN KEY (hub_id) REFERENCES public.hubs(id) ON DELETE CASCADE;


--
-- Name: topic_visits topic_visits_topic_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.topic_visits
    ADD CONSTRAINT topic_visits_topic_id_fkey FOREIGN KEY (topic_id) REFERENCES public.topics(id) ON DELETE CASCADE;


--
-- Name: topic_visits topic_visits_user_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.topic_visits
    ADD CONSTRAINT topic_visits_user_id_fkey FOREIGN KEY (user_id) REFERENCES public.users(id) ON DELETE CASCADE;


--
-- Name: topics topics_hub_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.topics
    ADD CONSTRAINT topics_hub_id_fkey FOREIGN KEY (hub_id) REFERENCES public.hubs(id) ON DELETE CASCADE;


--
-- Name: topics topics_parent_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.topics
    ADD CONSTRAINT topics_parent_id_fkey FOREIGN KEY (parent_id) REFERENCES public.topics(id);


--
-- Name: usage_events usage_events_user_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.usage_events
    ADD CONSTRAINT usage_events_user_id_fkey FOREIGN KEY (user_id) REFERENCES public.users(id) ON DELETE SET NULL;


--
-- Name: usage usage_user_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.usage
    ADD CONSTRAINT usage_user_id_fkey FOREIGN KEY (user_id) REFERENCES public.users(id) ON DELETE CASCADE;


--
-- Name: user_preferences user_preferences_user_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.user_preferences
    ADD CONSTRAINT user_preferences_user_id_fkey FOREIGN KEY (user_id) REFERENCES public.users(id) ON DELETE CASCADE;


--
-- Name: users users_invite_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.users
    ADD CONSTRAINT users_invite_id_fkey FOREIGN KEY (invite_id) REFERENCES public.waitlist_invites(id);


--
-- Name: users users_personal_plan_scope_fk; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.users
    ADD CONSTRAINT users_personal_plan_scope_fk FOREIGN KEY (personal_plan_id, personal_plan_scope) REFERENCES public.plans(id, scope);


--
-- Name: waitlist waitlist_approved_by_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.waitlist
    ADD CONSTRAINT waitlist_approved_by_fkey FOREIGN KEY (approved_by) REFERENCES public.users(id);


--
-- Name: waitlist_invites waitlist_invites_used_by_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.waitlist_invites
    ADD CONSTRAINT waitlist_invites_used_by_fkey FOREIGN KEY (used_by) REFERENCES public.users(id);


--
-- Name: waitlist_invites waitlist_invites_waitlist_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.waitlist_invites
    ADD CONSTRAINT waitlist_invites_waitlist_id_fkey FOREIGN KEY (waitlist_id) REFERENCES public.waitlist(id) ON DELETE CASCADE;




--
-- Seed data: plans table. Required for app boot + meter/ratelimit
-- resolution. Generated from a fresh migrated DB; idempotent via
-- ON CONFLICT DO NOTHING.
--

INSERT INTO public.plans (id, display_name, tier_order, monthly_price_cents, memory_limit, push_limit, recall_limit, ask_limit, ask_model, dreams_enabled, review_inbox, max_team_hubs, rate_limit_rpm, rate_limit_heavy_rpm, rate_limit_light_rpm, features, active, scope, entitlement_rank, max_owned_free_team_hubs, max_hub_members, seat_minimum, seat_billed, max_attachment_bytes, storage_bytes_limit) VALUES ('free', 'Free', 0, 0, 300, 200, 500, 10, 'haiku', false, false, 0, 60, 10, 60, '{}'::jsonb, false, 'personal', 0, 0, NULL, 0, false, 5242880, 536870912) ON CONFLICT (id) DO NOTHING;
INSERT INTO public.plans (id, display_name, tier_order, monthly_price_cents, memory_limit, push_limit, recall_limit, ask_limit, ask_model, dreams_enabled, review_inbox, max_team_hubs, rate_limit_rpm, rate_limit_heavy_rpm, rate_limit_light_rpm, features, active, scope, entitlement_rank, max_owned_free_team_hubs, max_hub_members, seat_minimum, seat_billed, max_attachment_bytes, storage_bytes_limit) VALUES ('hub_free_team', 'Free Team', 0, 0, 300, 500, 2000, 50, 'haiku', false, false, -1, 120, 15, 120, '{}'::jsonb, true, 'hub', 10, 0, 3, 0, false, 5242880, 536870912) ON CONFLICT (id) DO NOTHING;
INSERT INTO public.plans (id, display_name, tier_order, monthly_price_cents, memory_limit, push_limit, recall_limit, ask_limit, ask_model, dreams_enabled, review_inbox, max_team_hubs, rate_limit_rpm, rate_limit_heavy_rpm, rate_limit_light_rpm, features, active, scope, entitlement_rank, max_owned_free_team_hubs, max_hub_members, seat_minimum, seat_billed, max_attachment_bytes, storage_bytes_limit) VALUES ('personal_free', 'Free', 0, 0, 300, 200, 500, 10, 'haiku', false, false, 0, 60, 10, 60, '{}'::jsonb, true, 'personal', 0, 0, NULL, 0, false, 5242880, 536870912) ON CONFLICT (id) DO NOTHING;
INSERT INTO public.plans (id, display_name, tier_order, monthly_price_cents, memory_limit, push_limit, recall_limit, ask_limit, ask_model, dreams_enabled, review_inbox, max_team_hubs, rate_limit_rpm, rate_limit_heavy_rpm, rate_limit_light_rpm, features, active, scope, entitlement_rank, max_owned_free_team_hubs, max_hub_members, seat_minimum, seat_billed, max_attachment_bytes, storage_bytes_limit) VALUES ('early_access', 'Early Access', 15, 0, 10000, 2000, -1, 150, 'sonnet', true, true, 1, 120, 15, 120, '{}'::jsonb, false, 'personal', 15, 1, NULL, 0, false, 52428800, 5368709120) ON CONFLICT (id) DO NOTHING;
INSERT INTO public.plans (id, display_name, tier_order, monthly_price_cents, memory_limit, push_limit, recall_limit, ask_limit, ask_model, dreams_enabled, review_inbox, max_team_hubs, rate_limit_rpm, rate_limit_heavy_rpm, rate_limit_light_rpm, features, active, scope, entitlement_rank, max_owned_free_team_hubs, max_hub_members, seat_minimum, seat_billed, max_attachment_bytes, storage_bytes_limit) VALUES ('personal_early_access', 'Early Access', 15, 0, 10000, 2000, -1, 150, 'sonnet', true, true, 1, 120, 15, 120, '{}'::jsonb, true, 'personal', 15, 1, NULL, 0, false, 52428800, 5368709120) ON CONFLICT (id) DO NOTHING;
INSERT INTO public.plans (id, display_name, tier_order, monthly_price_cents, memory_limit, push_limit, recall_limit, ask_limit, ask_model, dreams_enabled, review_inbox, max_team_hubs, rate_limit_rpm, rate_limit_heavy_rpm, rate_limit_light_rpm, features, active, scope, entitlement_rank, max_owned_free_team_hubs, max_hub_members, seat_minimum, seat_billed, max_attachment_bytes, storage_bytes_limit) VALUES ('personal_pro', 'Pro', 20, 900, 5000, 1000, -1, 100, 'haiku', true, true, 0, 120, 15, 120, '{}'::jsonb, true, 'personal', 20, 3, NULL, 0, false, 52428800, 5368709120) ON CONFLICT (id) DO NOTHING;
INSERT INTO public.plans (id, display_name, tier_order, monthly_price_cents, memory_limit, push_limit, recall_limit, ask_limit, ask_model, dreams_enabled, review_inbox, max_team_hubs, rate_limit_rpm, rate_limit_heavy_rpm, rate_limit_light_rpm, features, active, scope, entitlement_rank, max_owned_free_team_hubs, max_hub_members, seat_minimum, seat_billed, max_attachment_bytes, storage_bytes_limit) VALUES ('pro', 'Pro', 20, 900, 5000, 1000, -1, 100, 'haiku', true, true, 0, 120, 15, 120, '{}'::jsonb, false, 'personal', 20, 3, NULL, 0, false, 52428800, 5368709120) ON CONFLICT (id) DO NOTHING;
INSERT INTO public.plans (id, display_name, tier_order, monthly_price_cents, memory_limit, push_limit, recall_limit, ask_limit, ask_model, dreams_enabled, review_inbox, max_team_hubs, rate_limit_rpm, rate_limit_heavy_rpm, rate_limit_light_rpm, features, active, scope, entitlement_rank, max_owned_free_team_hubs, max_hub_members, seat_minimum, seat_billed, max_attachment_bytes, storage_bytes_limit) VALUES ('personal_pro_plus', 'Pro+', 30, 1900, -1, -1, -1, 200, 'sonnet', true, true, 1, 180, 20, 180, '{}'::jsonb, true, 'personal', 30, 10, NULL, 0, false, 209715200, 53687091200) ON CONFLICT (id) DO NOTHING;
INSERT INTO public.plans (id, display_name, tier_order, monthly_price_cents, memory_limit, push_limit, recall_limit, ask_limit, ask_model, dreams_enabled, review_inbox, max_team_hubs, rate_limit_rpm, rate_limit_heavy_rpm, rate_limit_light_rpm, features, active, scope, entitlement_rank, max_owned_free_team_hubs, max_hub_members, seat_minimum, seat_billed, max_attachment_bytes, storage_bytes_limit) VALUES ('pro_plus', 'Pro+', 30, 1900, -1, -1, -1, 200, 'sonnet', true, true, 1, 180, 20, 180, '{}'::jsonb, false, 'personal', 30, 10, NULL, 0, false, 209715200, 53687091200) ON CONFLICT (id) DO NOTHING;
INSERT INTO public.plans (id, display_name, tier_order, monthly_price_cents, memory_limit, push_limit, recall_limit, ask_limit, ask_model, dreams_enabled, review_inbox, max_team_hubs, rate_limit_rpm, rate_limit_heavy_rpm, rate_limit_light_rpm, features, active, scope, entitlement_rank, max_owned_free_team_hubs, max_hub_members, seat_minimum, seat_billed, max_attachment_bytes, storage_bytes_limit) VALUES ('hub_team', 'Team', 40, 1500, -1, -1, -1, 200, 'sonnet', true, true, -1, 240, 30, 240, '{}'::jsonb, true, 'hub', 40, 0, NULL, 3, true, 524288000, 214748364800) ON CONFLICT (id) DO NOTHING;
INSERT INTO public.plans (id, display_name, tier_order, monthly_price_cents, memory_limit, push_limit, recall_limit, ask_limit, ask_model, dreams_enabled, review_inbox, max_team_hubs, rate_limit_rpm, rate_limit_heavy_rpm, rate_limit_light_rpm, features, active, scope, entitlement_rank, max_owned_free_team_hubs, max_hub_members, seat_minimum, seat_billed, max_attachment_bytes, storage_bytes_limit) VALUES ('team', 'Team', 40, 1500, -1, -1, -1, 200, 'sonnet', true, true, -1, 240, 30, 240, '{}'::jsonb, false, 'hub', 40, 0, NULL, 0, false, 524288000, 214748364800) ON CONFLICT (id) DO NOTHING;
INSERT INTO public.plans (id, display_name, tier_order, monthly_price_cents, memory_limit, push_limit, recall_limit, ask_limit, ask_model, dreams_enabled, review_inbox, max_team_hubs, rate_limit_rpm, rate_limit_heavy_rpm, rate_limit_light_rpm, features, active, scope, entitlement_rank, max_owned_free_team_hubs, max_hub_members, seat_minimum, seat_billed, max_attachment_bytes, storage_bytes_limit) VALUES ('enterprise', 'Enterprise', 50, 0, -1, -1, -1, -1, 'sonnet', true, true, -1, 600, 60, 600, '{}'::jsonb, false, 'hub', 50, 0, NULL, 0, false, 1073741824, -1) ON CONFLICT (id) DO NOTHING;
INSERT INTO public.plans (id, display_name, tier_order, monthly_price_cents, memory_limit, push_limit, recall_limit, ask_limit, ask_model, dreams_enabled, review_inbox, max_team_hubs, rate_limit_rpm, rate_limit_heavy_rpm, rate_limit_light_rpm, features, active, scope, entitlement_rank, max_owned_free_team_hubs, max_hub_members, seat_minimum, seat_billed, max_attachment_bytes, storage_bytes_limit) VALUES ('hub_enterprise', 'Enterprise', 50, 0, -1, -1, -1, -1, 'sonnet', true, true, -1, 600, 60, 600, '{}'::jsonb, true, 'hub', 50, 0, NULL, 0, true, 1073741824, -1) ON CONFLICT (id) DO NOTHING;


--
-- Seed data: email_brand_settings singleton row. The app assumes a
-- single row with id='singleton' exists (enforced by the CHECK
-- constraint).
--

INSERT INTO public.email_brand_settings (id) VALUES ('singleton') ON CONFLICT (id) DO NOTHING;
