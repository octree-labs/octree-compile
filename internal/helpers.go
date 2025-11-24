package internal

import (
	"encoding/base64"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	storage_go "github.com/supabase-community/storage-go"
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

type bibliographyTool int

const (
	bibliographyToolNone bibliographyTool = iota
	bibliographyToolBibtex
	bibliographyToolBiber
)

func (t bibliographyTool) String() string {
	switch t {
	case bibliographyToolBibtex:
		return "bibtex"
	case bibliographyToolBiber:
		return "biber"
	default:
		return "none"
	}
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

func detectBibliographyTool(mainContent string, files []FileEntry) bibliographyTool {
	contentsToScan := []string{mainContent}

	for _, file := range files {
		if file.Encoding == "base64" {
			continue
		}
		if !strings.HasSuffix(file.Path, ".tex") && !strings.HasSuffix(file.Path, ".sty") && !strings.HasSuffix(file.Path, ".cls") {
			continue
		}
		contentsToScan = append(contentsToScan, file.Content)
	}

	seenBiblatex := false

	for _, content := range contentsToScan {
		lower := strings.ToLower(content)

		switch {
		case strings.Contains(lower, "backend=bibtex") || strings.Contains(lower, "backend = bibtex"):
			return bibliographyToolBibtex
		case strings.Contains(lower, "backend=biber") || strings.Contains(lower, "backend = biber"):
			seenBiblatex = true
		case strings.Contains(lower, "\\usepackage{biblatex}") ||
			(strings.Contains(lower, "\\usepackage[") && strings.Contains(lower, "{biblatex}")) ||
			strings.Contains(lower, "\\requirepackage{biblatex}") ||
			(strings.Contains(lower, "\\requirepackage[") && strings.Contains(lower, "{biblatex}")):
			seenBiblatex = true
		case strings.Contains(lower, "\\addbibresource") ||
			strings.Contains(lower, "\\printbibliography") ||
			strings.Contains(lower, "\\executebibliographyoptions"):
			seenBiblatex = true
		}
	}

	if seenBiblatex {
		return bibliographyToolBiber
	}

	return bibliographyToolBibtex
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
// Handles both text files and binary files (encoded as base64)
func createFileStructure(tempDir string, files []FileEntry) error {
	for _, file := range files {
		fullPath := filepath.Join(tempDir, file.Path)

		// Create directory if needed
		dir := filepath.Dir(fullPath)
		if err := os.MkdirAll(dir, 0755); err != nil {
			return fmt.Errorf("failed to create directory %s: %v", dir, err)
		}

		// Handle binary files encoded as base64
		if file.Encoding == "base64" {
			decoded, err := base64.StdEncoding.DecodeString(file.Content)
			if err != nil {
				return fmt.Errorf("failed to decode base64 file %s: %v", file.Path, err)
			}
			if err := os.WriteFile(fullPath, decoded, 0644); err != nil {
				return fmt.Errorf("failed to write binary file %s: %v", file.Path, err)
			}
		} else {
			// Text file
			if err := os.WriteFile(fullPath, []byte(file.Content), 0644); err != nil {
				return fmt.Errorf("failed to write text file %s: %v", file.Path, err)
			}
		}
	}

	return nil
}

// FileChanges represents changes between file sets
type FileChanges struct {
	Added           []FileEntry
	Modified        []FileEntry
	Deleted         []string // Just the paths
	HasTexChanges   bool
	HasBibChanges   bool
	HasAssetChanges bool
}

// diffFiles compares current files with cached file hashes and returns changes
func diffFiles(currentFiles []FileEntry, cachedHashes map[string]string) *FileChanges {
	changes := &FileChanges{
		Added:    []FileEntry{},
		Modified: []FileEntry{},
		Deleted:  []string{},
	}

	// Track which cached files we've seen
	seen := make(map[string]bool)

	// Check for added and modified files
	for _, file := range currentFiles {
		seen[file.Path] = true
		currentHash := HashFileContent(file.Content)

		if cachedHash, exists := cachedHashes[file.Path]; exists {
			// File exists in cache
			if currentHash != cachedHash {
				changes.Modified = append(changes.Modified, file)
				categorizeFileChange(file.Path, changes)
			}
		} else {
			// New file
			changes.Added = append(changes.Added, file)
			categorizeFileChange(file.Path, changes)
		}
	}

	// Check for deleted files
	for path := range cachedHashes {
		if !seen[path] {
			changes.Deleted = append(changes.Deleted, path)
			categorizeFileChange(path, changes)
		}
	}

	return changes
}

// categorizeFileChange updates the change flags based on file extension
func categorizeFileChange(path string, changes *FileChanges) {
	if strings.HasSuffix(path, ".tex") || strings.HasSuffix(path, ".sty") || strings.HasSuffix(path, ".cls") {
		changes.HasTexChanges = true
	} else if strings.HasSuffix(path, ".bib") {
		changes.HasBibChanges = true
	} else {
		// Images, data files, etc.
		changes.HasAssetChanges = true
	}
}

// buildFileHashMap creates a map of file path to content hash
func buildFileHashMap(files []FileEntry) map[string]string {
	hashes := make(map[string]string)
	for _, file := range files {
		hashes[file.Path] = HashFileContent(file.Content)
	}
	return hashes
}

// updateCachedFiles writes only changed files to the temp directory
func updateCachedFiles(tempDir string, changes *FileChanges) error {
	// Write added files
	for _, file := range changes.Added {
		if err := writeFile(tempDir, file); err != nil {
			return err
		}
	}

	// Write modified files
	for _, file := range changes.Modified {
		if err := writeFile(tempDir, file); err != nil {
			return err
		}
	}

	// Delete removed files
	for _, path := range changes.Deleted {
		fullPath := filepath.Join(tempDir, path)
		if err := os.Remove(fullPath); err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("failed to delete file %s: %v", path, err)
		}
	}

	return nil
}

// writeFile writes a single file to the temp directory
func writeFile(tempDir string, file FileEntry) error {
	fullPath := filepath.Join(tempDir, file.Path)

	// Create directory if needed
	dir := filepath.Dir(fullPath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("failed to create directory %s: %v", dir, err)
	}

	// Handle binary files encoded as base64
	if file.Encoding == "base64" {
		decoded, err := base64.StdEncoding.DecodeString(file.Content)
		if err != nil {
			return fmt.Errorf("failed to decode base64 file %s: %v", file.Path, err)
		}
		if err := os.WriteFile(fullPath, decoded, 0644); err != nil {
			return fmt.Errorf("failed to write binary file %s: %v", file.Path, err)
		}
	} else {
		// Text file
		if err := os.WriteFile(fullPath, []byte(file.Content), 0644); err != nil {
			return fmt.Errorf("failed to write text file %s: %v", file.Path, err)
		}
	}

	return nil
}

func FetchFilesFromSupabase(projectID, supabaseURL, supabaseKey string) ([]FileEntry, error) {
	client := storage_go.NewClient(supabaseURL+"/storage/v1", supabaseKey, nil)

	bucketName := "octree"
	folderPath := projectID

	result, err := client.ListFiles(bucketName, folderPath, storage_go.FileSearchOptions{
		Limit: 1000,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to list files from Supabase: %v", err)
	}

	var files []FileEntry

	for _, fileInfo := range result {
		if fileInfo.Id == "" {
			continue
		}

		fileName := fileInfo.Name
		if fileName == "" {
			continue
		}

		fullPath := folderPath + "/" + fileName

		content, err := client.DownloadFile(bucketName, fullPath)
		if err != nil {
			return nil, fmt.Errorf("failed to download file %s: %v", fullPath, err)
		}

		fileEntry := FileEntry{
			Path: fileName,
		}

		if isBinaryFile(fileName) {
			fileEntry.Encoding = "base64"
			fileEntry.Content = base64.StdEncoding.EncodeToString(content)
		} else {
			fileEntry.Content = string(content)
		}

		files = append(files, fileEntry)
	}

	return files, nil
}

func isBinaryFile(filename string) bool {
	binaryExtensions := []string{
		".pdf", ".png", ".jpg", ".jpeg", ".gif", ".bmp", ".eps", ".ps",
		".tif", ".tiff", ".pbm", ".svg", ".ico",
	}

	lowerName := strings.ToLower(filename)
	for _, ext := range binaryExtensions {
		if strings.HasSuffix(lowerName, ext) {
			return true
		}
	}

	return false
}
