import { redirect } from "next/navigation";

export default function AdminNotificationsRedirectPage() {
  redirect("/admin/communications/campaigns");
}
