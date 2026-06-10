package email

import (
	"bytes"
	"embed"
	"fmt"
	"html/template"
	"strings"
	texttemplate "text/template"
)

//go:embed templates/*.html templates/*.txt
var templateFS embed.FS

// Templates holds parsed HTML and text templates for all email types.
type Templates struct {
	html *template.Template
	text *texttemplate.Template
}

// LoadTemplates parses all embedded email templates.
// Call once at startup — templates are immutable after load.
func LoadTemplates() (*Templates, error) {
	html, err := template.New("").ParseFS(templateFS, "templates/*.html")
	if err != nil {
		return nil, fmt.Errorf("parse HTML templates: %w", err)
	}

	text, err := texttemplate.New("").ParseFS(templateFS, "templates/*.txt")
	if err != nil {
		return nil, fmt.Errorf("parse text templates: %w", err)
	}

	return &Templates{html: html, text: text}, nil
}

// RenderResult holds the rendered output of a template.
type RenderResult struct {
	Subject string
	HTML    string
	Text    string
}

// Render executes a named template with the given data.
// Looks up "{name}.html" and "{name}.txt" from the embedded templates.
// Subject is extracted from the first line of the text template (line starting with "Subject: ").
func (t *Templates) Render(name string, data any) (RenderResult, error) {
	var result RenderResult

	// Render HTML
	var htmlBuf bytes.Buffer
	htmlTmpl := t.html.Lookup(name + ".html")
	if htmlTmpl == nil {
		return result, fmt.Errorf("HTML template %q not found", name)
	}
	if err := htmlTmpl.Execute(&htmlBuf, data); err != nil {
		return result, fmt.Errorf("render HTML template %q: %w", name, err)
	}
	result.HTML = htmlBuf.String()

	// Render text
	var textBuf bytes.Buffer
	textTmpl := t.text.Lookup(name + ".txt")
	if textTmpl == nil {
		return result, fmt.Errorf("text template %q not found", name)
	}
	if err := textTmpl.Execute(&textBuf, data); err != nil {
		return result, fmt.Errorf("render text template %q: %w", name, err)
	}

	// Extract subject from first line ("Subject: ...")
	textContent := textBuf.String()
	lines := strings.SplitN(textContent, "\n", 2)
	if len(lines) >= 1 && strings.HasPrefix(lines[0], "Subject: ") {
		result.Subject = strings.TrimPrefix(lines[0], "Subject: ")
		if len(lines) >= 2 {
			result.Text = strings.TrimLeft(lines[1], "\n")
		}
	} else {
		result.Text = textContent
	}

	return result, nil
}
