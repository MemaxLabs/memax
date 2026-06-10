package eval

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
)

// PrintMarkdownReport writes a human-readable eval report to stdout.
func PrintMarkdownReport(report EvalReport) {
	fmt.Print(FormatMarkdownReport(report))
}

// FormatMarkdownReport returns the markdown report as a string.
func FormatMarkdownReport(report EvalReport) string {
	var b strings.Builder

	// Header
	b.WriteString("# Retrieval Eval Report\n\n")
	fmt.Fprintf(&b, "**Date:** %s\n", report.Timestamp)
	fmt.Fprintf(&b, "**Corpus:** %d memories, %d queries\n\n", report.MemoryCount, report.QueryCount)

	// Aggregate Metrics
	b.WriteString("## Aggregate Metrics\n\n")
	b.WriteString("| Metric            | Value | Target | Status |\n")
	b.WriteString("|-------------------|-------|--------|--------|\n")

	// Targets match the enforcement thresholds in eval_test.go.
	// Calibrated for the current corpus (180 memories, 82 graded
	// queries). See eval_test.go for the 2026-05-19 drift-band-shift
	// rationale.
	writeMetricRow(&b, "nDCG@5", report.Aggregate.NDCG5, 0.72)
	writeMetricRow(&b, "nDCG@10", report.Aggregate.NDCG10, 0.72)
	writeMetricRow(&b, "Precision@5", report.Aggregate.Precision5, 0.20)
	writeMetricRow(&b, "StrongPrecision@3", report.Aggregate.StrongPrecision3, 0.25)
	writeMetricRow(&b, "Recall@20", report.Aggregate.Recall20, 0.60)
	writeMetricRow(&b, "MRR@10", report.Aggregate.MRR10, 0.72)
	writeHarmfulRow(&b, report.Aggregate.Harmful10)
	writeLatencyRow(&b, report.Aggregate.LatencyMs)
	b.WriteString("\n")

	// Per-Slice Metrics
	if len(report.Slices) > 0 {
		b.WriteString("## Per-Slice Metrics\n\n")
		b.WriteString("| Slice              | nDCG@5 | P@5  | MRR@10 | Count |\n")
		b.WriteString("|--------------------|--------|------|--------|-------|\n")
		for _, s := range report.Slices {
			fmt.Fprintf(&b, "| %-18s | %-6s | %-4s | %-6s | %-5d |\n",
				s.Slice,
				formatFloat(s.Metrics.NDCG5),
				formatFloat(s.Metrics.Precision5),
				formatFloat(s.Metrics.MRR10),
				s.Count,
			)
		}
		b.WriteString("\n")
	}

	// Per-Query Results
	if len(report.Queries) > 0 {
		b.WriteString("## Per-Query Results\n\n")
		b.WriteString("| ID | Query | nDCG@5 | MRR@10 | Harmful | Latency | Status |\n")
		b.WriteString("|----|-------|--------|--------|---------|---------|--------|\n")
		for _, q := range report.Queries {
			fmt.Fprintf(&b, "| %s | %s | %s | %s | %d | %dms | %s |\n",
				q.QueryID,
				truncate(q.Query, 40),
				formatFloat(q.Metrics.NDCG5),
				formatFloat(q.Metrics.MRR10),
				q.Metrics.Harmful10,
				q.Metrics.LatencyMs,
				passIcon(q.Pass),
			)
		}
		b.WriteString("\n")
	}

	return b.String()
}

// WriteJSONReport writes the report as formatted JSON to a file.
func WriteJSONReport(report EvalReport, path string) error {
	data, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal eval report: %w", err)
	}
	data = append(data, '\n')
	if err := os.WriteFile(path, data, 0644); err != nil {
		return fmt.Errorf("write eval report to %s: %w", path, err)
	}
	return nil
}

func writeMetricRow(b *strings.Builder, name string, value float64, target float64) {
	pass := value >= target
	fmt.Fprintf(b, "| %-17s | %-5s | %-6s | %-6s |\n",
		name,
		formatFloat(value),
		formatFloat(target),
		passIcon(pass),
	)
}

func writeHarmfulRow(b *strings.Builder, harmful int) {
	pass := harmful == 0
	fmt.Fprintf(b, "| %-17s | %-5d | %-6d | %-6s |\n",
		"Harmful@10",
		harmful,
		0,
		passIcon(pass),
	)
}

func writeLatencyRow(b *strings.Builder, latencyMs int64) {
	fmt.Fprintf(b, "| %-17s | %dms |        |        |\n",
		"Avg Latency",
		latencyMs,
	)
}

func passIcon(pass bool) string {
	if pass {
		return "PASS"
	}
	return "FAIL"
}

func formatFloat(v float64) string {
	return fmt.Sprintf("%.2f", v)
}

func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	if maxLen <= 3 {
		return s[:maxLen]
	}
	return s[:maxLen-3] + "..."
}
