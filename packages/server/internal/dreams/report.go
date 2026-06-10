package dreams

import (
	"fmt"
	"strings"

	"github.com/MemaxLabs/memax/packages/server/internal/model"
)

func (e *Engine) generateReport(run *model.DreamRun, actions []model.DreamAction) string {
	warnings := dreamReportWarnings(run)

	if len(actions) == 0 {
		if len(warnings) > 0 {
			return fmt.Sprintf("Scanned %d memories. %s", run.MemoriesScanned, strings.Join(warnings, " "))
		}
		return fmt.Sprintf("Scanned %d memories. No consolidation needed — your memory is clean.", run.MemoriesScanned)
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("## Dream Report\n\nScanned **%d memories**.\n\n", run.MemoriesScanned))

	if len(warnings) > 0 {
		sb.WriteString("### Warnings\n")
		for _, warning := range warnings {
			sb.WriteString(fmt.Sprintf("- %s\n", warning))
		}
		sb.WriteString("\n")
	}

	if run.DuplicatesMerged > 0 {
		sb.WriteString(fmt.Sprintf("### Merged %d duplicate(s)\n", run.DuplicatesMerged))
		for _, a := range actions {
			if a.ActionType == "merge" {
				sb.WriteString(fmt.Sprintf("- %s\n", a.Reason))
			}
		}
		sb.WriteString("\n")
	}

	if run.ContradictionsFound > 0 {
		sb.WriteString(fmt.Sprintf("### Found %d contradiction(s)\n", run.ContradictionsFound))
		for _, a := range actions {
			if a.ActionType == "contradiction" {
				sb.WriteString(fmt.Sprintf("- %s\n", a.Reason))
			}
		}
		sb.WriteString("\n")
	}

	if run.MemoriesArchived > 0 {
		sb.WriteString(fmt.Sprintf("### Archived %d stale memory(ies)\n", run.MemoriesArchived))
		for _, a := range actions {
			if a.ActionType == "archive" {
				sb.WriteString(fmt.Sprintf("- %s\n", a.Reason))
			}
		}
		sb.WriteString("\n")
	}

	if run.MemoriesOrganized > 0 {
		sb.WriteString(fmt.Sprintf("### Organized %d memory(ies) into topics\n", run.MemoriesOrganized))
		for _, a := range actions {
			if a.ActionType == "organize" {
				sb.WriteString(fmt.Sprintf("- %s\n", a.Reason))
			}
		}
		sb.WriteString("\n")
	}

	if run.TopicsRestructured > 0 {
		sb.WriteString(fmt.Sprintf("### Restructured %d topic(s) into hierarchy\n", run.TopicsRestructured))
		for _, a := range actions {
			if a.ActionType == "restructure" {
				sb.WriteString(fmt.Sprintf("- %s\n", a.Reason))
			}
		}
	}

	return sb.String()
}

// callLLM sends a prompt to Claude using the default model (Haiku). Used for merge/contradiction.
