package language

import (
	"strings"
	"unicode"

	"github.com/abadojack/whatlanggo"
)

const (
	UnknownCode    = "und"
	SimpleConfig   = "simple"
	minimumRunes   = 24
	codeRatioCut   = 0.35
	symbolRatioCut = 0.45
)

type Result struct {
	Code       string
	Config     string
	Confidence float64
	Reliable   bool
}

func Detect(text string) Result {
	sample := normalizeSample(text)
	if sample == "" || countLetters(sample) < minimumRunes || looksCodeHeavy(sample) {
		return Result{Code: UnknownCode, Config: SimpleConfig}
	}

	info := whatlanggo.Detect(sample)
	code := normalizeCode(info.Lang)
	config, ok := configByLang[info.Lang]
	if !ok {
		config = SimpleConfig
	}
	reliable := info.IsReliable() && ok
	if !reliable {
		config = SimpleConfig
	}
	if code == "" {
		code = UnknownCode
	}
	return Result{Code: code, Config: config, Confidence: info.Confidence, Reliable: reliable}
}

func SearchConfig(text string) string {
	return Detect(text).Config
}

func DetectChunk(headingChain, content, hint string) Result {
	return Detect(ChunkSample(headingChain, content, hint))
}

func ChunkSample(headingChain, content, hint string) string {
	parts := make([]string, 0, 3)
	if headingChain != "" {
		parts = append(parts, headingChain)
	}
	if hint != "" {
		parts = append(parts, hint)
	}
	if content != "" {
		parts = append(parts, content)
	}
	return strings.Join(parts, "\n")
}

func normalizeCode(lang whatlanggo.Lang) string {
	switch lang {
	case whatlanggo.Eng:
		return "en"
	case whatlanggo.Fra:
		return "fr"
	case whatlanggo.Deu:
		return "de"
	case whatlanggo.Spa:
		return "es"
	case whatlanggo.Por:
		return "pt"
	case whatlanggo.Ita:
		return "it"
	case whatlanggo.Nld:
		return "nl"
	case whatlanggo.Rus:
		return "ru"
	case whatlanggo.Swe:
		return "sv"
	case whatlanggo.Tur:
		return "tr"
	case whatlanggo.Fin:
		return "fi"
	case whatlanggo.Nob, whatlanggo.Nno:
		return "no"
	case whatlanggo.Ron:
		return "ro"
	case whatlanggo.Hun:
		return "hu"
	case whatlanggo.Dan:
		return "da"
	default:
		return whatlanggo.LangToStringShort(lang)
	}
}

func normalizeSample(text string) string {
	text = strings.TrimSpace(text)
	if text == "" {
		return ""
	}
	if len(text) > 4000 {
		text = text[:4000]
	}
	lines := strings.Split(text, "\n")
	kept := make([]string, 0, len(lines))
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "```") {
			continue
		}
		kept = append(kept, trimmed)
	}
	return strings.Join(kept, " ")
}

func countLetters(text string) int {
	count := 0
	for _, r := range text {
		if unicode.IsLetter(r) {
			count++
		}
	}
	return count
}

func looksCodeHeavy(text string) bool {
	if text == "" {
		return true
	}
	lower := strings.ToLower(text)
	codeKeywords := 0
	for _, token := range []string{"func ", "return ", "package ", "import ", "const ", "var ", "class ", "public ", "private ", "if ", "else ", "for ", "while "} {
		if strings.Contains(lower, token) {
			codeKeywords++
		}
	}
	if codeKeywords >= 2 {
		return true
	}
	total := 0
	codey := 0
	symbols := 0
	for _, r := range text {
		if unicode.IsSpace(r) {
			continue
		}
		total++
		if strings.ContainsRune("{}[]()<>=/_\\`~|@#$%^&*;", r) {
			codey++
			symbols++
			continue
		}
		if unicode.IsPunct(r) || unicode.IsSymbol(r) {
			symbols++
		}
	}
	if total == 0 {
		return true
	}
	return float64(codey)/float64(total) > codeRatioCut || float64(symbols)/float64(total) > symbolRatioCut
}

var configByLang = map[whatlanggo.Lang]string{
	whatlanggo.Dan: "danish",
	whatlanggo.Nld: "dutch",
	whatlanggo.Eng: "english",
	whatlanggo.Fin: "finnish",
	whatlanggo.Fra: "french",
	whatlanggo.Deu: "german",
	whatlanggo.Hun: "hungarian",
	whatlanggo.Ita: "italian",
	whatlanggo.Nob: "norwegian",
	whatlanggo.Nno: "norwegian",
	whatlanggo.Por: "portuguese",
	whatlanggo.Ron: "romanian",
	whatlanggo.Rus: "russian",
	whatlanggo.Spa: "spanish",
	whatlanggo.Swe: "swedish",
	whatlanggo.Tur: "turkish",
}
