// Package voice provides text-to-speech capabilities using Microsoft Edge's
// online TTS service.
package voice

import (
	"bytes"
	"context"
	"fmt"
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/difyz9/edge-tts-go/pkg/communicate"
)

// DefaultVoice is the Edge TTS voice used for TTS.
// Change this to pick a different voice in the future.
const DefaultVoice = "en-GB-SoniaNeural"

// ensureSentenceEnd appends a period to s when it ends with a letter or
// digit (i.e. it has no trailing punctuation). This is used after stripping
// markdown emphasis markers so narration like "*she nods*" becomes
// "she nods." and the TTS engine inserts a natural pause before the
// following dialogue.
func ensureSentenceEnd(s string) string {
	trimmed := strings.TrimRight(s, " \t\r\n")
	if trimmed == "" {
		return trimmed
	}
	r, _ := utf8.DecodeLastRuneInString(trimmed)
	if unicode.IsLetter(r) || unicode.IsDigit(r) {
		return trimmed + "."
	}
	return trimmed
}

// stripMarkdown removes common markdown formatting from text.
// It keeps the actual content (bold/italic/link text) and removes the syntax.
func stripMarkdown(text string) string {
	// Remove bold/italic markers: **text**, *text*, __text__, _text_
	// When the enclosed text has no trailing punctuation, a period is added
	// so the TTS engine pauses naturally after the narration.
	re := regexp.MustCompile(`(\*\*|__)(.*?)(\*\*|__)`)
	text = re.ReplaceAllStringFunc(text, func(match string) string {
		return ensureSentenceEnd(match[2 : len(match)-2])
	})

	re = regexp.MustCompile(`(\*|_)(.*?)(\*|_)`)
	text = re.ReplaceAllStringFunc(text, func(match string) string {
		return ensureSentenceEnd(match[1 : len(match)-1])
	})

	// Remove inline code: `text`
	re = regexp.MustCompile("`([^`]*)`")
	text = re.ReplaceAllString(text, "$1")

	// Remove code blocks: ```text```
	re = regexp.MustCompile("```[^`]*```")
	text = re.ReplaceAllString(text, "")

	// Remove headers: # text, ## text, etc.
	re = regexp.MustCompile(`(?m)^\s{0,3}#{1,6}\s+`)
	text = re.ReplaceAllString(text, "")

	// Remove links: [text](url)
	re = regexp.MustCompile(`\[([^\]]*)\]\([^)]*\)`)
	text = re.ReplaceAllString(text, "$1")

	// Remove images: ![alt](url)
	re = regexp.MustCompile(`!\[([^\]]*)\]\([^)]*\)`)
	text = re.ReplaceAllString(text, "$1")

	// Remove blockquotes: > text
	re = regexp.MustCompile(`(?m)^\s{0,3}>\s?`)
	text = re.ReplaceAllString(text, "")

	// Remove horizontal rules: ---, ***, ___
	re = regexp.MustCompile(`(?m)^\s{0,3}((-{3,})|(\*{3,})|(_{3,}))\s*$`)
	text = re.ReplaceAllString(text, "")

	// Remove list markers: - item, * item, + item, 1. item
	re = regexp.MustCompile(`(?m)^\s{0,3}([-*+]|\d+\.)\s+`)
	text = re.ReplaceAllString(text, "")

	// Remove strikethrough: ~~text~~
	re = regexp.MustCompile(`~~(.*?)~~`)
	text = re.ReplaceAllString(text, "$1")

	// Clean up any leftover markdown symbols and collapse multiple spaces
	text = strings.ReplaceAll(text, "**", "")
	text = strings.ReplaceAll(text, "__", "")
	text = strings.ReplaceAll(text, "*", "")
	text = strings.ReplaceAll(text, "_", "")
	text = strings.ReplaceAll(text, "`", "")
	text = strings.ReplaceAll(text, "~~", "")

	// Collapse multiple spaces and trim
	re = regexp.MustCompile(`\s+`)
	text = re.ReplaceAllString(text, " ")

	return strings.TrimSpace(text)
}

// stripPausePunctuation removes punctuation characters ONLY from text inside
// double quotes (dialogue). This prevents the TTS engine from inserting pauses
// mid-dialogue (e.g. "You're a mess, Sarah?" becomes "You're a mess Sarah")
// while leaving narration and non-quoted text untouched.
// Apostrophes are preserved so contractions like "You're" still work.
func stripPausePunctuation(text string) string {
	// Find all double-quoted sections and strip punctuation from only those
	re := regexp.MustCompile(`"([^"]*)"`)
	return re.ReplaceAllStringFunc(text, func(match string) string {
		// Extract the inner text (without the quotes)
		inner := match[1 : len(match)-1]

		// Remove pause-inducing punctuation from the dialogue text
		// (keep apostrophes for contractions)
		punctRe := regexp.MustCompile(`[.,!?;:()\[\]{}\-—–…]`)
		inner = punctRe.ReplaceAllString(inner, " ")

		// Collapse multiple spaces
		spaceRe := regexp.MustCompile(`\s+`)
		inner = spaceRe.ReplaceAllString(inner, " ")

		// Rebuild with the quotes preserved
		return `"` + strings.TrimSpace(inner) + `"`
	})
}

// GenerateVoiceBuffer converts text to MP3 bytes in memory without touching disk.
// It streams audio chunks directly into a bytes.Buffer in RAM.
// This is a universal, cross-platform method that works on all devices
// (Windows, Linux, macOS, etc.) via Microsoft Edge's online TTS service.
// If stripPunct is true, all punctuation is removed before TTS to avoid
// TTS-induced pauses between words.
func GenerateVoiceBuffer(text, voice, rate string, stripPunct bool) ([]byte, error) {
	cleaned := stripMarkdown(text)
	if stripPunct {
		cleaned = stripPausePunctuation(cleaned)
	}
	if cleaned == "" {
		cleaned = "..."
	}

	comm, err := communicate.NewCommunicate(
		cleaned,
		voice,
		rate,
		"+0%",
		"+0Hz",
		"",
		10,
		60,
	)
	if err != nil {
		return nil, fmt.Errorf("initialization error: %w", err)
	}

	var buf bytes.Buffer
	ctx := context.Background()

	// Stream returns a chunk channel and an error channel
	chunkChan, errChan := comm.Stream(ctx)

	// Write audio chunks directly into the memory buffer
	for chunk := range chunkChan {
		if chunk.Type == "audio" {
			if _, err := buf.Write(chunk.Data); err != nil {
				return nil, fmt.Errorf("buffer write error: %w", err)
			}
		}
	}

	// Check for streaming errors
	if err := <-errChan; err != nil {
		return nil, fmt.Errorf("streaming error: %w", err)
	}

	return buf.Bytes(), nil
}

// SpeakToBytes generates MP3 audio from the given text using
// Microsoft Edge's online TTS service, returning the audio in memory.
// No temporary files are written to disk.
// speed is 1-9 where the value maps directly to a multiplier of 1.nx
// (e.g. 3 = 1.3x normal speed, 5 = 1.5x). 3 is the default.
// If stripPunct is true, all punctuation is removed before TTS to avoid
// TTS-induced pauses between words.
func SpeakToBytes(text string, speed int, stripPunct bool) ([]byte, error) {
	return SpeakToBytesWithVoice(text, DefaultVoice, speed, stripPunct)
}

// SpeakToBytesWithVoice generates MP3 audio with the selected Edge TTS voice.
func SpeakToBytesWithVoice(text, voiceName string, speed int, stripPunct bool) ([]byte, error) {
	// Clamp speed to valid range 1-9
	if speed < 1 {
		speed = 1
	}
	if speed > 9 {
		speed = 9
	}
	rate := fmt.Sprintf("+%d%%", speed*10)
	if strings.TrimSpace(voiceName) == "" {
		voiceName = DefaultVoice
	}
	return GenerateVoiceBuffer(text, voiceName, rate, stripPunct)
}
