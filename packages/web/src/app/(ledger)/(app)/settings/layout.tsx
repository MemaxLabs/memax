import { SettingsFrame } from "./settings-frame";

// /settings/…: account-level settings beside a settings nav (Account.png),
// inside the app frame so the rail stays on the last space visited.
export default function SettingsLayout({
  children,
}: {
  children: React.ReactNode;
}) {
  return <SettingsFrame>{children}</SettingsFrame>;
}
