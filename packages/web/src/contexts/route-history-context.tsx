"use client";

import { createContext, useContext } from "react";

const RouteHistoryContext = createContext<string | null>(null);

export function RouteHistoryProvider({
  previousPathname,
  children,
}: {
  previousPathname: string | null;
  children: React.ReactNode;
}) {
  return (
    <RouteHistoryContext.Provider value={previousPathname}>
      {children}
    </RouteHistoryContext.Provider>
  );
}

export function usePreviousPathname() {
  return useContext(RouteHistoryContext);
}
