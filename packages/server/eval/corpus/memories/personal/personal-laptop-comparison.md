Comparing options for a new development laptop. Current machine is a 2022 M2 MacBook Air with 16GB RAM which is starting to feel cramped running Docker, local Postgres, the Go server, and the Next.js dev server simultaneously.

## Option 1: M4 MacBook Pro 14" (2025)

- **Config:** M4 Pro (12-core CPU, 18-core GPU), 36GB unified memory, 512GB SSD
- **Price:** $2,399
- **Weight:** 3.4 lbs
- **Battery:** ~17 hours (Apple claim), realistically 10-12 hours for dev work
- **Display:** 14.2" Liquid Retina XDR, 120Hz ProMotion
- **Ports:** 3x Thunderbolt 4, HDMI, SD slot, MagSafe

**Pros:** Apple Silicon is unmatched for battery life and single-core perf. Go compiles are 2x faster than Intel. Docker Desktop runs natively. Continuity with my current Apple ecosystem (AirDrop, iMessage, etc.).

**Cons:** 512GB SSD is tight — would need the $2,599 1TB config. No upgradeable RAM. macOS can be annoying with Docker networking (host.docker.internal issues). $200 more for 1TB brings total to $2,599.

## Option 2: Lenovo ThinkPad X1 Carbon Gen 12

- **Config:** Intel Core Ultra 7 155H, 32GB LPDDR5x, 1TB SSD
- **Price:** $1,649 (Lenovo corporate discount)
- **Weight:** 2.48 lbs
- **Battery:** ~10 hours (spec), realistically 6-7 hours for dev work
- **Display:** 14" 2.8K OLED, 120Hz
- **Ports:** 2x Thunderbolt 4, 2x USB-A, HDMI

**Pros:** $750-950 cheaper. Better keyboard (ThinkPad keyboard is legendary). Linux support is first-class. The OLED display is gorgeous. Lighter by almost a pound. Docker networking is simpler on Linux.

**Cons:** Battery life is significantly worse. Intel thermals throttle under sustained loads (noticed in reviews). Go compilation is ~40% slower than M4 Pro. No MagSafe equivalent. Would need to switch to Linux daily driver.

## Current Leaning

Leaning toward the **M4 MacBook Pro 14" with 1TB** ($2,599). The battery life difference is the deciding factor — I work from coffee shops 2-3 days/week and hate carrying a charger. The compile speed advantage for Go is also meaningful since I'm building the Memax server daily. The $950 premium over the ThinkPad pays for itself in convenience.

Will wait for the WWDC 2026 announcements in case an M5 refresh is coming.
