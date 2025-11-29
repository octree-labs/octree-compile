package internal

import (
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
)

// WordCountHandler handles word count requests using texcount
func WordCountHandler(c *gin.Context) {
	var req WordCountRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, ErrorResponse{
			Error:   "Invalid request",
			Message: "Could not parse JSON payload",
		})
		return
	}

	if len(req.Files) == 0 {
		c.JSON(http.StatusBadRequest, ErrorResponse{
			Error:   "Invalid request",
			Message: "The files array must contain at least one file",
		})
		return
	}

	// Create temp directory for files
	tempDir, err := os.MkdirTemp("", "texcount-")
	if err != nil {
		c.JSON(http.StatusInternalServerError, ErrorResponse{
			Error:   "Internal error",
			Message: "Failed to create temporary directory",
		})
		return
	}
	defer os.RemoveAll(tempDir)

	// Write files to temp directory and find main tex file
	var mainTexFile string
	var texFiles []string
	for _, file := range req.Files {
		filePath := filepath.Join(tempDir, file.Path)

		// Create parent directories if needed
		if err := os.MkdirAll(filepath.Dir(filePath), 0755); err != nil {
			c.JSON(http.StatusInternalServerError, ErrorResponse{
				Error:   "Internal error",
				Message: fmt.Sprintf("Failed to create directory for %s", file.Path),
			})
			return
		}

		// Write file content
		if err := os.WriteFile(filePath, []byte(file.Content), 0644); err != nil {
			c.JSON(http.StatusInternalServerError, ErrorResponse{
				Error:   "Internal error",
				Message: fmt.Sprintf("Failed to write file %s", file.Path),
			})
			return
		}

		// Track .tex files
		if strings.HasSuffix(strings.ToLower(file.Path), ".tex") {
			texFiles = append(texFiles, file.Path)
			// Check if this is the main file (contains \documentclass)
			if strings.Contains(file.Content, "\\documentclass") {
				mainTexFile = file.Path
			}
		}
	}

	if len(texFiles) == 0 {
		c.JSON(http.StatusBadRequest, ErrorResponse{
			Error:   "Invalid request",
			Message: "No .tex files found to count",
		})
		return
	}

	// If no main file found, use the first tex file
	if mainTexFile == "" {
		mainTexFile = texFiles[0]
	}

	// Run texcount with options for detailed output
	// -inc: include \input and \include files
	// -sum: show sum with breakdown
	mainFilePath := filepath.Join(tempDir, mainTexFile)
	cmd := exec.Command("texcount",
		"-inc",
		"-sum",
		"-utf8", // UTF-8 encoding
		mainFilePath,
	)
	cmd.Dir = tempDir

	output, _ := cmd.CombinedOutput()
	rawOutput := string(output)

	// Parse texcount output
	total, byFile := parseTexcountOutput(rawOutput, mainTexFile)

	// Build summary
	summary := fmt.Sprintf("Total: %d words, %d headers, %d captions, %d inline math, %d display math",
		total.Words, total.Headers, total.Captions, total.MathInline, total.MathDisplay)

	c.JSON(http.StatusOK, WordCountResponse{
		Success:   true,
		Total:     total,
		ByFile:    byFile,
		Summary:   summary,
		RawOutput: rawOutput,
	})
}

// parseTexcountOutput parses texcount output
// Standard texcount output format:
// Words in text: 123
// Words in headers: 45
// Words outside text (captions, etc.): 6
// Number of headers: 7
// Number of floats/figures/tables: 8
// Number of math inlines: 9
// Number of math displayed: 10
func parseTexcountOutput(output string, defaultFile string) (WordCountStats, []FileWordCount) {
	var total WordCountStats
	var byFile []FileWordCount

	// Patterns for standard texcount output
	wordsInTextPattern := regexp.MustCompile(`Words in text:\s*(\d+)`)
	wordsInHeadersPattern := regexp.MustCompile(`Words in headers:\s*(\d+)`)
	wordsOutsidePattern := regexp.MustCompile(`Words outside text.*?:\s*(\d+)`)
	mathInlinePattern := regexp.MustCompile(`Number of math inlines:\s*(\d+)`)
	mathDisplayPattern := regexp.MustCompile(`Number of math displayed:\s*(\d+)`)

	// Also check for "Sum count" section for totals
	sumWordsPattern := regexp.MustCompile(`Sum count:\s*(\d+)`)

	// Parse words in text
	if matches := wordsInTextPattern.FindStringSubmatch(output); matches != nil {
		total.Words = atoi(matches[1])
	}

	// Parse words in headers
	if matches := wordsInHeadersPattern.FindStringSubmatch(output); matches != nil {
		total.Headers = atoi(matches[1])
	}

	// Parse captions/outside text
	if matches := wordsOutsidePattern.FindStringSubmatch(output); matches != nil {
		total.Captions = atoi(matches[1])
	}

	// Parse math inline
	if matches := mathInlinePattern.FindStringSubmatch(output); matches != nil {
		total.MathInline = atoi(matches[1])
	}

	// Parse math displayed
	if matches := mathDisplayPattern.FindStringSubmatch(output); matches != nil {
		total.MathDisplay = atoi(matches[1])
	}

	// If we found sum count, use that as the main word count
	if matches := sumWordsPattern.FindStringSubmatch(output); matches != nil {
		total.Words = atoi(matches[1])
	}

	// If still no words found, try simple number on its own line (texcount -total output)
	if total.Words == 0 {
		simplePattern := regexp.MustCompile(`^\s*(\d+)\s*$`)
		lines := strings.Split(output, "\n")
		for _, line := range lines {
			if matches := simplePattern.FindStringSubmatch(line); matches != nil {
				total.Words = atoi(matches[1])
				break
			}
		}
	}

	// Add single file entry if we have stats
	if total.Words > 0 || total.Headers > 0 || total.Captions > 0 {
		byFile = append(byFile, FileWordCount{
			File:  defaultFile,
			Stats: total,
		})
	}

	return total, byFile
}

func atoi(s string) int {
	n, _ := strconv.Atoi(s)
	return n
}
