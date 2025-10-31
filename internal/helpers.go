package internal

import (
	"os"
	"path/filepath"
	"strings"
)

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

// needsBibliography checks if content requires bibliography processing
func needsBibliography(content string, files []FileEntry) bool {
	// Check for .bib files in the files array
	for _, file := range files {
		if strings.HasSuffix(file.Path, ".bib") {
			return true
		}
	}
	
	// Check for bibliography commands in content
	bibCommands := []string{
		"\\bibliography{",
		"\\addbibresource{",
		"\\cite{",
		"\\citep{",
		"\\citet{",
		"\\nocite{",
	}
	
	for _, cmd := range bibCommands {
		if strings.Contains(content, cmd) {
			return true
		}
	}
	
	return false
}

// needsMultiplePasses checks if content requires multiple compilation passes
func needsMultiplePasses(content string) bool {
	// Check for cross-reference commands
	refCommands := []string{
		"\\ref{",
		"\\pageref{",
		"\\eqref{",
		"\\label{",
		"\\tableofcontents",
		"\\listoffigures",
		"\\listoftables",
	}
	
	for _, cmd := range refCommands {
		if strings.Contains(content, cmd) {
			return true
		}
	}
	
	return false
}

// createFileStructure writes all files to the temp directory, preserving directory structure
func createFileStructure(tempDir string, files []FileEntry) error {
	for _, file := range files {
		fullPath := filepath.Join(tempDir, file.Path)
		
		// Create directory if needed
		dir := filepath.Dir(fullPath)
		if err := os.MkdirAll(dir, 0755); err != nil {
			return err
		}
		
		// Write file
		if err := os.WriteFile(fullPath, []byte(file.Content), 0644); err != nil {
			return err
		}
	}
	
	return nil
}

