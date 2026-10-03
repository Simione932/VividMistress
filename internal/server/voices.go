package server

import (
	"fmt"
	"strings"
)

// VoiceOption is one selectable Edge TTS voice. The ID (e.g.
// "en-GB-SoniaNeural") is the internal name used by the TTS engine; Name and
// Detail are the user-friendly names shown instead.
type VoiceOption struct {
	ID     string // internal name, e.g. en-GB-SoniaNeural
	Name   string // friendly name, e.g. Sonia
	Detail string // short description, e.g. British, Clear/Expressive
	Female bool
}

// voiceCatalog is the full list of selectable voices. Ordering matches the
// product list: female voices first, then male voices. `voice` numbers follow
// this order so `voice <n>` selects by a continuous number.
var voiceCatalog = []VoiceOption{
	// --- Female voices ---
	{ID: "en-GB-SoniaNeural", Name: "Sonia", Detail: "British, Clear/Expressive", Female: true},
	{ID: "en-GB-LibbyNeural", Name: "Libby", Detail: "British, Soft/Storytelling", Female: true},
	{ID: "en-GB-MaisieNeural", Name: "Maisie", Detail: "British, Bright/Upbeat", Female: true},
	{ID: "en-US-AriaNeural", Name: "Aria", Detail: "American, Natural/Conversational", Female: true},
	{ID: "en-US-JennyNeural", Name: "Jenny", Detail: "American, Professional/Balanced", Female: true},
	{ID: "en-US-AnaNeural", Name: "Ana", Detail: "American, Young/Soft", Female: true},
	{ID: "en-CA-ClaraNeural", Name: "Clara", Detail: "Canadian, Neutral/Smooth", Female: true},
	{ID: "en-AU-NatashaNeural", Name: "Natasha", Detail: "Australian, Regional Accent", Female: true},
	{ID: "ja-JP-NanamiNeural", Name: "Nanami", Detail: "Japanese, English Accent/Multilingual", Female: true},
	{ID: "en-IN-NeerjaNeural", Name: "Neerja", Detail: "Indian, Expressive", Female: true},
	{ID: "en-IN-AashiNeural", Name: "Aashi", Detail: "Indian, Natural", Female: true},
	{ID: "en-SG-LunaNeural", Name: "Luna", Detail: "Singaporean, Regional Accent", Female: true},
	{ID: "en-HK-YanNeural", Name: "Yan", Detail: "Hong Kong, Regional Accent", Female: true},
	{ID: "en-PH-RosaNeural", Name: "Rosa", Detail: "Philippine, Regional Accent", Female: true},
	{ID: "en-NG-EzinneNeural", Name: "Ezinne", Detail: "Nigerian, Expressive", Female: true},
	{ID: "en-ZA-LeahNeural", Name: "Leah", Detail: "South African, Natural", Female: true},
	{ID: "en-KE-AsiliaNeural", Name: "Asilia", Detail: "Kenyan, Expressive", Female: true},
	{ID: "en-IE-EmilyNeural", Name: "Emily", Detail: "Irish, Natural", Female: true},
	{ID: "en-GB-MiaNeural", Name: "Mia", Detail: "Scottish, Regional Accent", Female: true},

	{ID: "en-US-ChristopherNeural", Name: "Christopher", Detail: "American, Deep/Authoritative", Female: false},
	{ID: "en-US-GuyNeural", Name: "Guy", Detail: "American, Casual/Friendly", Female: false},
	{ID: "en-US-EricNeural", Name: "Eric", Detail: "American, Measured/Calm", Female: false},
	{ID: "en-GB-RyanNeural", Name: "Ryan", Detail: "British, Rich/Expressive", Female: false},
	{ID: "en-GB-ThomasNeural", Name: "Thomas", Detail: "British, Formal/Classic", Female: false},
	{ID: "en-CA-LiamNeural", Name: "Liam", Detail: "Canadian, Clear/Natural", Female: false},
	{ID: "en-AU-WilliamNeural", Name: "William", Detail: "Australian, Regional Accent", Female: false},
	{ID: "ja-JP-KeitaNeural", Name: "Keita", Detail: "Japanese, English Accent/Multilingual", Female: false},
	{ID: "en-IN-PrabhatNeural", Name: "Prabhat", Detail: "Indian, Expressive", Female: false},
	{ID: "en-IN-KunalNeural", Name: "Kunal", Detail: "Indian, Natural", Female: false},
	{ID: "en-SG-WayneNeural", Name: "Wayne", Detail: "Singaporean, Regional Accent", Female: false},
	{ID: "en-HK-SamNeural", Name: "Sam", Detail: "Hong Kong, Regional Accent", Female: false},
	{ID: "en-PH-JamesNeural", Name: "James", Detail: "Philippine, Regional Accent", Female: false},
	{ID: "en-NG-AbeoNeural", Name: "Abeo", Detail: "Nigerian, Expressive", Female: false},
	{ID: "en-ZA-LukeNeural", Name: "Luke", Detail: "South African, Natural", Female: false},
	{ID: "en-KE-ChilembaNeural", Name: "Chilemba", Detail: "Kenyan, Expressive", Female: false},
	{ID: "en-IE-ConnorNeural", Name: "Connor", Detail: "Irish, Natural", Female: false},
	{ID: "en-NZ-MitchellNeural", Name: "Mitchell", Detail: "New Zealand, Regional Accent", Female: false},
}

// voiceOptionByNumber returns the catalog entry at 1-based position n.
func voiceOptionByNumber(n int) (VoiceOption, bool) {
	if n < 1 || n > len(voiceCatalog) {
		return VoiceOption{}, false
	}
	return voiceCatalog[n-1], true
}

// voiceOptionByID looks up a voice by its internal id (case-insensitive).
func voiceOptionByID(id string) (VoiceOption, bool) {
	for _, v := range voiceCatalog {
		if strings.EqualFold(v.ID, strings.TrimSpace(id)) {
			return v, true
		}
	}
	return VoiceOption{}, false
}

// voiceFriendlyName returns the external (friendly) name for an internal voice
// id, e.g. "Sonia" for "en-GB-SoniaNeural". Falls back to the id itself when
// the voice is not in the catalog.
func voiceFriendlyName(id string) string {
	if v, ok := voiceOptionByID(id); ok {
		return v.Name
	}
	return strings.TrimSpace(id)
}

// voiceFriendlyLabel returns "Name — Detail" for an internal voice id.
func voiceFriendlyLabel(id string) string {
	if v, ok := voiceOptionByID(id); ok {
		return v.Name + " — " + v.Detail
	}
	return strings.TrimSpace(id)
}

// formatVoiceList builds the numbered voice list shown by the `voice` command.
// Numbers are continuous across both groups so `voice <n>` is unambiguous.
func formatVoiceList() string {
	var b strings.Builder
	b.WriteString("Female Voices:\n")
	first := true
	for i, v := range voiceCatalog {
		if v.Female {
			if !first {
				b.WriteString("\n")
			}
			first = false
			fmt.Fprintf(&b, "%d) %s — %s", i+1, v.Name, v.Detail)
		}
	}
	b.WriteString("\n\nMale Voices:\n")
	first = true
	for i, v := range voiceCatalog {
		if !v.Female {
			if !first {
				b.WriteString("\n")
			}
			first = false
			fmt.Fprintf(&b, "%d) %s — %s", i+1, v.Name, v.Detail)
		}
	}
	b.WriteString("\n\nChoose with:  voice [n]")
	return b.String()
}
