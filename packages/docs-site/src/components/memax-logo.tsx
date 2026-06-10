import * as React from "react";

interface MemaxLogoProps extends React.SVGProps<SVGSVGElement> {
  size?: number;
}

/**
 * Memax icon mark — the stylized mirrored M shape.
 *
 * Inlined here (rather than imported from `@memaxlabs/ui`) so this
 * Apache-2.0 package does not link the AGPL-3.0 `@memaxlabs/ui` package.
 * Keep visually in sync with `packages/ui/src/memax-logo.tsx` if the brand
 * mark changes.
 */
export function MemaxLogo({ size = 24, className, ...props }: MemaxLogoProps) {
  return (
    <svg
      width={size}
      height={size}
      viewBox="0 0 128 128"
      xmlns="http://www.w3.org/2000/svg"
      fill="currentColor"
      stroke="currentColor"
      strokeLinecap="round"
      strokeLinejoin="round"
      className={className}
      {...props}
    >
      <path
        d="M693.827 1493.738s-16.84 2.831-16.516 20.493c1.067 16.532 15.335 19.167 15.335 19.167s1.425 21.119 27.246 21.269c0 0-7.763-5.005-10.598-14.042-2.699-1.779-4.919-4.636-5.618-9.79l.04-6.555a1.047 1.047 0 0 0-1.045-1.053l-5.271-.007s-8.544-.066-8.948-9.434c.065-8.179 8.036-9.562 8.036-9.562l6.533.07a.93.93 0 0 0 .939-.94l-.075-6.782s2.8-12.189 14.791-11.958c14.484.209 15.19 15.232 15.19 15.232l.134 14.264s-.862 4.118 5.047 4.261c6.154.043 5.34-4.22 5.34-4.22l-.005-17.452S741.535 1474.114 718 1474c-20.435.336-24.173 19.738-24.173 19.738"
        strokeWidth=".97"
        transform="translate(-691.755 -1503.419)scale(1.03547)"
      />
      <path
        d="M693.827 1493.738s-16.84 2.831-16.516 20.493c1.067 16.532 15.335 19.167 15.335 19.167s1.425 21.119 27.246 21.269c0 0-7.763-5.005-10.598-14.042-2.699-1.779-4.919-4.636-5.618-9.79l.04-6.555a1.047 1.047 0 0 0-1.045-1.053l-5.271-.007s-8.544-.066-8.948-9.434c.065-8.179 8.036-9.562 8.036-9.562l6.533.07a.93.93 0 0 0 .939-.94l-.075-6.782s2.8-12.189 14.791-11.958c14.484.209 15.19 15.232 15.19 15.232l.134 14.264s-.862 4.118 5.047 4.261c6.154.043 5.34-4.22 5.34-4.22l-.005-17.452S741.535 1474.114 718 1474c-20.435.336-24.173 19.738-24.173 19.738"
        strokeWidth=".97"
        transform="translate(819.755 1631.418)scale(-1.03547)"
      />
    </svg>
  );
}
