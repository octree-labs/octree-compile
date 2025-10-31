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

func (c *Compiler) Compile(content string, files []FileEntry, enqueuedAt time.Time) *CompileResult {
	receivedAt := time.Now()
	queueMs := receivedAt.Sub(enqueuedAt).Milliseconds()

	log.Printf("[%s] ==== COMPILE REQUEST RECEIVED ====", c.RequestID)
	
	// Determine if single or multi-file
	isMultiFile := len(files) > 0
	var mainContent string
	
	if isMultiFile {
		log.Printf("[%s] Multi-file project: %d files", c.RequestID, len(files))
		// Find main.tex content for preview and detection
		for _, f := range files {
			if f.Path == "main.tex" {
				mainContent = f.Content
				break
			}
		}
		if mainContent == "" {
			log.Printf("[%s] Warning: No main.tex found in files, using first .tex file", c.RequestID)
			for _, f := range files {
				if strings.HasSuffix(f.Path, ".tex") {
					mainContent = f.Content
					break
				}
			}
		}
	} else {
		log.Printf("[%s] Single-file mode (backward compat)", c.RequestID)
		log.Printf("[%s] Content length: %d bytes", c.RequestID, len(content))
		mainContent = content
	}
	
	log.Printf("[%s] Queue wait: %dms", c.RequestID, queueMs)
	
	// Preview first 120 chars
	preview := strings.ReplaceAll(mainContent[:min(120, len(mainContent))], "\n", " ")
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

	// Write files
	if isMultiFile {
		// Write all files preserving directory structure
		if err := createFileStructure(tempDir, files); err != nil {
			return c.errorResult(metadata, fmt.Sprintf("Failed to write files: %v", err), queueMs, receivedAt)
		}
		log.Printf("[%s] Multi-file structure created in: %s", c.RequestID, tempDir)
	} else {
		// Single file - backward compatible
		if err := os.WriteFile(texFilePath, []byte(content), 0644); err != nil {
			return c.errorResult(metadata, fmt.Sprintf("Failed to write TeX file: %v", err), queueMs, receivedAt)
		}
	}

	metadata.Status = "written"
	c.persistMetadata(metadata)
	log.Printf("[%s] TeX content written to: %s", c.RequestID, texFilePath)

	// Smart compilation detection
	needsBib := needsBibliography(mainContent, files)
	needsMultiPass := needsMultiplePasses(mainContent)
	
	log.Printf("[%s] Compilation strategy - Bibliography: %v, Multi-pass: %v", c.RequestID, needsBib, needsMultiPass)
	
	// Run compilation passes
	var stdout, stderr bytes.Buffer
	exitCode := 0
	
	if needsBib {
		// Full pipeline: pdflatex -> bibtex -> pdflatex -> pdflatex
		log.Printf("[%s] Running full bibliography pipeline", c.RequestID)
		
		// First pdflatex pass
		if err := c.runPdflatex(tempDir, texFilePath, &stdout, &stderr); err != nil {
			if exitError, ok := err.(*exec.ExitError); ok {
				exitCode = exitError.ExitCode()
			} else {
				exitCode = -1
			}
		}
		
		if exitCode == 0 {
			// Run bibtex
			log.Printf("[%s] Running bibtex...", c.RequestID)
			bibtexCmd := exec.Command("bibtex", "main")
			bibtexCmd.Dir = tempDir
			bibtexCmd.Stdout = &stdout
			bibtexCmd.Stderr = &stderr
			_ = bibtexCmd.Run() // bibtex errors are often non-fatal
			
			// Second pdflatex pass
			log.Printf("[%s] Running pdflatex (pass 2/3)...", c.RequestID)
			if err := c.runPdflatex(tempDir, texFilePath, &stdout, &stderr); err != nil {
				if exitError, ok := err.(*exec.ExitError); ok {
					exitCode = exitError.ExitCode()
				}
			}
			
			// Third pdflatex pass
			if exitCode == 0 {
				log.Printf("[%s] Running pdflatex (pass 3/3)...", c.RequestID)
				if err := c.runPdflatex(tempDir, texFilePath, &stdout, &stderr); err != nil {
					if exitError, ok := err.(*exec.ExitError); ok {
						exitCode = exitError.ExitCode()
					}
				}
			}
		}
	} else if needsMultiPass {
		// Two passes for cross-references
		log.Printf("[%s] Running two-pass compilation for cross-references", c.RequestID)
		
		if err := c.runPdflatex(tempDir, texFilePath, &stdout, &stderr); err != nil {
			if exitError, ok := err.(*exec.ExitError); ok {
				exitCode = exitError.ExitCode()
			} else {
				exitCode = -1
			}
		}
		
		if exitCode == 0 {
			log.Printf("[%s] Running pdflatex (pass 2/2)...", c.RequestID)
			if err := c.runPdflatex(tempDir, texFilePath, &stdout, &stderr); err != nil {
				if exitError, ok := err.(*exec.ExitError); ok {
					exitCode = exitError.ExitCode()
				}
			}
		}
	} else {
		// Single pass - fast path
		log.Printf("[%s] Running single-pass compilation", c.RequestID)
		err = c.runPdflatex(tempDir, texFilePath, &stdout, &stderr)
		if err != nil {
			if exitError, ok := err.(*exec.ExitError); ok {
				exitCode = exitError.ExitCode()
			} else {
				exitCode = -1
			}
		}
	}

	completedAt := time.Now()
	durationMs := completedAt.Sub(receivedAt).Milliseconds()

	metadata.CompletedAt = completedAt
	metadata.DurationMs = durationMs
	metadata.ExitCode = exitCode
	metadata.StdoutBytes = stdout.Len()
	metadata.StderrBytes = stderr.Len()

	log.Printf("[%s] Compilation completed with exit code: %d", c.RequestID, exitCode)
	log.Printf("[%s] Total stdout length: %d bytes", c.RequestID, stdout.Len())
	log.Printf("[%s] Total stderr length: %d bytes", c.RequestID, stderr.Len())

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

// runPdflatex runs a single pdflatex pass
func (c *Compiler) runPdflatex(tempDir, texFilePath string, stdout, stderr *bytes.Buffer) error {
	cmd := exec.Command("pdflatex",
		"-interaction=nonstopmode",
		"-halt-on-error",
		"-file-line-error",
		fmt.Sprintf("-output-directory=%s", tempDir),
		texFilePath,
	)
	cmd.Dir = tempDir
	cmd.Stdout = stdout
	cmd.Stderr = stderr
	
	return cmd.Run()
}

