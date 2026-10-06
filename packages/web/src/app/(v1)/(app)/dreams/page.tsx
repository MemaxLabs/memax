import { redirect } from "next/navigation";

/** Deprecated route — dreams live inline, redirect to home. */
export default function DreamsPage() {
  redirect("/home");
}
