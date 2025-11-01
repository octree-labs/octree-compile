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

func (c *Compiler) Compile(content string, files []FileEntry, enqueuedAt time.Time, projectID string) *CompileResult {
	receivedAt := time.Now()
	queueMs := receivedAt.Sub(enqueuedAt).Milliseconds()

	log.Printf("[%s] ==== COMPILE REQUEST RECEIVED ====", c.RequestID)
	if projectID != "" {
		log.Printf("[%s] ProjectID: %s", c.RequestID, projectID)
	}
	
	// Determine if single or multi-file
	isMultiFile := len(files) > 0
	var mainContent string
	
	if isMultiFile {
		// Count text vs binary files
		textFiles, binaryFiles := 0, 0
		for _, f := range files {
			if f.Encoding == "base64" {
				binaryFiles++
			} else {
				textFiles++
			}
		}
		log.Printf("[%s] Multi-file project: %d files (%d text, %d binary)", c.RequestID, len(files), textFiles, binaryFiles)
		
		// Find main.tex content for preview and detection
		for _, f := range files {
			if f.Path == "main.tex" && f.Encoding != "base64" {
				mainContent = f.Content
				break
			}
		}
		if mainContent == "" {
			log.Printf("[%s] Warning: No main.tex found in files, using first .tex file", c.RequestID)
			for _, f := range files {
				if strings.HasSuffix(f.Path, ".tex") && f.Encoding != "base64" {
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

	// ==== CACHING LOGIC ====
	cache := GetCache()
	var tempDir string
	var fileChanges *FileChanges
	var isIncremental bool
	var shouldCleanup bool = true

	if projectID != "" && isMultiFile {
		// Acquire project lock to serialize compilations
		cache.LockProject(projectID)
		defer cache.UnlockProject(projectID)

		// Check content hash for instant cache hit
		contentHash := HashFileSet(files)
		if cache.CheckContentHash(projectID, contentHash) {
			entry, _ := cache.Get(projectID)
			if entry != nil && len(entry.LastPDFData) > 0 {
				log.Printf("[%s] CACHE HIT: Content unchanged, returning cached PDF", c.RequestID)
				completedAt := time.Now()
				durationMs := completedAt.Sub(receivedAt).Milliseconds()

				return &CompileResult{
					RequestID:  c.RequestID,
					Success:    true,
					PDFData:    entry.LastPDFData,
					SHA256:     entry.LastSHA256,
					QueueMs:    queueMs,
					DurationMs: durationMs,
					PDFSize:    len(entry.LastPDFData),
					CacheHit:   true,
				}
			}
		}

		// Check if we have a cached temp directory
		entry, exists := cache.Get(projectID)
		if exists && entry.TempDir != "" {
			// Verify temp dir still exists
			if _, err := os.Stat(entry.TempDir); err == nil {
				log.Printf("[%s] Using cached temp directory: %s", c.RequestID, entry.TempDir)
				tempDir = entry.TempDir
				isIncremental = true
				shouldCleanup = false // Don't clean up cached dir

				// Diff files to find changes
				fileChanges = diffFiles(files, entry.FileHashes)
				changeCount := len(fileChanges.Added) + len(fileChanges.Modified) + len(fileChanges.Deleted)
				log.Printf("[%s] File changes: %d added, %d modified, %d deleted (total: %d)",
					c.RequestID, len(fileChanges.Added), len(fileChanges.Modified), len(fileChanges.Deleted), changeCount)
				log.Printf("[%s] Change types: tex=%v bib=%v assets=%v",
					c.RequestID, fileChanges.HasTexChanges, fileChanges.HasBibChanges, fileChanges.HasAssetChanges)
			} else {
				log.Printf("[%s] Cached temp dir no longer exists, creating new one", c.RequestID)
			}
		}
	}

	// Create temporary directory if not using cache
	if tempDir == "" {
		var err error
		tempDir, err = os.MkdirTemp("", "latex-*")
		if err != nil {
			return c.errorResult(metadata, fmt.Sprintf("Failed to create temp directory: %v", err), queueMs, receivedAt)
		}
		log.Printf("[%s] Created new temp directory: %s", c.RequestID, tempDir)
		
		// If we have a projectID, we want to cache this directory, so don't clean it up
		if projectID != "" && isMultiFile {
			shouldCleanup = false
			log.Printf("[%s] Temp directory will be cached for project: %s", c.RequestID, projectID)
		}
	}

	if shouldCleanup {
		defer os.RemoveAll(tempDir)
	}

	texFilePath := filepath.Join(tempDir, "main.tex")
	pdfPath := filepath.Join(tempDir, "main.pdf")
	logPath := filepath.Join(tempDir, "main.log")

	// Write files
	if isMultiFile {
		if isIncremental && fileChanges != nil {
			// Incremental: only write changed files
			if err := updateCachedFiles(tempDir, fileChanges); err != nil {
				return c.errorResult(metadata, fmt.Sprintf("Failed to update files: %v", err), queueMs, receivedAt)
			}
			log.Printf("[%s] Incremental update: wrote %d changed files", c.RequestID,
				len(fileChanges.Added)+len(fileChanges.Modified)+len(fileChanges.Deleted))
		} else {
			// Full write: all files
			if err := createFileStructure(tempDir, files); err != nil {
				return c.errorResult(metadata, fmt.Sprintf("Failed to write files: %v", err), queueMs, receivedAt)
			}
			log.Printf("[%s] Multi-file structure created in: %s", c.RequestID, tempDir)
		}
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
	
	// Override strategy for incremental compilation
	if isIncremental && fileChanges != nil {
		// Optimization based on what changed:
		// Key insight: If .bib unchanged, .bbl file is still valid → skip bibtex!
		
		if !fileChanges.HasBibChanges {
			// Case 1, 3, 5: .bib files unchanged → .bbl file is still valid
			if fileChanges.HasTexChanges && !fileChanges.HasAssetChanges {
				// Case 1: Only .tex changed (most common!)
				log.Printf("[%s] INCREMENTAL: Only .tex changed, skipping bibtex (reusing .bbl)", c.RequestID)
				needsBib = false
				// Keep needsMultiPass as-is for cross-references in .tex
			} else if !fileChanges.HasTexChanges && fileChanges.HasAssetChanges {
				// Case 3: Only assets changed
				log.Printf("[%s] INCREMENTAL: Only assets changed, single pass", c.RequestID)
				needsBib = false
				needsMultiPass = false // Cross-refs already resolved
			} else if fileChanges.HasTexChanges && fileChanges.HasAssetChanges {
				// Case 5: .tex + assets changed
				log.Printf("[%s] INCREMENTAL: .tex + assets changed, skipping bibtex", c.RequestID)
				needsBib = false
				// Keep needsMultiPass as-is for cross-references in .tex
			} else {
				// Case 8: Nothing changed (shouldn't happen, but defensive)
				log.Printf("[%s] INCREMENTAL: No changes detected", c.RequestID)
				needsBib = false
				needsMultiPass = false
			}
		} else if !fileChanges.HasTexChanges {
			// Case 2, 6: Only .bib (and maybe assets) changed
			// → .aux file still valid, but need to regenerate .bbl
			// TODO: Optimize to skip first pdflatex (requires new pipeline branch)
			log.Printf("[%s] INCREMENTAL: Only .bib/assets changed (could skip first pdflatex)", c.RequestID)
			needsBib = true
			// Currently runs full pipeline, future optimization possible
		}
		// Case 4, 7: .tex + .bib changed → No optimization, run full pipeline
		// This falls through with original needsBib and needsMultiPass values
	}
	
	log.Printf("[%s] Compilation strategy - Bibliography: %v, Multi-pass: %v, Incremental: %v", 
		c.RequestID, needsBib, needsMultiPass, isIncremental)
	
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
		if err := c.runPdflatex(tempDir, texFilePath, &stdout, &stderr); err != nil {
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

		// Cache the result for future compilations
		if projectID != "" && isMultiFile {
			contentHash := HashFileSet(files)
			fileHashes := buildFileHashMap(files)
			
			cacheEntry := &CacheEntry{
				ProjectID:      projectID,
				TempDir:        tempDir,
				FileHashes:     fileHashes,
				ContentHash:    contentHash,
				LastPDFData:    pdfData,
				LastSHA256:     sha256Hex,
				LastAccessTime: time.Now(),
			}
			
			cache.Set(projectID, cacheEntry)
			log.Printf("[%s] Cached compilation result for project %s", c.RequestID, projectID)
		}

		log.Printf("[%s] Compilation successful", c.RequestID)

		return &CompileResult{
			RequestID:  c.RequestID,
			Success:    true,
			PDFData:    pdfData,
			SHA256:     sha256Hex,
			QueueMs:    queueMs,
			DurationMs: durationMs,
			PDFSize:    len(pdfData),
			CacheHit:   false,
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

