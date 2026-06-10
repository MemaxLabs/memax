## Onboarding Funnel Analysis -- March 2026

Data from PostHog, covering 412 signups between March 1-31, 2026.

### Funnel Steps and Conversion

| Step | Users | Conversion | Drop-off |
|---|---|---|---|
| 1. Landing page visit | 3,840 | -- | -- |
| 2. Signup started | 892 | 23.2% | 76.8% |
| 3. Signup completed | 412 | 46.2% | 53.8% |
| 4. First memory pushed | 186 | 45.1% | 54.9% |
| 5. Second session (D1 return) | 98 | 52.7% | 47.3% |
| 6. 5+ memories pushed | 61 | 62.2% | 37.8% |
| 7. Upgraded to Pro | 14 | 23.0% | 77.0% |

### Key Drop-off Points

1. **Signup started -> completed (53.8% drop):** OAuth flow confusion. Users click "Sign up with GitHub" but bounce when they see the permissions screen. The scope request includes `read:user` and `user:email` which is standard, but the GitHub UI makes it look scary.
2. **Signup completed -> first push (54.9% drop):** Users land on an empty hub with no guidance. The "push your first memory" CTA is below the fold on most screens. No in-product tutorial.
3. **First push -> D1 return (47.3% drop):** Users push one memory but don't experience recall value. They need to push 3+ memories before recall becomes useful, but they leave before getting there.

### Recommendations

- Add an interactive onboarding checklist (push, recall, install CLI) -- target: reduce step 4 drop-off by 15pp
- Seed new accounts with 3 example memories showing what good context looks like
- Add a "try asking me something" prompt after first push to demonstrate recall immediately
- Simplify GitHub OAuth scope description with a custom interstitial explaining what we access

### Prepared by Sarah Kim, April 3, 2026
