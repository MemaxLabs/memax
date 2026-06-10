## Stakeholder Meeting: Enterprise Pitch -- April 7, 2026

**Attendees:** Ziyang (CEO), Sarah Kim (PM), Rachel Torres (VP Eng at NovaTech), Tom Bradley (CTO at NovaTech)
**Location:** Zoom, 45 minutes

### Context

NovaTech is a Series C fintech company (180 engineers, $40M ARR). They reached out after seeing Jiahao's demo at the AI Eng meetup in SF. They currently use a homegrown Confluence-to-LLM pipeline that breaks constantly.

### Key Requirements from NovaTech

1. **SSO/SAML integration** -- mandatory for procurement. They use Okta.
2. **Data residency** -- must be US-only. They are SOC 2 Type II certified and cannot send data to EU regions.
3. **Audit logging** -- every memory access must be logged with user, timestamp, and context. Compliance requires 2-year retention.
4. **Team size** -- would start with a 30-seat pilot in their platform team, expand to 180 if successful.
5. **Budget** -- $15-20/seat/mo is within their tooling budget. $25+ requires VP approval.

### Our Gaps

- SSO/SAML: not built yet. Estimated 3-4 weeks of eng work. Auth0 integration could accelerate.
- Data residency: Fly.io supports region pinning, but we haven't tested single-region deployment.
- Audit logging: we log memory access but retention is currently 90 days, not 2 years.

### Next Steps

- Sarah to send NovaTech a capabilities deck by April 14
- Ziyang to scope SSO/SAML eng work and provide timeline
- Follow-up meeting scheduled for April 21 with NovaTech's security team
- If pilot closes, estimated deal value: $6,480/mo ($19 x 30 seats + custom support)

### Notes

Rachel mentioned they also evaluated Mem0 but rejected it because it has no team model. Tom asked specifically about on-premise -- we said not on the roadmap but could discuss dedicated tenancy. They seemed satisfied with that.
