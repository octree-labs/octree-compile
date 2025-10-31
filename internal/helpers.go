package internal

import "strings"

// truncateText truncates text to the last maxChars characters
func truncateText(text string, maxChars int) string {
	if len(text) <= maxChars {
		return text
	}
	return text[len(text)-maxChars:]
}

// tailLines returns the last maxLines lines from text
func tailLines(text string, maxLines int) string {
	if text == "" {
		return ""
	}
	lines := strings.Split(text, "\n")
	if len(lines) <= maxLines {
		return text
	}
	return strings.Join(lines[len(lines)-maxLines:], "\n")
}

// min returns the minimum of two integers
func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

