package voice

import "testing"

func TestStripMarkdownAddsPeriodAfterNarration(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		// Narration without ending punctuation gets a period added.
		{`*she nods* "hello"`, `she nods. "hello"`},
		{`*her knuckles white* "I can do it"`, `her knuckles white. "I can do it"`},
		{`**bold action** then words`, `bold action. then words`},
		// Narration that already ends with punctuation is left alone.
		{`*she nods.* "hello"`, `she nods. "hello"`},
		{`*she screams!* "no"`, `she screams! "no"`},
		{`*she asks?* "why"`, `she asks? "why"`},
		{`*she pauses,* "hi"`, `she pauses, "hi"`},
		// Text without markers is untouched.
		{"plain text here", "plain text here"},
	}

	for _, c := range cases {
		got := stripMarkdown(c.in)
		if got != c.want {
			t.Errorf("stripMarkdown(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}
