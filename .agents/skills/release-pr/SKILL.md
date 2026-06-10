---
name: release-pr
description: "Use when the user asks to 'release to prod', 'cut a release', 'promote main to prod', 'open a release PR', 'ship what's on main', or any variation of promoting the `main` branch's commits into the `prod` deploy branch. This skill computes the next semver, drafts a release-styled PR title + body, and opens the PR with a `release:*` label applied at creation time — matching the repo's existing release conventions (see prod PRs #3, #13, #14). Always trigger this skill when the user mentions releasing, promoting, or shipping code to the prod branch; do not hand-craft the PR yourself."
user-invocable: true
---

# Release PR Skill

Open a promote-main-to-prod PR following this monorepo's release conventions — release-style title (`Release vX.Y.Z — <summary>`), body with Summary + Commits + Test plan, and the right `release:*` label applied at creation time so the `deploy-production.yml` resolve-version step picks it up cleanly.

## Why This Exists

Prod deploys are triggered by merging a PR with a `release:*` label from `main` to `prod`. The deploy workflow reads PR labels to compute the version bump, and any stumble here — wrong label, no label, cosmetic title, missed commits — either skips the tag or produces a confusing release note.

The manual path has footguns:

- Forgetting to apply the `release:*` label (or applying it after creation, which is easy to miss)
- Picking too-small a bump when the batch actually contained a `release:minor` PR
- Writing a PR title that reads like a dev commit ("fix XYZ") instead of a release entry
- Opening a duplicate PR when one is already open against prod
- Opening an empty PR when main is already equal to prod

This skill encodes the workflow so every release PR looks consistent and ships under the correct bump.

## Prerequisites

- `gh` CLI authenticated with push/PR permissions on the repo
- Working directory is somewhere inside the memax monorepo
- Some commits exist on `origin/main` that are not on `origin/prod`; otherwise there's nothing to release

## Workflow

### Step 1: Sync with origin

Before anything, fetch so local refs match remote:

```bash
git fetch origin
```

You'll be comparing `origin/main` against `origin/prod`; not your local copies.

### Step 2: Check for shippable commits

```bash
git log --oneline origin/prod..origin/main
```

If this is empty, stop and tell the user: "No commits on main that aren't already on prod — nothing to release." Do not open an empty PR.

### Step 3: Check for an existing open main → prod PR

```bash
gh pr list --base prod --head main --state open --json number,url,title --jq '.[0]'
```

If anything comes back, don't open a duplicate. Tell the user the existing PR's number + URL + title and let them decide whether to close it or continue adding commits to the same branch.

### Step 4: Determine the release level

The deploy workflow's bump rule is **"max label wins"** — any merged PR carrying `release:major` → major; else any `release:minor` → minor; else patch. The skill should preview the same logic so the PR title's version matches what the deploy will actually tag.

For each commit in the range `origin/prod..origin/main`, find its source PR (if any) and check that PR's labels:

```bash
# Get the commits
commits=$(git log origin/prod..origin/main --format='%H %s')

# For each commit subject, extract a trailing "(#NN)" if present
# (squash-merged PRs embed their number in the subject). Where that
# fails, fall back to `gh pr list --search "<sha>"` to find the PR.
#
# Then for each discovered PR number:
gh pr view <N> --json labels --jq '.labels[].name' | grep '^release:'
```

Also scan commit messages themselves for a `release:major|minor|patch` token — sometimes the signal is in the commit body (especially for direct pushes or hotfixes), not a PR label.

Max across everything found:

- any `release:major` → `major`
- else any `release:minor` → `minor`
- else any `release:patch` → `patch`
- else → default to `patch`, **and flag it in the PR body** so the reviewer knows the batch was missing an explicit label

### Step 5: Compute the next version

Read the latest semver tag on the repo:

```bash
git describe --tags --abbrev=0 --match 'v[0-9]*.[0-9]*.[0-9]*'
```

Parse as `vMAJOR.MINOR.PATCH` and apply the bump:

- `major` → `v(MAJOR+1).0.0`
- `minor` → `vMAJOR.(MINOR+1).0`
- `patch` → `vMAJOR.MINOR.(PATCH+1)`

If the describe pattern fails (no prior tag, or tag in an unexpected format), tell the user what was found and ask them to resolve before proceeding — the deploy workflow will fail the same way so it's better to flag now.

### Step 6: Draft the PR

**Title format** — `Release vX.Y.Z — <short summary>`

- When the range has a single commit, use that commit's subject (with the conventional prefix stripped) as the summary.
- When the range spans a batch, synthesize a 2–5 word theme covering the dominant change.
- Keep titles under ~70 chars.

Good examples (real prod history):

- `Release v0.1.0 — first production deploy`
- `Release v0.1.9 — agent-first tagline`
- `admin ops: per-job logs panel + dreams/ingest logging enrichment`
- `admin: four UX improvements + slug hardening`

**Body template:**

```markdown
## Summary

<one paragraph summarizing the theme of the batch. For single-commit releases, expand on what changed and why.>

## Commits

- `<sha>` <subject>
- `<sha>` <subject>

## Test plan

- [ ] <concrete thing to verify post-deploy, e.g. "landing callout renders new copy", "api.memax.app/favicon.ico returns 200", "prod deploy tags vX.Y.Z">
- [ ] ...

release:<level>

🤖 Generated with [Claude Code](https://claude.com/claude-code)
```

The bottom-line `release:<level>` is the critical trailer — the repo's `validate-release-label.yml` check reads this.

If the level was defaulted to `patch` because no PR in the batch carried a `release:*` label, add a small note above the `release:patch` trailer: `> No PR in this batch carried an explicit release label; defaulting to patch.` This makes the situation visible to whoever merges.

### Step 7: Open the PR with the label

Apply the label **at creation time** via `--label`. Applying it after creation is easy to forget and leaves the PR briefly unlabeled, which could race the validate-release-label.yml check.

```bash
gh pr create \
  --base prod \
  --head main \
  --title "Release vX.Y.Z — <summary>" \
  --label "release:<level>" \
  --body "$(cat <<'EOF'
<body from step 6>
EOF
)"
```

Use a heredoc for the body so markdown formatting and multi-line content survive shell quoting unchanged.

### Step 8: Verify + report

Confirm the label actually landed (defense in depth against a silent `gh` failure):

```bash
gh pr view <N> --json labels --jq '.labels[].name'
```

You should see `release:<level>` in the output. If it's missing, apply it now with `gh pr edit <N> --add-label "release:<level>"` and tell the user.

Then print the PR URL to the user, along with the version it will tag once merged.

## Edge cases

**Nothing to ship** — `origin/prod..origin/main` is empty. Tell the user and stop. This happens when the user already merged a prior release PR and hasn't pushed new work yet, or when they're on the wrong remote.

**Existing open main → prod PR** — link to it (URL + number + title). Don't open a duplicate. If the user wants a fresh PR, they need to close the old one first.

**No release:\* label anywhere in the batch** — default to `patch` and add the flag-line in the body (step 6). The validate-release-label check will still pass because the PR itself carries `release:patch`.

**Weird or missing tag** — if `git describe` can't find a vX.Y.Z tag, don't guess. Report what's on the tag list and let the user fix it. The deploy workflow will fail with the same complaint.

**Branch protection requires a reviewer** — creation will still succeed but the PR can't merge until a reviewer approves. Report the PR URL and let the user handle approval.

## What NOT to do

- Don't hand-craft a release PR with `gh pr create` yourself without running the label + version logic — this is exactly the workflow the skill exists to automate.
- Don't apply the label in a second `gh pr edit` call after creation when you could have used `--label` on `pr create`. The deploy workflow's label-validation step runs on PR events and a briefly-unlabeled PR can cause a spurious failure.
- Don't bump the version without checking the existing tag list — recreating an existing tag later is painful (see the v0.7.0 rewrite drama in git history).
- Don't include a `release:*` label in both the title AND as a raw trailer — one or the other, pick the trailer form (`release:patch` on its own line at the bottom).

## Example

The user says: "put out a release PR". You:

1. `git fetch origin`
2. `git log --oneline origin/prod..origin/main` → shows one commit `993277fd docs+agents: reframe tagline...`
3. `gh pr list --base prod --head main --state open` → empty, nothing open
4. No prior PR found for that commit (or found one with no `release:*` label) → default to `patch`
5. `git describe --tags --abbrev=0 --match 'v[0-9]*.[0-9]*.[0-9]*'` → `v0.1.8`
6. Next version: `v0.1.9`
7. Draft title: `Release v0.1.9 — agent-first tagline` (derived from the single commit's theme)
8. Draft body per the template, including a Test plan
9. `gh pr create --base prod --head main --title "Release v0.1.9 — agent-first tagline" --label "release:patch" --body "$(cat <<'EOF'...EOF)"`
10. `gh pr view <N> --json labels --jq '.labels[].name'` → confirms `release:patch`
11. Report: "Opened PR #15: https://... — will tag v0.1.9 once merged."
