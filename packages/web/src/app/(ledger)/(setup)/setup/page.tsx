import { redirect } from "next/navigation";

// /setup starts at FirstRun, keeping ?space= and ?import=.
export default async function SetupPage({
  searchParams,
}: {
  searchParams: Promise<Record<string, string | string[] | undefined>>;
}) {
  const params = new URLSearchParams();
  for (const [key, value] of Object.entries(await searchParams)) {
    if ((key === "space" || key === "import") && typeof value === "string") {
      params.set(key, value);
    }
  }
  const query = params.toString();
  redirect(`/setup/import${query ? `?${query}` : ""}`);
}
