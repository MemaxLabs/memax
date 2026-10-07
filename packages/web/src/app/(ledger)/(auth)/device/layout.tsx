import { OnboardingFrame } from "../../_onboarding/frame";
import { onboardingMode } from "../../_onboarding/mode";

// /device (CliAuth): a signed-in person's page, outside any space. Signed
// out, it signs in first and comes back with the code.
export default async function DeviceLayout({
  children,
}: {
  children: React.ReactNode;
}) {
  return (
    <OnboardingFrame mode={await onboardingMode()}>{children}</OnboardingFrame>
  );
}
