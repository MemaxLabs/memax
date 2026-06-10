-- Revert 004: bootstrap_system_user_and_tutorial_hub

DELETE FROM public.hub_members
  WHERE hub_id = '00000000-0000-0000-0000-00000000babe'::uuid
    AND user_id = '00000000-0000-0000-0000-00000000ada7'::uuid;

DELETE FROM public.hubs
  WHERE id = '00000000-0000-0000-0000-00000000babe'::uuid;

DELETE FROM public.users
  WHERE id = '00000000-0000-0000-0000-00000000ada7'::uuid;
