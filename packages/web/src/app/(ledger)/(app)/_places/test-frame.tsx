import type { ReactNode } from "react";
import { render } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { LedgerProvider } from "@memaxlabs/ledger";
import { LocaleProvider } from "@/i18n";
import { DEMO_OVERVIEWS, DEMO_SPACES } from "@/lib/v2/data/demo-dataset";
import { KeymapProvider } from "@/lib/v2/keymap/react";
import { ToastProvider, ToastViewport } from "../_components/toasts";
import { LedgerDataProvider } from "../_lib/data";
import { OverlayProvider } from "../_lib/overlays";
import { SpaceViewContext } from "../_lib/space-context";

/**
 * The app frame's providers around one place, for DOM tests: the demo
 * source (tests swap it through vi.mock of demo-source), the keymap,
 * toasts and memax-v2's overview. Test-only; no app code imports it.
 */
export const TEST_SPACE = DEMO_SPACES.find((s) => s.slug === "memax-v2")!;

export function renderPlace(children: ReactNode): void {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
  render(
    <QueryClientProvider client={client}>
      <LocaleProvider>
        <LedgerProvider locale="en">
          <LedgerDataProvider mode="demo">
            <KeymapProvider>
              <ToastProvider>
                <OverlayProvider>
                  <SpaceViewContext
                    value={{
                      space: TEST_SPACE,
                      overview: DEMO_OVERVIEWS["memax-v2"],
                      overviewFailed: false,
                      retryOverview: () => {},
                    }}
                  >
                    {children}
                  </SpaceViewContext>
                  <ToastViewport />
                </OverlayProvider>
              </ToastProvider>
            </KeymapProvider>
          </LedgerDataProvider>
        </LedgerProvider>
      </LocaleProvider>
    </QueryClientProvider>,
  );
}
