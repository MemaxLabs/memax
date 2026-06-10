import * as React from "react";

interface MemaxLogoProps extends React.SVGProps<SVGSVGElement> {
  size?: number;
}

/** Memax icon mark — the stylized mirrored M shape. */
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

interface MemaxWordmarkProps extends React.SVGProps<SVGSVGElement> {
  height?: number;
  /**
   * Render only the stylized "memax" text without the icon. The viewBox
   * tightens to the text region (`130 0 330 99`) so the icon paths fall
   * outside the viewport and aren't visible. Same path data is shared with
   * the full wordmark — single source of truth — only the viewBox crop and
   * the rendered aspect ratio differ.
   *
   * Useful where the icon is supplied separately (e.g., shell-v2 LeftRail
   * keeps a stand-alone MemaxLogo + a text-only wordmark next to it).
   *
   * Aspect ratio when textOnly: ~3.4:1 (vs ~4.6:1 for the full wordmark).
   */
  textOnly?: boolean;
}

/** Memax icon + stylized "memax" text. Aspect ratio ~4.5:1 (full) or ~3.4:1 (text-only). */
export function MemaxWordmark({
  height = 24,
  className,
  textOnly = false,
  ...props
}: MemaxWordmarkProps) {
  // Text starts at viewBox x ≈ 130 (icon occupies 0–130). Cropping the
  // viewBox skips the icon entirely; the icon path elements still render
  // but fall outside the viewport so they're invisible.
  const fullViewBox = "0 0 460 99";
  const textViewBox = "130 0 330 99";
  const aspectRatio = textOnly ? 330 / 99 : 460 / 99;
  const width = Math.round(height * aspectRatio);
  return (
    <svg
      width={width}
      height={height}
      viewBox={textOnly ? textViewBox : fullViewBox}
      xmlns="http://www.w3.org/2000/svg"
      fill="currentColor"
      stroke="currentColor"
      strokeLinecap="round"
      strokeLinejoin="round"
      className={className}
      {...props}
    >
      {/* Icon */}
      <path
        d="M693.827 1493.738s-16.84 2.831-16.516 20.493c1.067 16.532 15.335 19.167 15.335 19.167s1.425 21.119 27.246 21.269c0 0-7.763-5.005-10.598-14.042-2.699-1.779-4.919-4.636-5.618-9.79l.04-6.518a1.084 1.084 0 0 0-1.083-1.09l-5.233-.007s-8.544-.066-8.948-9.434c.065-8.179 8.036-9.562 8.036-9.562l6.499.07a.966.966 0 0 0 .973-.973l-.075-6.749s2.8-12.189 14.791-11.958c14.484.209 15.19 15.232 15.19 15.232l.134 14.264s-.862 4.118 5.047 4.261c6.154.043 5.34-4.22 5.34-4.22l-.005-17.452S741.535 1474.114 718 1474c-20.435.336-24.173 19.738-24.173 19.738"
        strokeWidth="1"
        transform="translate(-669 -1464.112)"
      />
      <path
        d="M693.827 1493.738s-16.84 2.831-16.516 20.493c1.067 16.532 15.335 19.167 15.335 19.167s1.425 21.119 27.246 21.269c0 0-7.763-5.005-10.598-14.042-2.699-1.779-4.919-4.636-5.618-9.79l.04-6.518a1.084 1.084 0 0 0-1.083-1.09l-5.233-.007s-8.544-.066-8.948-9.434c.065-8.179 8.036-9.562 8.036-9.562l6.499.07a.966.966 0 0 0 .973-.973l-.075-6.749s2.8-12.189 14.791-11.958c14.484.209 15.19 15.232 15.19 15.232l.134 14.264s-.862 4.118 5.047 4.261c6.154.043 5.34-4.22 5.34-4.22l-.005-17.452S741.535 1474.114 718 1474c-20.435.336-24.173 19.738-24.173 19.738"
        strokeWidth="1"
        transform="matrix(-1 0 0 -1 790.729 1563.335)"
      />
      <g transform="translate(10 0)">
        {/* m */}
        <path
          d="M747.116 1506a16.574 16.574 0 0 0-16.576 16.576v23.765c0 1.236.491 2.421 1.364 3.295a4.66 4.66 0 0 0 3.295 1.364h1.19a4.505 4.505 0 0 0 4.505-4.505v-22.735a7.2 7.2 0 0 1 7.2-7.201h1.692a7.41 7.41 0 0 1 7.41 7.411v22.395a4.635 4.635 0 0 0 4.635 4.635h1.154a4.274 4.274 0 0 0 4.274-4.274v-22.72a7.42 7.42 0 0 1 7.421-7.422h1.625a7.34 7.34 0 0 1 7.337 7.338v22.458a4.624 4.624 0 0 0 4.62 4.62h1.014a4.723 4.723 0 0 0 4.724-4.724v-23.993A16.286 16.286 0 0 0 777.717 1506H776c-6.647.09-12.009 3.696-13.793 6.168-3.903-4.079-8.345-6.153-13.343-6.168z"
          strokeWidth=".9"
          transform="translate(-674.38 -1635.112)scale(1.10546)"
        />
        {/* e */}
        <path
          d="M832.144 1519.275c-2.116-2.153-4.985-3.477-8.144-3.477-6.499 0-11.775 5.604-11.775 12.507s5.276 12.507 11.775 12.507c2.917 0 5.588-1.129 7.646-2.998l2.137-2.134c1.365-1.546 3.043-1.37 4.916-.121l2.333 1.689c1.261.882 1.143 2.11.396 3.498-3.773 5.948-10.173 9.864-17.428 9.864-11.59 0-21-9.995-21-22.305S812.41 1506 824 1506c7.861 0 14.025 4.312 17.453 11.041.062.094.315.642.328.669q.514 1.09.933 2.254c.839 2.161-.112 4.647-2.158 5.589l-18.037 8.303c-1.407.647-3.05-.027-3.667-1.506l-1.384-3.322c-.579-1.388.024-3.008 1.345-3.617z"
          strokeWidth=".86"
          transform="matrix(1.18553 0 0 1.12825 -742.365 -1669.943)"
        />
        {/* m */}
        <path
          d="M747.116 1506a16.574 16.574 0 0 0-16.576 16.576v23.765c0 1.236.491 2.421 1.364 3.295a4.66 4.66 0 0 0 3.295 1.364h1.19a4.505 4.505 0 0 0 4.505-4.505v-22.735a7.2 7.2 0 0 1 7.2-7.201h1.692a7.41 7.41 0 0 1 7.41 7.411v22.395a4.635 4.635 0 0 0 4.635 4.635h1.154a4.274 4.274 0 0 0 4.274-4.274v-22.72a7.42 7.42 0 0 1 7.421-7.422h1.625a7.34 7.34 0 0 1 7.337 7.338v22.458a4.624 4.624 0 0 0 4.62 4.62h1.014a4.723 4.723 0 0 0 4.724-4.724v-23.993A16.286 16.286 0 0 0 777.717 1506H776c-6.647.09-12.009 3.696-13.793 6.168-3.903-4.079-8.345-6.153-13.343-6.168z"
          strokeWidth=".9"
          transform="translate(-542.83 -1635.543)scale(1.10546)"
        />
        {/* x */}
        <path
          d="m978.044 1509.929-.044 35.856c0 1.222-1.074 2.28-2.55 2.28-1.025.262-2.76-.83-5.205-2.357-.812-.457-1.196-1.508-1.196-2.731l-.049-35.09c0-1.223 1.199-2.215 2.675-2.215 0 0 .676-.041 1.755.408.66.326 4.592 2.133 4.614 3.849"
          strokeWidth=".75"
          transform="rotate(-36.142 -2506.902 2274.706)scale(1.19723 1.44565)"
        />
        <path
          d="m978.044 1509.929-.044 35.856c0 1.222-1.074 2.28-2.55 2.28-1.025.262-2.76-.83-5.205-2.357-.812-.457-1.196-1.508-1.196-2.731l-.049-35.09c0-1.223 1.199-2.215 2.675-2.215 0 0 .676-.041 1.755.408.66.326 4.592 2.133 4.614 3.849"
          strokeWidth=".75"
          transform="rotate(216.142 -724.534 872.207)scale(-1.19723 1.44565)"
        />
        {/* a */}
        <path
          d="M952.979 1545.461c-3.401 3.073-7.898 4.898-13.202 4.898-12.314 0-22.311-10.11-22.311-22.562s9.997-22.562 22.311-22.562c5.304 0 9.801 1.927 13.202 5.117v-.413c0-2.429 1.999-4.401 4.46-4.401h1.101c2.462 0 4.46 1.972 4.46 4.401v36.267c0 2.429-1.998 4.401-4.46 4.401h-1.101c-2.461 0-4.46-1.972-4.46-4.401zm-12.905-29.931c-6.863 0-12.435 5.499-12.435 12.271s5.572 12.271 12.435 12.271 12.435-5.498 12.435-12.271c0-6.772-5.572-12.271-12.435-12.271"
          strokeWidth=".91"
          transform="matrix(1.08631 0 0 1.10083 -655.622 -1627.647)"
        />
      </g>
    </svg>
  );
}

interface MemaxTextLogoProps extends React.SVGProps<SVGSVGElement> {
  height?: number;
}

/**
 * Stylized "memax" text alone, no icon. Thin wrapper around `MemaxWordmark`
 * with `textOnly` so paths stay single-sourced and brand updates flow to
 * both shapes through one file.
 *
 * Used by shell-v2's left rail (where the icon is rendered separately as a
 * collapse trigger and the text reads next to it). Aspect ratio ~3.4:1.
 */
export function MemaxTextLogo({ height = 16, ...props }: MemaxTextLogoProps) {
  return <MemaxWordmark height={height} textOnly {...props} />;
}
