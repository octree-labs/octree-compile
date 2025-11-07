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

type compileSession struct {
	compiler      *Compiler
	files         []FileEntry
	projectID     string
	enqueuedAt    time.Time
	receivedAt    time.Time
	queueMs       int64
	mainContent   string
	mainFilePath  string
	jobName       string
	tempDir       string
	texFilePath   string
	pdfPath       string
	logPath       string
	fileChanges   *FileChanges
	isIncremental bool
	shouldCleanup bool
	metadata      *compileMetadata
	stdout        bytes.Buffer
	stderr        bytes.Buffer
	exitCode      int
	bibTool       bibliographyTool
}

func newCompileSession(compiler *Compiler, files []FileEntry, enqueuedAt time.Time, projectID string) *compileSession {
	receivedAt := time.Now()
	queueMs := receivedAt.Sub(enqueuedAt).Milliseconds()

	session := &compileSession{
		compiler:      compiler,
		files:         files,
		projectID:     projectID,
		enqueuedAt:    enqueuedAt,
		receivedAt:    receivedAt,
		queueMs:       queueMs,
		shouldCleanup: true,
		metadata: &compileMetadata{
			RequestID:  compiler.RequestID,
			EnqueuedAt: enqueuedAt,
			ReceivedAt: receivedAt,
			QueueMs:    queueMs,
			Status:     "processing",
		},
		bibTool: bibliographyToolNone,
	}

	session.logInitialDetails()

	return session
}

func (c *Compiler) Compile(files []FileEntry, enqueuedAt time.Time, projectID string) *CompileResult {
	session := newCompileSession(c, files, enqueuedAt, projectID)

	cache := GetCache()
	if session.projectID != "" {
		cache.LockProject(session.projectID)
		defer cache.UnlockProject(session.projectID)
	}

	if result := session.tryServeCachedPDF(cache); result != nil {
		return result
	}

	if errResult := session.prepareWorkspace(cache); errResult != nil {
		return errResult
	}
	defer session.cleanup()

	needsBib, needsMultiPass := session.determineStrategy()
	session.runCompilation(needsBib, needsMultiPass)

	return session.finalize(cache)
}

func (s *compileSession) logInitialDetails() {
	log.Printf("[%s] ==== COMPILE REQUEST RECEIVED ====", s.compiler.RequestID)
	if s.projectID != "" {
		log.Printf("[%s] ProjectID: %s", s.compiler.RequestID, s.projectID)
	}

	s.mainContent = s.extractMainContent()

	log.Printf("[%s] Queue wait: %dms", s.compiler.RequestID, s.queueMs)

	preview := s.mainContent[:min(120, len(s.mainContent))]
	preview = strings.ReplaceAll(preview, "\n", " ")
	log.Printf("[%s] TeX preview: %s...", s.compiler.RequestID, preview)
}

func (s *compileSession) extractMainContent() string {
	textFiles, binaryFiles := 0, 0
	for _, f := range s.files {
		switch f.Encoding {
		case "base64":
			binaryFiles++
		default:
			textFiles++
		}
	}
	log.Printf("[%s] Project files received: %d total (%d text, %d binary)", s.compiler.RequestID, len(s.files), textFiles, binaryFiles)

	mainFile, hasDocclass, found := findMainFile(s.files)
	if !found {
		log.Printf("[%s] Warning: No LaTeX source file detected in request", s.compiler.RequestID)
		s.mainFilePath = ""
		return ""
	}

	s.mainFilePath = mainFile.Path

	if hasDocclass {
		log.Printf("[%s] Detected main file by \\documentclass: %s", s.compiler.RequestID, mainFile.Path)
	} else {
		log.Printf("[%s] Warning: No \\documentclass found; using first .tex file: %s", s.compiler.RequestID, mainFile.Path)
	}

	return mainFile.Content
}

func findMainFile(files []FileEntry) (FileEntry, bool, bool) {
	var fallback *FileEntry

	for i := range files {
		file := files[i]
		switch {
		case file.Encoding == "base64":
			continue
		case !strings.HasSuffix(file.Path, ".tex"):
			continue
		case strings.Contains(file.Content, "\\documentclass"):
			return file, true, true
		case fallback == nil:
			fallback = &files[i]
		}
	}

	if fallback != nil {
		return *fallback, false, true
	}

	return FileEntry{}, false, false
}

func (s *compileSession) attachCachedTempDir(cache *CompilationCache) {
	if s.projectID == "" {
		return
	}

	entry, exists := cache.Get(s.projectID)
	if !exists || entry.TempDir == "" {
		return
	}

	if _, err := os.Stat(entry.TempDir); err != nil {
		log.Printf("[%s] Cached temp dir %s unavailable: %v", s.compiler.RequestID, entry.TempDir, err)
		return
	}

	log.Printf("[%s] Using cached temp directory: %s", s.compiler.RequestID, entry.TempDir)
	s.tempDir = entry.TempDir
	s.isIncremental = true
	s.shouldCleanup = false

	s.fileChanges = diffFiles(s.files, entry.FileHashes)
	changeCount := len(s.fileChanges.Added) + len(s.fileChanges.Modified) + len(s.fileChanges.Deleted)
	log.Printf("[%s] File changes: %d added, %d modified, %d deleted (total: %d)",
		s.compiler.RequestID, len(s.fileChanges.Added), len(s.fileChanges.Modified), len(s.fileChanges.Deleted), changeCount)
	log.Printf("[%s] Change types: tex=%v bib=%v assets=%v",
		s.compiler.RequestID, s.fileChanges.HasTexChanges, s.fileChanges.HasBibChanges, s.fileChanges.HasAssetChanges)
}

func (s *compileSession) ensureTempDir() *CompileResult {
	if s.tempDir != "" {
		return nil
	}

	dir, err := os.MkdirTemp("", "latex-*")
	if err != nil {
		return s.compiler.errorResult(s.metadata, fmt.Sprintf("Failed to create temp directory: %v", err), s.queueMs, s.receivedAt)
	}

	s.tempDir = dir
	log.Printf("[%s] Created new temp directory: %s", s.compiler.RequestID, s.tempDir)

	if s.projectID != "" {
		s.shouldCleanup = false
		log.Printf("[%s] Temp directory will be cached for project: %s", s.compiler.RequestID, s.projectID)
	}

	return nil
}

func (s *compileSession) resolveMainFilePaths() *CompileResult {
	if s.mainFilePath == "" {
		return s.compiler.errorResult(s.metadata, "No LaTeX source (.tex) file found in request", s.queueMs, s.receivedAt)
	}

	texPath := filepath.Join(s.tempDir, filepath.FromSlash(s.mainFilePath))
	s.texFilePath = texPath
	jobName := strings.TrimSuffix(filepath.Base(texPath), filepath.Ext(texPath))
	s.pdfPath = filepath.Join(s.tempDir, fmt.Sprintf("%s.pdf", jobName))
	s.logPath = filepath.Join(s.tempDir, fmt.Sprintf("%s.log", jobName))
	s.jobName = jobName

	return nil
}

func (s *compileSession) syncFilesToWorkspace() *CompileResult {
	switch {
	case s.isIncremental && s.fileChanges != nil:
		if err := updateCachedFiles(s.tempDir, s.fileChanges); err != nil {
			return s.compiler.errorResult(s.metadata, fmt.Sprintf("Failed to update files: %v", err), s.queueMs, s.receivedAt)
		}
		log.Printf("[%s] Incremental update: wrote %d changed files", s.compiler.RequestID,
			len(s.fileChanges.Added)+len(s.fileChanges.Modified)+len(s.fileChanges.Deleted))
		return nil
	default:
		if err := createFileStructure(s.tempDir, s.files); err != nil {
			return s.compiler.errorResult(s.metadata, fmt.Sprintf("Failed to write files: %v", err), s.queueMs, s.receivedAt)
		}
		log.Printf("[%s] Project structure written to: %s", s.compiler.RequestID, s.tempDir)
		return nil
	}
}

func (s *compileSession) tryServeCachedPDF(cache *CompilationCache) *CompileResult {
	if s.projectID == "" {
		return nil
	}

	contentHash := HashFileSet(s.files)
	if !cache.CheckContentHash(s.projectID, contentHash) {
		return nil
	}

	entry, _ := cache.Get(s.projectID)
	if entry == nil || len(entry.LastPDFData) == 0 {
		return nil
	}

	log.Printf("[%s] CACHE HIT: Content unchanged, returning cached PDF", s.compiler.RequestID)
	completedAt := time.Now()
	durationMs := completedAt.Sub(s.receivedAt).Milliseconds()

	return &CompileResult{
		RequestID:  s.compiler.RequestID,
		Success:    true,
		PDFData:    entry.LastPDFData,
		SHA256:     entry.LastSHA256,
		QueueMs:    s.queueMs,
		DurationMs: durationMs,
		PDFSize:    len(entry.LastPDFData),
		CacheHit:   true,
	}
}

func (s *compileSession) prepareWorkspace(cache *CompilationCache) *CompileResult {
	s.attachCachedTempDir(cache)

	if result := s.ensureTempDir(); result != nil {
		return result
	}

	if result := s.resolveMainFilePaths(); result != nil {
		return result
	}

	if result := s.syncFilesToWorkspace(); result != nil {
		return result
	}

	s.metadata.Status = "written"
	s.compiler.persistMetadata(s.metadata)
	log.Printf("[%s] TeX content written to: %s", s.compiler.RequestID, s.texFilePath)

	return nil
}

func (s *compileSession) determineStrategy() (bool, bool) {
	needsBib := needsBibliography(s.mainContent, s.files)
	needsMultiPass := needsMultiplePasses(s.mainContent)

	if needsBib {
		s.bibTool = detectBibliographyTool(s.mainContent, s.files)
		if s.bibTool == bibliographyToolNone {
			s.bibTool = bibliographyToolBibtex
		}
	} else {
		s.bibTool = bibliographyToolNone
	}

	if s.isIncremental && s.fileChanges != nil {
		needsBib, needsMultiPass = s.adjustStrategyForIncremental(needsBib, needsMultiPass)
	}

	if !needsBib {
		s.bibTool = bibliographyToolNone
	}

	log.Printf("[%s] Compilation strategy - Bibliography: %v (%s), Multi-pass: %v, Incremental: %v",
		s.compiler.RequestID, needsBib, s.bibTool.String(), needsMultiPass, s.isIncremental)
	return needsBib, needsMultiPass
}

func (s *compileSession) adjustStrategyForIncremental(needsBib bool, needsMultiPass bool) (bool, bool) {
	changes := s.fileChanges
	if changes == nil {
		return needsBib, needsMultiPass
	}

	// When bibliography files are unchanged we can often skip rerunning the bibliography tool.
	if !changes.HasBibChanges {
		switch {
		case changes.HasTexChanges && !changes.HasAssetChanges:
			log.Printf("[%s] INCREMENTAL: Only .tex changed, skipping bibliography processor (reusing previous output)", s.compiler.RequestID)
			return false, needsMultiPass
		case !changes.HasTexChanges && changes.HasAssetChanges:
			log.Printf("[%s] INCREMENTAL: Only assets changed, single pass", s.compiler.RequestID)
			return false, false
		case changes.HasTexChanges && changes.HasAssetChanges:
			log.Printf("[%s] INCREMENTAL: .tex + assets changed, skipping bibliography processor", s.compiler.RequestID)
			return false, needsMultiPass
		case !changes.HasTexChanges && !changes.HasAssetChanges:
			log.Printf("[%s] INCREMENTAL: No changes detected", s.compiler.RequestID)
			return false, false
		default:
			return needsBib, needsMultiPass
		}
	}

	if !changes.HasTexChanges {
		log.Printf("[%s] INCREMENTAL: Only .bib/assets changed (could skip first pdflatex)", s.compiler.RequestID)
		return true, needsMultiPass
	}

	return needsBib, needsMultiPass
}

func (s *compileSession) runCompilation(needsBib, needsMultiPass bool) {
	s.exitCode = 0

	if needsBib {
		log.Printf("[%s] Running full bibliography pipeline using %s", s.compiler.RequestID, s.bibTool.String())

		s.recordExitCode(s.compiler.runPdflatex(s.tempDir, s.texFilePath, &s.stdout, &s.stderr))

		if s.exitCode == 0 {
			s.runBibliographyProcessor()

			if s.exitCode == 0 {
				log.Printf("[%s] Running pdflatex (pass 2/3)...", s.compiler.RequestID)
				s.recordExitCode(s.compiler.runPdflatex(s.tempDir, s.texFilePath, &s.stdout, &s.stderr))

				if s.exitCode == 0 {
					log.Printf("[%s] Running pdflatex (pass 3/3)...", s.compiler.RequestID)
					s.recordExitCode(s.compiler.runPdflatex(s.tempDir, s.texFilePath, &s.stdout, &s.stderr))
				}
			}
		}
	} else if needsMultiPass {
		log.Printf("[%s] Running two-pass compilation for cross-references", s.compiler.RequestID)

		s.recordExitCode(s.compiler.runPdflatex(s.tempDir, s.texFilePath, &s.stdout, &s.stderr))

		if s.exitCode == 0 {
			log.Printf("[%s] Running pdflatex (pass 2/2)...", s.compiler.RequestID)
			s.recordExitCode(s.compiler.runPdflatex(s.tempDir, s.texFilePath, &s.stdout, &s.stderr))
		}
	} else {
		log.Printf("[%s] Running single-pass compilation", s.compiler.RequestID)
		s.recordExitCode(s.compiler.runPdflatex(s.tempDir, s.texFilePath, &s.stdout, &s.stderr))
	}
}

func (s *compileSession) runBibliographyProcessor() {
	cmdName := "bibtex"
	switch s.bibTool {
	case bibliographyToolBiber:
		cmdName = "biber"
	case bibliographyToolBibtex:
		cmdName = "bibtex"
	default:
		cmdName = "bibtex"
	}

	log.Printf("[%s] Running %s...", s.compiler.RequestID, cmdName)
	cmd := exec.Command(cmdName, s.jobName)
	cmd.Dir = s.tempDir
	cmd.Stdout = &s.stdout
	cmd.Stderr = &s.stderr

	if err := cmd.Run(); err != nil {
		log.Printf("[%s] %s exited with error: %v", s.compiler.RequestID, cmdName, err)
		s.recordExitCode(err)
	} else {
		log.Printf("[%s] %s completed successfully", s.compiler.RequestID, cmdName)
	}
}

func (s *compileSession) recordExitCode(err error) {
	if err == nil {
		return
	}

	if exitError, ok := err.(*exec.ExitError); ok {
		s.exitCode = exitError.ExitCode()
	} else {
		s.exitCode = -1
	}
}

func (s *compileSession) finalize(cache *CompilationCache) *CompileResult {
	completedAt := time.Now()
	durationMs := completedAt.Sub(s.receivedAt).Milliseconds()

	s.metadata.CompletedAt = completedAt
	s.metadata.DurationMs = durationMs
	s.metadata.ExitCode = s.exitCode
	s.metadata.StdoutBytes = s.stdout.Len()
	s.metadata.StderrBytes = s.stderr.Len()

	log.Printf("[%s] Compilation completed with exit code: %d", s.compiler.RequestID, s.exitCode)
	log.Printf("[%s] Total stdout length: %d bytes", s.compiler.RequestID, s.stdout.Len())
	log.Printf("[%s] Total stderr length: %d bytes", s.compiler.RequestID, s.stderr.Len())

	if pdfData, err := os.ReadFile(s.pdfPath); err == nil {
		log.Printf("[%s] PDF created successfully: %d bytes", s.compiler.RequestID, len(pdfData))

		if len(pdfData) < 4 || string(pdfData[:4]) != "%PDF" {
			return s.compiler.errorResult(s.metadata, "Invalid PDF format", s.queueMs, s.receivedAt)
		}

		hash := sha256.Sum256(pdfData)
		sha256Hex := hex.EncodeToString(hash[:])

		logContent := ""
		if logData, err := os.ReadFile(s.logPath); err == nil {
			logContent = string(logData)
		}

		s.metadata.LogTail = tailLines(truncateText(logContent, MaxLogChars), LogTailLines)

		if s.exitCode != 0 {
			errMsg := fmt.Sprintf("LaTeX toolchain exited with code %d", s.exitCode)
			log.Printf("[%s] Compilation produced PDF but exited with code %d", s.compiler.RequestID, s.exitCode)
			s.metadata.Status = "error"
			s.metadata.Error = errMsg
			s.compiler.persistMetadata(s.metadata)

			return &CompileResult{
				RequestID:    s.compiler.RequestID,
				Success:      false,
				ErrorMessage: errMsg,
				Stdout:       truncateText(s.stdout.String(), MaxLogChars),
				Stderr:       truncateText(s.stderr.String(), MaxLogChars),
				LogTail:      s.metadata.LogTail,
				QueueMs:      s.queueMs,
				DurationMs:   durationMs,
			}
		}

		s.metadata.Status = "success"
		s.metadata.PDFSize = len(pdfData)
		s.metadata.SHA256 = sha256Hex
		s.compiler.persistMetadata(s.metadata)

		if s.projectID != "" {
			contentHash := HashFileSet(s.files)
			fileHashes := buildFileHashMap(s.files)

			cacheEntry := &CacheEntry{
				ProjectID:      s.projectID,
				TempDir:        s.tempDir,
				FileHashes:     fileHashes,
				ContentHash:    contentHash,
				LastPDFData:    pdfData,
				LastSHA256:     sha256Hex,
				LastAccessTime: time.Now(),
			}

			cache.Set(s.projectID, cacheEntry)
			log.Printf("[%s] Cached compilation result for project %s", s.compiler.RequestID, s.projectID)
		}

		log.Printf("[%s] Compilation successful", s.compiler.RequestID)

		return &CompileResult{
			RequestID:  s.compiler.RequestID,
			Success:    true,
			PDFData:    pdfData,
			SHA256:     sha256Hex,
			QueueMs:    s.queueMs,
			DurationMs: durationMs,
			PDFSize:    len(pdfData),
			CacheHit:   false,
		}
	}

	logContent := ""
	if logData, err := os.ReadFile(s.logPath); err == nil {
		logContent = string(logData)
		log.Printf("[%s] LaTeX log excerpt: %s", s.compiler.RequestID, logContent[:min(500, len(logContent))])
	}

	s.metadata.Status = "error"
	s.metadata.LogTail = tailLines(logContent, LogTailLines)
	s.compiler.persistMetadata(s.metadata)

	return &CompileResult{
		RequestID:    s.compiler.RequestID,
		Success:      false,
		ErrorMessage: "PDF file not generated",
		Stdout:       truncateText(s.stdout.String(), MaxLogChars),
		Stderr:       truncateText(s.stderr.String(), MaxLogChars),
		LogTail:      s.metadata.LogTail,
		QueueMs:      s.queueMs,
		DurationMs:   durationMs,
	}
}

func (s *compileSession) cleanup() {
	if s.shouldCleanup && s.tempDir != "" {
		_ = os.RemoveAll(s.tempDir)
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
	// TODO: Support alternative engines / aux tools (xelatex, latexmk, makeindex, shell-escape, etc.);
	// documents that rely on those currently fail to compile here.
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
