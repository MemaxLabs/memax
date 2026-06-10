package classify

import "testing"

func TestDetectTrackURL(t *testing.T) {
	t.Parallel()
	cases := []string{
		"http://example.com/article",
		"https://github.com/user/repo",
		"  https://with-leading-whitespace.com  ",
	}
	for _, c := range cases {
		if got := DetectTrack(c, ""); got != TrackLink {
			t.Errorf("DetectTrack(%q) = %q, want link", c, got)
		}
	}
}

func TestDetectTrackExplicitChatContentType(t *testing.T) {
	t.Parallel()
	for _, ct := range []string{"chat", "CHAT", "Chat", "transcript"} {
		if got := DetectTrack("arbitrary", ct); got != TrackExtraction {
			t.Errorf("DetectTrack(..., %q) = %q, want extraction", ct, got)
		}
	}
}

func TestDetectTrackSpeakerTurns(t *testing.T) {
	t.Parallel()
	// >30% speaker-like lines in a 6-line block → extraction
	content := `Alice: hello there
Bob: hi
Alice: how are you
Bob: good
Alice: great
Bob: bye`
	if got := DetectTrack(content, ""); got != TrackExtraction {
		t.Errorf("heavy speaker pattern: got %q, want extraction", got)
	}
}

func TestDetectTrackBlockQuoteCountsAsSpeaker(t *testing.T) {
	t.Parallel()
	// `> ` block quotes count as speaker-like per the regex.
	content := `> yes
> certainly
> noted
> agreed
> sure
> ok
> confirmed`
	if got := DetectTrack(content, ""); got != TrackExtraction {
		t.Errorf("block-quote heavy: got %q, want extraction", got)
	}
}

func TestDetectTrackDocumentMarkdown(t *testing.T) {
	t.Parallel()
	content := `# Intro

Some prose here.

## Details

More prose.`
	if got := DetectTrack(content, ""); got != TrackDocument {
		t.Errorf("markdown document: got %q, want document", got)
	}
}

func TestDetectTrackDocumentCodeBlock(t *testing.T) {
	t.Parallel()
	content := "plain intro\n```go\nfunc main() {}\n```\ntrailing text"
	if got := DetectTrack(content, ""); got != TrackDocument {
		t.Errorf("code-block doc: got %q, want document", got)
	}
}

func TestDetectTrackDefaultsToDocument(t *testing.T) {
	t.Parallel()
	if got := DetectTrack("just some prose", ""); got != TrackDocument {
		t.Errorf("default plain text: got %q, want document", got)
	}
}

func TestDetectTrackEmptyContent(t *testing.T) {
	t.Parallel()
	if got := DetectTrack("", ""); got != TrackDocument {
		t.Errorf("empty: got %q, want document (safest default)", got)
	}
}

func TestDetectTrackShortContentNotMisclassifiedAsExtraction(t *testing.T) {
	t.Parallel()
	// Fewer than 4 lines → rule 3's percentage test doesn't fire.
	// 2 speaker lines in 2 total is 100%, but the total-lines > 3
	// guard keeps it on the document track.
	content := "Alice: short\nBob: chat"
	if got := DetectTrack(content, ""); got != TrackDocument {
		t.Errorf("short 2-line chat: got %q, want document (guard against overclassification)", got)
	}
}
