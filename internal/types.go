package internal

import "time"

// CompileRequest represents the incoming compilation request
type CompileRequest struct {
	Content string `json:"content"`
}

// CompileJob represents a queued compilation job
type CompileJob struct {
	Context    interface{} // Will be *gin.Context
	Content    string
	EnqueuedAt time.Time
	ResultChan chan *CompileResult // Channel to send result back to handler
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
}

// CompileResult holds the result of a compilation
type CompileResult struct {
	RequestID    string
	Success      bool
	PDFData      []byte
	SHA256       string
	ErrorMessage string
	Stdout       string
	Stderr       string
	LogTail      string
	QueueMs      int64
	DurationMs   int64
	PDFSize      int
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

