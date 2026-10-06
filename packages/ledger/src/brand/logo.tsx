import { cx } from "../lib/cx";
import { MarkPaths } from "./mark";

/* The lockup is drawn, not typeset: "Memax" in Gloock, outlined, beside the mark.
   Geometry: the mark's ink is 1.04× the cap height and centred on it (so it overshoots the
   cap line and the baseline equally, like a round letter); the gap from the mark to the M's
   serif is 0.3× the cap height; tracking −0.015em. The viewBox is tight to the ink. */
const LOCKUP = {
  w: 4646.4,
  h: 780.0,
  mark: "translate(-82.82 -192.04) scale(9.1015)",
  text: "M1661.4 779 1381.4 137 1334.4 663Q1330.4 703 1357.4 728Q1384.4 753 1423.4 755V765H1223.4V755Q1261.4 753 1287.4 727Q1313.4 701 1317.4 662L1367.4 109Q1348.4 70 1319.9 47.5Q1291.4 25 1246.4 25V15H1486.4L1730.4 580L1974.4 15H2161.4V25Q2129.4 25 2107.4 44Q2085.4 63 2087.4 96L2138.4 688Q2143.4 722 2170.9 739Q2198.4 756 2225.4 756V765H1915.4V756Q1932.4 756 1950.4 748Q1968.4 740 1979.9 725Q1991.4 710 1988.4 690L1939.4 138ZM2430.4 777Q2361.4 777 2307.9 743Q2254.4 709 2224.4 648.5Q2194.4 588 2194.4 510Q2194.4 430 2227.4 370Q2260.4 310 2317.9 276.5Q2375.4 243 2448.4 243Q2509.4 243 2556.9 267Q2604.4 291 2632.9 334.5Q2661.4 378 2665.4 435L2666.4 450H2330.4V455Q2330.4 561 2377.4 624.5Q2424.4 688 2503.4 688Q2551.4 688 2594.9 664.5Q2638.4 641 2665.4 601L2674.4 604Q2657.4 655 2621.4 694Q2585.4 733 2536.4 755Q2487.4 777 2430.4 777ZM2331.4 435H2534.4Q2534.4 356 2508.9 307Q2483.4 258 2437.4 258Q2393.4 258 2364.4 306Q2335.4 354 2331.4 435ZM2664.4 765V755H2665.4Q2701.4 755 2723.4 733Q2745.4 711 2745.4 675V347Q2745.4 309 2729.9 288Q2714.4 267 2664.4 267V257H2763.4Q2804.4 257 2826.9 253Q2849.4 249 2860.4 242.5Q2871.4 236 2876.4 229H2886.4L2887.4 359Q2913.4 307 2966.4 275Q3019.4 243 3078.4 243Q3135.4 243 3173.4 275Q3211.4 307 3222.4 361Q3250.4 307 3302.9 275Q3355.4 243 3416.4 243Q3483.4 243 3523.9 287Q3564.4 331 3564.4 403V675Q3564.4 711 3586.4 733Q3608.4 755 3644.4 755H3645.4V765H3340.4V755H3341.4Q3377.4 755 3399.4 733Q3421.4 711 3421.4 675V393Q3421.4 343 3401.4 313Q3381.4 283 3349.4 283Q3318.4 283 3284.4 310.5Q3250.4 338 3225.4 382Q3226.4 384 3226.4 387Q3226.4 390 3226.4 392V675Q3226.4 711 3248.4 733Q3270.4 755 3306.4 755H3307.4V765H3002.4V755H3003.4Q3039.4 755 3061.4 733Q3083.4 711 3083.4 675V393Q3083.4 343 3063.9 313Q3044.4 283 3012.4 283Q2981.4 283 2947.9 309.5Q2914.4 336 2889.4 381V675Q2889.4 711 2911.4 733Q2933.4 755 2969.4 755H2970.4V765ZM3768.4 779Q3707.4 779 3670.4 746Q3633.4 713 3633.4 659Q3633.4 636 3641.4 615.5Q3649.4 595 3671.4 575Q3694.4 555 3734.4 533.5Q3774.4 512 3840.4 488L3933.4 454V379Q3933.4 322 3915.4 288.5Q3897.4 255 3865.4 255Q3827.4 256 3800.9 305Q3774.4 354 3765.4 441L3657.4 379Q3674.4 338 3709.4 307Q3744.4 276 3789.9 259Q3835.4 242 3885.4 242Q3970.4 242 4023.9 289.5Q4077.4 337 4077.4 419V660Q4077.4 718 4116.4 718Q4139.4 718 4161.4 693L4169.4 700Q4143.4 735 4106.4 757Q4069.4 779 4034.4 779Q3996.4 779 3968.9 753Q3941.4 727 3934.4 684Q3885.4 734 3848.9 756.5Q3812.4 779 3768.4 779ZM3843.4 717Q3865.4 717 3885.9 706Q3906.4 695 3932.4 670L3933.4 467L3893.4 482Q3842.4 501 3815.4 520.5Q3788.4 540 3777.9 564Q3767.4 588 3767.4 619Q3767.4 662 3788.9 689.5Q3810.4 717 3843.4 717ZM4133.4 765V755Q4151.4 752 4178.4 735Q4202.4 718 4235.4 672L4330.4 541Q4314.4 515 4299.4 489Q4284.4 463 4268.4 438Q4252.4 412 4235.9 385Q4219.4 358 4202.4 329Q4181.4 296 4167.4 282.5Q4153.4 269 4133.4 267V257H4393.4V267Q4361.4 269 4358.9 295Q4356.4 321 4375.4 353Q4390.4 379 4404.4 403Q4418.4 427 4432.4 450L4499.4 356Q4524.4 321 4520.9 295Q4517.4 269 4487.4 267V257H4632.4V267Q4604.4 270 4578.9 287Q4553.4 304 4520.4 350L4439.4 462Q4474.4 522 4509.4 579.5Q4544.4 637 4585.4 704Q4602.4 731 4614.9 742Q4627.4 753 4646.4 755V765H4386.4V755Q4417.4 753 4422.4 729Q4427.4 705 4405.4 668Q4388.4 637 4371.4 608.5Q4354.4 580 4337.4 552L4249.4 676Q4226.4 708 4234.9 730.5Q4243.4 753 4278.4 755V765Z",
};

export interface LogoProps {
  variant?: "mark" | "lockup";
  /** The mark's box for `mark`; the lockup's ink height for `lockup` (18 onboarding, 20 nav, 36–64 hero). */
  size?: number;
  className?: string;
  /** Accessible name. The product name is never translated. */
  label?: string;
  /** Hide from assistive technology when the name is already written next to it. */
  decorative?: boolean;
}

/** The Memax mark alone, or locked up with the name in Gloock. Draws in currentColor. */
export function Logo({
  variant = "lockup",
  size = 24,
  className,
  label = "Memax",
  decorative = false,
}: LogoProps) {
  const a11y = decorative
    ? { "aria-hidden": true as const }
    : { role: "img" as const, "aria-label": label };
  if (variant === "mark") {
    return (
      <span className={cx("mx-logo", className)}>
        <svg
          className="mx-logo-mark"
          width={size}
          height={size}
          viewBox="0 0 128 128"
          fill="currentColor"
          stroke="currentColor"
          strokeLinecap="round"
          strokeLinejoin="round"
          {...a11y}
        >
          <MarkPaths />
        </svg>
      </span>
    );
  }
  const width = Math.round(((size * LOCKUP.w) / LOCKUP.h) * 10) / 10;
  return (
    <span className={cx("mx-logo", "is-lockup", className)}>
      <svg
        className="mx-logo-lockup"
        width={width}
        height={size}
        viewBox={`0 0 ${LOCKUP.w} ${LOCKUP.h}`}
        fill="currentColor"
        {...a11y}
      >
        <path d={LOCKUP.text} />
        <g
          transform={LOCKUP.mark}
          stroke="currentColor"
          strokeLinecap="round"
          strokeLinejoin="round"
        >
          <MarkPaths />
        </g>
      </svg>
    </span>
  );
}
