package internal

import (
	"fmt"
	"net/http"
	"os"
	"time"

	"github.com/gin-gonic/gin"
)

var requestQueue chan *CompileJob

// SetRequestQueue sets the queue for compilation jobs
func SetRequestQueue(queue chan *CompileJob) {
	requestQueue = queue
}

// HealthHandler handles health check requests
func HealthHandler(c *gin.Context) {
	c.JSON(http.StatusOK, HealthResponse{
		Status:        "ok",
		QueueLength:   len(requestQueue),
		QueueCapacity: cap(requestQueue),
		Timestamp:     time.Now().Format(time.RFC3339),
	})
}

// CompileHandler handles LaTeX compilation requests
func CompileHandler(c *gin.Context) {
	var req CompileRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, ErrorResponse{
			Error:   "Invalid request",
			Message: "Could not parse JSON payload",
		})
		return
	}

	if req.ProjectID == "" {
		c.JSON(http.StatusBadRequest, ErrorResponse{
			Error:   "Invalid request",
			Message: "projectId is required",
		})
		return
	}

	supabaseURL := os.Getenv("SUPABASE_URL")
	supabaseKey := os.Getenv("SUPABASE_KEY")

	if supabaseURL == "" || supabaseKey == "" {
		c.JSON(http.StatusInternalServerError, ErrorResponse{
			Error:   "Configuration error",
			Message: "Supabase credentials not configured",
		})
		return
	}

	files, err := FetchFilesFromSupabase(req.ProjectID, supabaseURL, supabaseKey)
	if err != nil {
		c.JSON(http.StatusInternalServerError, ErrorResponse{
			Error:   "Storage error",
			Message: fmt.Sprintf("Failed to fetch files from Supabase: %v", err),
		})
		return
	}

	if len(files) == 0 {
		c.JSON(http.StatusBadRequest, ErrorResponse{
			Error:   "Invalid request",
			Message: fmt.Sprintf("No files found for project %s in Supabase Storage", req.ProjectID),
		})
		return
	}

	// Check queue capacity
	if len(requestQueue) >= cap(requestQueue) {
		c.JSON(http.StatusServiceUnavailable, gin.H{
			"error":         "Server busy",
			"message":       "Too many compilation requests. Please try again in a moment.",
			"queuePosition": len(requestQueue) + 1,
		})
		return
	}

	// Create job with result channel
	job := &CompileJob{
		Context:          c,
		Files:            files,
		ProjectID:        req.ProjectID,
		LastModifiedFile: req.LastModifiedFile,
		EnqueuedAt:       time.Now(),
		ResultChan:       make(chan *CompileResult, 1),
	}

	// Add to queue (non-blocking with timeout)
	select {
	case requestQueue <- job:
		// Wait for worker to send result back
		result := <-job.ResultChan

		// Set custom headers
		c.Header("X-Compile-Request-Id", result.RequestID)
		c.Header("X-Compile-Duration-Ms", fmt.Sprintf("%d", result.DurationMs))
		c.Header("X-Compile-Queue-Ms", fmt.Sprintf("%d", result.QueueMs))

		// Send response based on result
		if result.Success {
			c.Header("X-Compile-Sha256", result.SHA256)
			c.Header("Content-Type", "application/pdf")
			c.Header("Content-Length", fmt.Sprintf("%d", len(result.PDFData)))
			c.Header("Content-Disposition", "attachment; filename=\"compiled.pdf\"")
			c.Data(http.StatusOK, "application/pdf", result.PDFData)
		} else {
			c.JSON(http.StatusInternalServerError, ErrorResponse{
				Error:      "LaTeX compilation failed",
				Message:    result.ErrorMessage,
				RequestID:  result.RequestID,
				QueueMs:    result.QueueMs,
				DurationMs: result.DurationMs,
				Stdout:     result.Stdout,
				Stderr:     result.Stderr,
				Log:        result.LogTail,
			})
		}
	case <-time.After(5 * time.Second):
		c.JSON(http.StatusServiceUnavailable, ErrorResponse{
			Error:   "Server busy",
			Message: "Could not enqueue request, timeout",
		})
	}
}

// HandleCompilation processes a compilation job
func HandleCompilation(job *CompileJob) {
	defer func() {
		if r := recover(); r != nil {
			fmt.Printf("Recovered from panic in compilation: %v\n", r)
			// Send error result back through channel
			job.ResultChan <- &CompileResult{
				Success:      false,
				ErrorMessage: fmt.Sprintf("Internal server error: %v", r),
			}
		}
	}()

	comp := New()
	result := comp.Compile(job.Files, job.EnqueuedAt, job.ProjectID)

	// Send result back to handler through channel
	job.ResultChan <- result
}
