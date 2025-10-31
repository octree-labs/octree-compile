package internal

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/google/uuid"
)

const (
	MaxLogChars  = 5000
	LogTailLines = 80
)

var historyDir string

// SetHistoryDir sets the directory for compilation history logs
func SetHistoryDir(dir string) {
	historyDir = dir
}

type Compiler struct {
	RequestID string
}

func New() *Compiler {
	return &Compiler{
		RequestID: uuid.New().String(),
	}
}

func (c *Compiler) Compile(content string, enqueuedAt time.Time) *CompileResult {
	receivedAt := time.Now()
	queueMs := receivedAt.Sub(enqueuedAt).Milliseconds()

	log.Printf("[%s] ==== COMPILE REQUEST RECEIVED ====", c.RequestID)
	log.Printf("[%s] Content length: %d bytes", c.RequestID, len(content))
	log.Printf("[%s] Queue wait: %dms", c.RequestID, queueMs)
	
	// Preview first 120 chars
	preview := strings.ReplaceAll(content[:min(120, len(content))], "\n", " ")
	log.Printf("[%s] TeX preview: %s...", c.RequestID, preview)

	metadata := &compileMetadata{
		RequestID:  c.RequestID,
		EnqueuedAt: enqueuedAt,
		ReceivedAt: receivedAt,
		QueueMs:    queueMs,
		Status:     "processing",
	}

	// Create temporary directory
	tempDir, err := os.MkdirTemp("", "latex-*")
	if err != nil {
		return c.errorResult(metadata, fmt.Sprintf("Failed to create temp directory: %v", err), queueMs, receivedAt)
	}
	defer os.RemoveAll(tempDir)

	texFilePath := filepath.Join(tempDir, "main.tex")
	pdfPath := filepath.Join(tempDir, "main.pdf")
	logPath := filepath.Join(tempDir, "main.log")

	// Write TeX content
	if err := os.WriteFile(texFilePath, []byte(content), 0644); err != nil {
		return c.errorResult(metadata, fmt.Sprintf("Failed to write TeX file: %v", err), queueMs, receivedAt)
	}

	metadata.Status = "written"
	c.persistMetadata(metadata)
	log.Printf("[%s] TeX content written to: %s", c.RequestID, texFilePath)

	// Run pdflatex
	log.Printf("[%s] Starting pdflatex compilation...", c.RequestID)
	cmd := exec.Command("pdflatex",
		"-interaction=nonstopmode",
		"-halt-on-error",
		"-file-line-error",
		fmt.Sprintf("-output-directory=%s", tempDir),
		texFilePath,
	)
	cmd.Dir = tempDir

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err = cmd.Run()
	exitCode := 0
	if err != nil {
		if exitError, ok := err.(*exec.ExitError); ok {
			exitCode = exitError.ExitCode()
		} else {
			exitCode = -1
		}
	}

	completedAt := time.Now()
	durationMs := completedAt.Sub(receivedAt).Milliseconds()

	metadata.CompletedAt = completedAt
	metadata.DurationMs = durationMs
	metadata.ExitCode = exitCode
	metadata.StdoutBytes = stdout.Len()
	metadata.StderrBytes = stderr.Len()

	log.Printf("[%s] pdflatex exited with code: %d", c.RequestID, exitCode)
	log.Printf("[%s] stdout length: %d bytes", c.RequestID, stdout.Len())
	log.Printf("[%s] stderr length: %d bytes", c.RequestID, stderr.Len())

	// Check if PDF was created
	if pdfData, err := os.ReadFile(pdfPath); err == nil {
		log.Printf("[%s] PDF created successfully: %d bytes", c.RequestID, len(pdfData))

		// Verify PDF format
		if len(pdfData) < 4 || string(pdfData[:4]) != "%PDF" {
			return c.errorResult(metadata, "Invalid PDF format", queueMs, receivedAt)
		}

		// Calculate SHA256
		hash := sha256.Sum256(pdfData)
		sha256Hex := hex.EncodeToString(hash[:])

		// Read log file
		logContent := ""
		if logData, err := os.ReadFile(logPath); err == nil {
			logContent = string(logData)
		}

		metadata.Status = "success"
		metadata.PDFSize = len(pdfData)
		metadata.SHA256 = sha256Hex
		metadata.LogTail = tailLines(truncateText(logContent, MaxLogChars), LogTailLines)
		c.persistMetadata(metadata)

		log.Printf("[%s] Compilation successful", c.RequestID)

		return &CompileResult{
			RequestID:  c.RequestID,
			Success:    true,
			PDFData:    pdfData,
			SHA256:     sha256Hex,
			QueueMs:    queueMs,
			DurationMs: durationMs,
			PDFSize:    len(pdfData),
		}
	}

	// Compilation failed
	logContent := ""
	if logData, err := os.ReadFile(logPath); err == nil {
		logContent = string(logData)
		log.Printf("[%s] LaTeX log excerpt: %s", c.RequestID, logContent[:min(500, len(logContent))])
	}

	metadata.Status = "error"
	metadata.LogTail = tailLines(logContent, LogTailLines)
	c.persistMetadata(metadata)

	return &CompileResult{
		RequestID:    c.RequestID,
		Success:      false,
		ErrorMessage: "PDF file not generated",
		Stdout:       truncateText(stdout.String(), MaxLogChars),
		Stderr:       truncateText(stderr.String(), MaxLogChars),
		LogTail:      metadata.LogTail,
		QueueMs:      queueMs,
		DurationMs:   durationMs,
	}
}

func (c *Compiler) errorResult(metadata *compileMetadata, message string, queueMs int64, receivedAt time.Time) *CompileResult {
	metadata.Status = "error"
	metadata.Error = message
	metadata.CompletedAt = time.Now()
	metadata.DurationMs = metadata.CompletedAt.Sub(receivedAt).Milliseconds()
	c.persistMetadata(metadata)

	log.Printf("[%s] Error: %s", c.RequestID, message)

	return &CompileResult{
		RequestID:    c.RequestID,
		Success:      false,
		ErrorMessage: message,
		QueueMs:      queueMs,
		DurationMs:   metadata.DurationMs,
	}
}

func (c *Compiler) persistMetadata(metadata *compileMetadata) {
	if historyDir == "" {
		return
	}

	data, err := json.MarshalIndent(metadata, "", "  ")
	if err != nil {
		log.Printf("[%s] Failed to marshal metadata: %v", c.RequestID, err)
		return
	}

	filePath := filepath.Join(historyDir, fmt.Sprintf("%s.json", c.RequestID))
	if err := os.WriteFile(filePath, data, 0644); err != nil {
		log.Printf("[%s] Failed to persist metadata: %v", c.RequestID, err)
	}
}

