import { redirect } from "next/navigation";

/** Legacy route — redirect to home (dreams live inline). */
export default function DreamDetailRedirect() {
  redirect("/home");
}
