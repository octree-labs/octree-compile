package internal

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
)

const (
	tectonicBinaryEnv      = "TECTONIC_BINARY"
	tectonicTimeoutEnv     = "TECTONIC_TIMEOUT_SECONDS"
	defaultTectonicBinary  = "tectonic"
	defaultTectonicTimeout = 30 * time.Second
)

// CompileWithTectonic compiles a LaTeX payload using the Tectonic engine.
// It mirrors the behaviour of the pdflatex-based pipeline but skips the
// multi-pass heuristics and delegates the heavy lifting to Tectonic. Routing
// logic (choosing Tex Live vs. Tectonic) lives elsewhere; this function is a
// drop-in for experimentation and future TeXpresso-style incrementality.
func CompileWithTectonic(requestID string, files []FileEntry, enqueuedAt time.Time, projectID string, cacheSession *CacheSession) *CompileResult {
	if requestID == "" {
		requestID = uuid.New().String()
	}

	receivedAt := time.Now()
	queueMs := receivedAt.Sub(enqueuedAt).Milliseconds()

	log.Printf("[%s] ==== TECTONIC COMPILE REQUEST ====", requestID)
	if projectID != "" {
		log.Printf("[%s] ProjectID: %s", requestID, projectID)
	}

	if len(files) == 0 {
		return tectonicErrorResult(requestID, queueMs, receivedAt, "no files provided for Tectonic compilation")
	}

	tempDir, err := os.MkdirTemp("", "tectonic-*")
	if err != nil {
		return tectonicErrorResult(requestID, queueMs, receivedAt, fmt.Sprintf("failed to create temp directory: %v", err))
	}
	defer os.RemoveAll(tempDir)

	if err := createFileStructure(tempDir, files); err != nil {
		return tectonicErrorResult(requestID, queueMs, receivedAt, fmt.Sprintf("failed to write files: %v", err))
	}

	mainRelative := findPrimaryTex(files)
	mainPath := filepath.Join(tempDir, mainRelative)

	tectonicBin := os.Getenv(tectonicBinaryEnv)
	if tectonicBin == "" {
		tectonicBin = defaultTectonicBinary
	}

	timeout := resolveTectonicTimeout()
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	var stdout bytes.Buffer
	var stderr bytes.Buffer

	args := []string{
		"--synctex",
		"--keep-logs",
		"--keep-intermediates",
		"--outdir",
		tempDir,
		mainPath,
	}

	cmd := exec.CommandContext(ctx, tectonicBin, args...)
	cmd.Dir = tempDir
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	log.Printf("[%s] Running %s %s", requestID, tectonicBin, strings.Join(args, " "))

	runErr := cmd.Run()
	completedAt := time.Now()
	durationMs := completedAt.Sub(receivedAt).Milliseconds()

	logPath := filepath.Join(tempDir, strings.TrimSuffix(mainRelative, filepath.Ext(mainRelative))+".log")
	pdfPath := filepath.Join(tempDir, strings.TrimSuffix(mainRelative, filepath.Ext(mainRelative))+".pdf")

	pdfData, readErr := os.ReadFile(pdfPath)
	if runErr != nil || readErr != nil {
		errMsg := fmt.Sprintf("tectonic compilation failed: %v", firstNonNil(runErr, readErr))
		return &CompileResult{
			RequestID:    requestID,
			Success:      false,
			ErrorMessage: errMsg,
			Stdout:       truncateText(stdout.String(), MaxLogChars),
			Stderr:       truncateText(stderr.String(), MaxLogChars),
			LogTail:      readLogTail(logPath),
			QueueMs:      queueMs,
			DurationMs:   durationMs,
		}
	}

	if len(pdfData) < 4 || string(pdfData[:4]) != "%PDF" {
		return &CompileResult{
			RequestID:    requestID,
			Success:      false,
			ErrorMessage: "tectonic produced an invalid PDF payload",
			Stdout:       truncateText(stdout.String(), MaxLogChars),
			Stderr:       truncateText(stderr.String(), MaxLogChars),
			LogTail:      readLogTail(logPath),
			QueueMs:      queueMs,
			DurationMs:   durationMs,
		}
	}

	hash := sha256.Sum256(pdfData)
	sha256Hex := hex.EncodeToString(hash[:])

	log.Printf("[%s] Tectonic compilation successful (%d bytes, %dms)", requestID, len(pdfData), durationMs)

	// ==== CACHE WRITE: Store successful compilation ====
	if cacheSession != nil && len(files) > 0 {
		cacheSession.StoreCompilation(files, "", pdfData, sha256Hex, requestID, "tectonic")
	}

	return &CompileResult{
		RequestID:  requestID,
		Success:    true,
		PDFData:    pdfData,
		SHA256:     sha256Hex,
		QueueMs:    queueMs,
		DurationMs: durationMs,
		PDFSize:    len(pdfData),
		CacheHit:   false,
	}
}

func findPrimaryTex(files []FileEntry) string {
	for _, file := range files {
		if file.Path == "main.tex" {
			return file.Path
		}
	}
	for _, file := range files {
		if strings.HasSuffix(file.Path, ".tex") {
			return file.Path
		}
	}
	return "main.tex"
}

func resolveTectonicTimeout() time.Duration {
	raw := os.Getenv(tectonicTimeoutEnv)
	if raw == "" {
		return defaultTectonicTimeout
	}
	secs, err := strconv.Atoi(raw)
	if err != nil || secs <= 0 {
		log.Printf("[TECTONIC] Invalid timeout %q, falling back to default", raw)
		return defaultTectonicTimeout
	}
	return time.Duration(secs) * time.Second
}

func readLogTail(path string) string {
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	return tailLines(truncateText(string(data), MaxLogChars), LogTailLines)
}

func firstNonNil(errs ...error) error {
	for _, err := range errs {
		if err != nil {
			return err
		}
	}
	return nil
}

func tectonicErrorResult(requestID string, queueMs int64, receivedAt time.Time, message string) *CompileResult {
	duration := time.Since(receivedAt).Milliseconds()
	log.Printf("[%s] Tectonic error: %s", requestID, message)
	return &CompileResult{
		RequestID:    requestID,
		Success:      false,
		ErrorMessage: message,
		QueueMs:      queueMs,
		DurationMs:   duration,
	}
}
