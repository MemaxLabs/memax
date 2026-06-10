import { loader } from "fumadocs-core/source";
import { docs } from "@/../.source/server";

export const source = loader({
  baseUrl: "/",
  // eslint-disable-next-line @typescript-eslint/no-explicit-any
  source: (docs as any).toFumadocsSource(),
});
