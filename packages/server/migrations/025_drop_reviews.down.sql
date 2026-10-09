-- Revert 025: drop_reviews
--
-- Recreates the empty V1 `reviews` table exactly as 001_baseline_v1
-- defined it (columns, primary key, indexes and foreign keys). Rows
-- dropped by the up migration are not restored.
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

ALTER TABLE ONLY public.reviews
    ADD CONSTRAINT reviews_pkey PRIMARY KEY (id);

CREATE INDEX idx_reviews_hub_pending ON public.reviews USING btree (hub_id) WHERE (status = 'pending'::text);

CREATE UNIQUE INDEX idx_reviews_hub_pending_key ON public.reviews USING btree (hub_id, review_key) WHERE ((status = 'pending'::text) AND (review_key IS NOT NULL));

CREATE INDEX idx_reviews_hub_status ON public.reviews USING btree (hub_id, status);

ALTER TABLE ONLY public.reviews
    ADD CONSTRAINT fk_reviews_hub FOREIGN KEY (hub_id) REFERENCES public.hubs(id) ON DELETE CASCADE;

ALTER TABLE ONLY public.reviews
    ADD CONSTRAINT reviews_dream_run_id_fkey FOREIGN KEY (dream_run_id) REFERENCES public.dream_runs(id) ON DELETE SET NULL;
