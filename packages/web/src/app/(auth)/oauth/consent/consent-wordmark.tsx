export function ConsentWordmark() {
  return (
    <div
      className="h-8 w-40"
      style={{
        backgroundColor: "var(--foreground)",
        opacity: 0.85,
        maskImage: "url(/images/memax-wordmark.svg)",
        maskSize: "contain",
        maskRepeat: "no-repeat",
        maskPosition: "center",
        WebkitMaskImage: "url(/images/memax-wordmark.svg)",
        WebkitMaskSize: "contain",
        WebkitMaskRepeat: "no-repeat",
        WebkitMaskPosition: "center",
      }}
    />
  );
}
