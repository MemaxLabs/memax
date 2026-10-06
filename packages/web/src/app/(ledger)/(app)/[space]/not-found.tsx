"use client";

// notFound() inside a space (a record that isn't there, Decisions in a
// space that isn't a team's): the States2 "Not found" state, inside the
// frame so the rail stays.

import { PlaceNotFound } from "../_components/status";
import { useSpaceView } from "../_lib/space-context";

export default function SpaceNotFoundBoundary() {
  const { space } = useSpaceView();
  return <PlaceNotFound space={space} />;
}
