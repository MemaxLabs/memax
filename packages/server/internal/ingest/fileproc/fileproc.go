package fileproc

import (
	"bytes"
	"context"
	"encoding/base64"
	"fmt"
	"log/slog"
	"strings"

	"github.com/MemaxLabs/memax/packages/server/internal/anthropic"
	gopdf "github.com/ledongthuc/pdf"
)

const maxBase64Size = 10 * 1024 * 1024

type Processor struct {
	client *anthropic.Client
	model  string
}

func New(client *anthropic.Client) *Processor {
	if client == nil {
		return nil
	}
	return &Processor{
		client: client,
		model:  anthropic.ModelFromEnv("FILEPROC_MODEL"),
	}
}

func (p *Processor) ExtractText(base64Content string, contentType string) (string, error) {
	return p.ExtractTextContext(context.Background(), base64Content, contentType)
}

func (p *Processor) ExtractTextContext(ctx context.Context, base64Content string, contentType string) (string, error) {
	if len(base64Content) > maxBase64Size {
		return "", fmt.Errorf("file too large: %d bytes exceeds %d byte limit", len(base64Content), maxBase64Size)
	}

	switch contentType {
	case "pdf":
		return p.extractPDF(ctx, base64Content)
	case "image":
		return p.extractImage(ctx, base64Content)
	default:
		return "", fmt.Errorf("unsupported file type for extraction: %s", contentType)
	}
}

func (p *Processor) extractPDF(ctx context.Context, base64Content string) (string, error) {
	pdfBytes, err := base64.StdEncoding.DecodeString(base64Content)
	if err != nil {
		return "", fmt.Errorf("decode base64: %w", err)
	}

	text, err := extractPDFTextNative(pdfBytes)
	if err == nil && len(strings.TrimSpace(text)) > 50 {
		slog.Info("PDF text extracted natively", "length", len(text))
		return text, nil
	}

	if err != nil {
		slog.Info("native PDF extraction failed, falling back to Claude", "error", err)
	} else {
		slog.Info("native PDF extraction returned too little text, falling back to Claude", "length", len(strings.TrimSpace(text)))
	}

	messages := []map[string]any{{
		"role": "user",
		"content": []map[string]any{
			{
				"type": "document",
				"source": map[string]string{
					"type":       "base64",
					"media_type": "application/pdf",
					"data":       base64Content,
				},
			},
			{
				"type": "text",
				"text": "Extract ALL text content from this PDF. Preserve headings, lists, and structure using markdown formatting. Output the text only, no commentary.",
			},
		},
	}}

	resp, err := p.client.CompleteMessages(ctx, p.model, 4096, messages, "ingest.file_pdf_extract")
	if err != nil {
		return "", err
	}
	slog.Info("file text extracted", "length", len(resp.Text))
	return resp.Text, nil
}

func extractPDFTextNative(pdfBytes []byte) (string, error) {
	reader := bytes.NewReader(pdfBytes)
	r, err := gopdf.NewReader(reader, int64(len(pdfBytes)))
	if err != nil {
		return "", fmt.Errorf("open PDF: %w", err)
	}

	var sb strings.Builder
	for i := 1; i <= r.NumPage(); i++ {
		page := r.Page(i)
		if page.V.IsNull() {
			continue
		}
		text, err := page.GetPlainText(nil)
		if err != nil {
			continue
		}
		if sb.Len() > 0 {
			sb.WriteString("\n\n---\n\n")
		}
		sb.WriteString(strings.TrimSpace(text))
	}

	result := strings.ReplaceAll(sb.String(), "\x00", "")
	return result, nil
}

func (p *Processor) extractImage(ctx context.Context, base64Content string) (string, error) {
	mediaType := detectImageMediaType(base64Content)

	messages := []map[string]any{{
		"role": "user",
		"content": []map[string]any{
			{
				"type": "image",
				"source": map[string]string{
					"type":       "base64",
					"media_type": mediaType,
					"data":       base64Content,
				},
			},
			{
				"type": "text",
				"text": "Extract ALL text visible in this image. This is a screenshot or document image. Preserve structure using markdown. Output the text only, no commentary.",
			},
		},
	}}

	resp, err := p.client.CompleteMessages(ctx, p.model, 4096, messages, "ingest.file_image_extract")
	if err != nil {
		return "", err
	}
	slog.Info("file text extracted", "length", len(resp.Text))
	return resp.Text, nil
}

func detectImageMediaType(base64Content string) string {
	if strings.HasPrefix(base64Content, "/9j/") {
		return "image/jpeg"
	}
	if strings.HasPrefix(base64Content, "iVBOR") {
		return "image/png"
	}
	if strings.HasPrefix(base64Content, "R0lGOD") {
		return "image/gif"
	}
	if strings.HasPrefix(base64Content, "UklGR") {
		return "image/webp"
	}
	return "image/png"
}
