---
name: codex-review
description: "Run a Codex CLI code review on recent changes. Use this skill whenever the user asks for a 'codex review', 'second opinion', 'get codex to review', or wants an independent code review of their changes. Also trigger when the user says 'let codex check this', 'review with codex', or any variation of asking for an automated review pass. This skill handles the full lifecycle: briefing Codex, running the review, monitoring, parsing findings, and iterating on fixes."
---

# Codex Review Skill

Run an independent code review using the Codex CLI (`codex exec`). Codex reads the codebase, runs tests, and produces structured findings with severity levels and file references.

## Why This Exists

Code review by a second model catches bugs, edge cases, and design issues that the primary agent may have blind spots on. Codex runs autonomously in the background, reads any file it needs, and can run tests — making it a thorough reviewer. The non-interactive `exec` mode is perfect for review because Codex reads, analyzes, and reports without needing human interaction.

## Prerequisites

- `codex` CLI must be installed and authenticated (OpenAI API key configured)
- The repo must be a git repository with a working directory

## Workflow

### Step 1: Gather Context

Before running Codex, build a clear review brief. The quality of the review depends on the quality of the prompt.

Gather:

- **What changed**: `git diff --stat` for the scope, key file paths
- **Why it changed**: The intent behind the changes (bug fix, new feature, refactor)
- **What to focus on**: Specific concerns (correctness, edge cases, security, performance)
- **What to ignore**: Known issues, unrelated failures, pre-existing lint warnings

Structure the prompt as a clear review request:

```
Review the recent [description of changes]. Focus on:

1. [file path] — [what this file does and what changed]
2. [file path] — [what this file does and what changed]
...

Context: [why these changes were made, what problem they solve]

Check for: [specific concerns — correctness, edge cases, race conditions, etc.]
```

The more specific the prompt, the more useful the findings. Avoid vague "review everything" requests.

Save the prompt to a file, such as `/tmp/codex-review/prompt.txt`. Multi-line prompts are easy to mangle with shell quoting, especially when they include code blocks, quotes, `$VARS`, or backticks.

### Step 2: Run Codex

Launch Codex in the background using `exec` mode (non-interactive, autonomous):

```bash
mkdir -p /tmp/codex-review
cat > /tmp/codex-review/prompt.txt <<'EOF'
<review prompt>
EOF

codex exec \
  --dangerously-bypass-approvals-and-sandbox \
  --model gpt-5.5 \
  - \
  < /tmp/codex-review/prompt.txt \
  2>&1
```

Run this with `run_in_background: true` so the main session continues while Codex works. Codex may take 2-5 minutes for a thorough review.

The output starts with metadata including the **session ID** — save this for resuming later:

```
OpenAI Codex v0.121.0 (research preview)
--------
session id: 019d9a9e-cb26-79f2-9b77-1e93876a7e6a
--------
```

### Step 3: Monitor for Completion

Use the Monitor tool to watch for findings in the output. Codex's review text contains keywords like "Finding", "Medium:", "High:", "Low:", "Verification", "passed", "FAIL".

```bash
tail -f <output_file> 2>&1 | grep -E --line-buffered \
  "session id:|Finding|Medium:|High:|Low:|No blocking|passed|FAIL|Verification|codex$"
```

The monitor will notify when Codex produces findings or completes. Do NOT poll or sleep — the monitor handles notification.

### Step 4: Read and Parse Findings

When Codex completes, read the full output file. Codex structures its review as:

```
Findings

1. High: [description]
   [file:line] — [details]

2. Medium: [description]
   [file:line] — [details]

Verification
- [what tests were run]
- [what passed/failed]
```

Extract:

- **Session ID** (from the metadata header — needed for resume)
- **Findings** (numbered, with severity)
- **Verification results** (what Codex tested)

Present findings to the user in a clear table format.

### Step 5: Handle Incomplete Reviews

Codex may get cut off if the review is complex (context limit, timeout). Signs:

- Exit code 1 with no findings section
- Output ends mid-analysis (reading files but no conclusions)

To resume, use the saved session ID:

```bash
cat > /tmp/codex-review/resume_prompt.txt <<'EOF'
<resume prompt with context>
EOF

codex exec resume \
  --dangerously-bypass-approvals-and-sandbox \
  --model gpt-5.5 \
  <SESSION_ID> \
  - \
  < /tmp/codex-review/resume_prompt.txt \
  2>&1
```

The resume prompt should remind Codex what it was doing and ask it to produce findings:

```
You were reviewing [description] and got cut off. You had already:
1. [what Codex read]
2. [what tests it ran]

Please produce your review findings now. Format: numbered findings
with severity (High/Medium/Low), file, line, and description.
```

### Step 6: Fix and Re-review

After addressing findings, resume the same Codex session with an update:

```bash
cat > /tmp/codex-review/rereview_prompt.txt <<'EOF'
<update about fixes and request for re-review>
EOF

codex exec resume \
  --dangerously-bypass-approvals-and-sandbox \
  --model gpt-5.5 \
  <SESSION_ID> \
  - \
  < /tmp/codex-review/rereview_prompt.txt \
  2>&1
```

Brief Codex on what was fixed and ask for another pass. Include:

- Which findings were addressed and how
- Any findings that were intentionally deferred with rationale
- Request for re-review of the specific changes

### Step 7: Iterate Until Clean

Repeat steps 4-6 until Codex reports no blocking findings. A clean review looks like:

```
No blocking findings in this pass.
[description of what was verified]
Verification: [tests run and passed]
```

## Key Patterns

### Model Selection

Default to `gpt-5.5` for thorough reviews. The model flag is configurable:

```bash
codex exec --model gpt-5.5    # thorough, recommended
codex exec --model gpt-5.4          # alternative
```

### Background Execution

Always run Codex in the background. Reviews take 2-5 minutes. Use the main session productively while waiting — work on other tasks, prepare for potential fixes, or brief the user.

### Session Persistence

The session ID is the key to the iterative review loop. Always save it from the first run's output. The `resume` subcommand carries full conversation context, so Codex remembers what it already reviewed.

### Output Location

Background commands write to a temp file. The path is returned when the command starts:

```
Output is being written to: /tmp/claude-1000/.../tasks/<id>.output
```

Use this path to read results after completion.

## Common Issues

- **"go: cannot find main module"**: Codex tried to run Go commands from the repo root instead of the Go module directory. The resume prompt should mention the correct working directory.
- **Codex reads many files but produces no findings**: It likely hit context limits. Resume with a focused prompt asking for conclusions only.
- **MCP/auth errors in output**: These are Codex's internal tool errors, not review findings. Ignore lines like `ERROR rmcp::transport::worker`.

## Example

A typical review cycle:

1. User: "Let's get Codex to review the metadata validation changes"
2. Agent gathers context from git diff, builds review prompt
3. Agent runs `codex exec` in background, monitors
4. Codex produces 3 findings (1 High, 2 Medium)
5. Agent presents findings, fixes the High and both Mediums
6. Agent resumes session with fix summary, asks for re-review
7. Codex reports "No blocking findings"
8. Agent reports clean review to user
