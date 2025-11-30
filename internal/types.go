package internal

import "time"

// FileEntry represents a single file in a multi-file project
type FileEntry struct {
	Path     string `json:"path"`
	Content  string `json:"content"`            // Text content (for .tex, .sty, etc.)
	Encoding string `json:"encoding,omitempty"` // "base64" for binary files, empty for text
}

// CompileRequest represents the incoming compilation request
type CompileRequest struct {
	Files            []FileEntry `json:"files"`
	ProjectID        string      `json:"projectId,omitempty"`
	LastModifiedFile string      `json:"lastModifiedFile,omitempty"`
}

// CompileJob represents a queued compilation job
type CompileJob struct {
	Context          interface{} // Will be *gin.Context
	Files            []FileEntry // Multi-file content
	ProjectID        string      // Project identifier for caching
	LastModifiedFile string      // Hint for which file changed
	EnqueuedAt       time.Time
	ResultChan       chan *CompileResult // Channel to send result back to handler
}

// CompileMetadata tracks compilation metadata for logging
type compileMetadata struct {
	RequestID   string    `json:"requestId"`
	EnqueuedAt  time.Time `json:"enqueuedAt"`
	ReceivedAt  time.Time `json:"receivedAt"`
	CompletedAt time.Time `json:"completedAt,omitempty"`
	QueueMs     int64     `json:"queueMs"`
	DurationMs  int64     `json:"durationMs"`
	Status      string    `json:"status"`
	ExitCode    int       `json:"exitCode,omitempty"`
	PDFSize     int       `json:"pdfSize,omitempty"`
	SHA256      string    `json:"sha256,omitempty"`
	StdoutBytes int       `json:"stdoutBytes,omitempty"`
	StderrBytes int       `json:"stderrBytes,omitempty"`
	LogTail     string    `json:"logTail,omitempty"`
	Error       string    `json:"error,omitempty"`
	Engine      string    `json:"engine,omitempty"`
}

// CompileResult holds the result of a compilation
type CompileResult struct {
	RequestID    string
	Success      bool
	PDFData      []byte
	SyncTexData  []byte // .synctex.gz file contents for source-PDF synchronization
	SHA256       string
	ErrorMessage string
	Stdout       string
	Stderr       string
	LogTail      string
	QueueMs      int64
	DurationMs   int64
	PDFSize      int
	CacheHit     bool // Whether result was served from cache
}

// HealthResponse represents the health check response
type HealthResponse struct {
	Status        string `json:"status"`
	QueueLength   int    `json:"queueLength"`
	QueueCapacity int    `json:"queueCapacity"`
	Timestamp     string `json:"timestamp"`
}

// ErrorResponse represents an error response
type ErrorResponse struct {
	Error      string `json:"error"`
	Message    string `json:"message,omitempty"`
	RequestID  string `json:"requestId,omitempty"`
	QueueMs    int64  `json:"queueMs,omitempty"`
	DurationMs int64  `json:"durationMs,omitempty"`
	Stdout     string `json:"stdout,omitempty"`
	Stderr     string `json:"stderr,omitempty"`
	Log        string `json:"log,omitempty"`
}

// LintRequest represents a request to lint LaTeX files
type LintRequest struct {
	Files []FileEntry `json:"files"`
}

// LintWarning represents a single chktex warning
type LintWarning struct {
	File     string `json:"file"`
	Line     int    `json:"line"`
	Column   int    `json:"column"`
	Severity string `json:"severity"` // "warning" or "error"
	Code     int    `json:"code"`
	Message  string `json:"message"`
}

// LintResponse represents the response from the lint endpoint
type LintResponse struct {
	Success    bool          `json:"success"`
	Warnings   []LintWarning `json:"warnings"`
	Messages   string        `json:"messages"` // Human-readable summary of all warnings/errors
	ErrorCount int           `json:"errorCount"`
	WarnCount  int           `json:"warnCount"`
	RawOutput  string        `json:"rawOutput,omitempty"`
}

// WordCountRequest represents a request to count words in LaTeX files
type WordCountRequest struct {
	Files []FileEntry `json:"files"`
}

// WordCountResponse represents the response from the word-count endpoint
type WordCountResponse struct {
	Success   bool            `json:"success"`
	Total     WordCountStats  `json:"total"`
	ByFile    []FileWordCount `json:"byFile,omitempty"`
	Summary   string          `json:"summary"` // Human-readable summary
	RawOutput string          `json:"rawOutput,omitempty"`
}

// WordCountStats holds word count statistics
type WordCountStats struct {
	Words       int `json:"words"`
	Headers     int `json:"headers"`
	Captions    int `json:"captions"`
	MathInline  int `json:"mathInline"`
	MathDisplay int `json:"mathDisplay"`
}

// FileWordCount holds word count for a specific file
type FileWordCount struct {
	File  string         `json:"file"`
	Stats WordCountStats `json:"stats"`
}

// SyncTexRequest represents a request for SyncTeX synchronization
type SyncTexRequest struct {
	// For forward sync (source → PDF): provide file, line, column
	// For backward sync (PDF → source): provide page, x, y
	Direction string `json:"direction"` // "forward" or "backward"

	// Forward sync parameters (source to PDF)
	File   string `json:"file,omitempty"`   // Source file path
	Line   int    `json:"line,omitempty"`   // 1-based line number
	Column int    `json:"column,omitempty"` // 1-based column number (0 if unknown)

	// Backward sync parameters (PDF to source)
	Page int     `json:"page,omitempty"` // 1-based page number
	X    float64 `json:"x,omitempty"`    // X coordinate from top-left (in points, 72 dpi)
	Y    float64 `json:"y,omitempty"`    // Y coordinate from top-left (in points, 72 dpi)

	// SyncTeX data (base64 encoded .synctex.gz content)
	SyncTexData string `json:"synctexData"`

	// PDF filename (for synctex to locate the .synctex.gz)
	PDFName string `json:"pdfName,omitempty"`
}

// SyncTexResponse represents the response from SyncTeX synchronization
type SyncTexResponse struct {
	Success bool   `json:"success"`
	Error   string `json:"error,omitempty"`

	// Forward sync result (source → PDF)
	Page   int     `json:"page,omitempty"`   // 1-based page number
	X      float64 `json:"x,omitempty"`      // X coordinate
	Y      float64 `json:"y,omitempty"`      // Y coordinate
	Width  float64 `json:"width,omitempty"`  // Width of the box
	Height float64 `json:"height,omitempty"` // Height of the box

	// Backward sync result (PDF → source)
	File   string `json:"file,omitempty"`   // Source file path
	Line   int    `json:"line,omitempty"`   // 1-based line number
	Column int    `json:"column,omitempty"` // Column number (-1 if unknown)

	// Raw output for debugging
	RawOutput string `json:"rawOutput,omitempty"`
}
