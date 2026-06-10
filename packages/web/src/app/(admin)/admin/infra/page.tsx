import { redirect } from "next/navigation";

// /admin/infra → /admin/infra/config. Keeps existing bookmarks
// working and gives the layout a canonical landing tab.
export default function AdminInfraIndex(): never {
  redirect("/admin/infra/config");
}
