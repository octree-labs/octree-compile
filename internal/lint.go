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

// LintHandler handles LaTeX linting requests using chktex
func LintHandler(c *gin.Context) {
	var req LintRequest
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
	tempDir, err := os.MkdirTemp("", "chktex-")
	if err != nil {
		c.JSON(http.StatusInternalServerError, ErrorResponse{
			Error:   "Internal error",
			Message: "Failed to create temporary directory",
		})
		return
	}
	defer os.RemoveAll(tempDir)

	// Write files to temp directory
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

		// Track .tex files for linting
		if strings.HasSuffix(strings.ToLower(file.Path), ".tex") {
			texFiles = append(texFiles, file.Path)
		}
	}

	if len(texFiles) == 0 {
		c.JSON(http.StatusBadRequest, ErrorResponse{
			Error:   "Invalid request",
			Message: "No .tex files found to lint",
		})
		return
	}

	// Run chktex on all tex files
	var allWarnings []LintWarning
	var rawOutputBuilder strings.Builder

	for _, texFile := range texFiles {
		filePath := filepath.Join(tempDir, texFile)

		// Run chktex with parseable output format
		// -q: quiet mode (no banner)
		// -v0: minimal verbosity
		// -f: format string for output
		cmd := exec.Command("chktex",
			"-q",
			"-v0",
			"-f", "%f:%l:%c:%k:%n:%m\n",
			filePath,
		)
		cmd.Dir = tempDir

		output, _ := cmd.CombinedOutput()
		rawOutputBuilder.WriteString(string(output))

		// Parse chktex output
		warnings := parseChktexOutput(string(output), texFile)
		allWarnings = append(allWarnings, warnings...)
	}

	// Count errors and warnings, build messages string
	errorCount := 0
	warnCount := 0
	var messagesBuilder strings.Builder
	for i, w := range allWarnings {
		if w.Severity == "error" {
			errorCount++
		} else {
			warnCount++
		}
		// Build human-readable message line
		if i > 0 {
			messagesBuilder.WriteString("\n")
		}
		messagesBuilder.WriteString(fmt.Sprintf("%s:%d:%d: [%s] %s", w.File, w.Line, w.Column, w.Severity, w.Message))
	}

	c.JSON(http.StatusOK, LintResponse{
		Success:    true,
		Warnings:   allWarnings,
		Messages:   messagesBuilder.String(),
		ErrorCount: errorCount,
		WarnCount:  warnCount,
		RawOutput:  rawOutputBuilder.String(),
	})
}

// parseChktexOutput parses chktex output in the format: file:line:col:kind:code:message
func parseChktexOutput(output string, defaultFile string) []LintWarning {
	var warnings []LintWarning

	// Pattern for our custom format: file:line:col:kind:code:message
	// kind is "Warning" or "Error"
	pattern := regexp.MustCompile(`^([^:]+):(\d+):(\d+):(\w+):(\d+):(.+)$`)

	lines := strings.Split(output, "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}

		matches := pattern.FindStringSubmatch(line)
		if matches == nil {
			continue
		}

		lineNum, _ := strconv.Atoi(matches[2])
		colNum, _ := strconv.Atoi(matches[3])
		kind := strings.ToLower(matches[4])
		code, _ := strconv.Atoi(matches[5])
		message := strings.TrimSpace(matches[6])

		// Normalize severity
		severity := "warning"
		if kind == "error" {
			severity = "error"
		}

		// Use relative path from the matches, or default
		file := matches[1]
		if filepath.IsAbs(file) {
			// Try to get just the filename
			file = filepath.Base(file)
		}
		// If file path contains temp dir artifacts, use the default
		if strings.Contains(file, "chktex-") {
			file = defaultFile
		}

		warnings = append(warnings, LintWarning{
			File:     file,
			Line:     lineNum,
			Column:   colNum,
			Severity: severity,
			Code:     code,
			Message:  message,
		})
	}

	return warnings
}
