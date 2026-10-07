import { OnboardingFrame } from "../../_onboarding/frame";
import { onboardingMode } from "../../_onboarding/mode";

// The setup screens (plan §6.3: /setup/{agents,import,cleanup,done}):
// Connect, FirstRun, Cleanup and CompileDone, outside any space's frame.
export default async function SetupLayout({
  children,
}: {
  children: React.ReactNode;
}) {
  return (
    <OnboardingFrame mode={await onboardingMode()}>{children}</OnboardingFrame>
  );
}
