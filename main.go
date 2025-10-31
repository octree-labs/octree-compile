package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/octree/latex-compile/internal"
)

const (
	DefaultPort           = "3001"
	MaxConcurrentRequests = 2
	CompilationTimeout    = 30 * time.Second
	ShutdownTimeout       = 30 * time.Second
)

var requestQueue chan *internal.CompileJob

func main() {
	// Setup
	port := os.Getenv("PORT")
	if port == "" {
		port = DefaultPort
	}

	historyDir := os.Getenv("HISTORY_DIR")
	if historyDir == "" {
		historyDir = "./logs"
	}

	// Create history directory
	if err := os.MkdirAll(historyDir, 0755); err != nil {
		log.Printf("Warning: Failed to create history directory: %v", err)
	}

	// Set history dir for compiler
	internal.SetHistoryDir(historyDir)

	// Initialize request queue
	requestQueue = make(chan *internal.CompileJob, MaxConcurrentRequests*2)
	internal.SetRequestQueue(requestQueue)

	// Start workers
	for i := 0; i < MaxConcurrentRequests; i++ {
		go worker(i)
	}

	// Setup router
	router := setupRouter()

	// Create server
	srv := &http.Server{
		Addr:         ":" + port,
		Handler:      router,
		ReadTimeout:  60 * time.Second,
		WriteTimeout: 60 * time.Second,
		IdleTimeout:  120 * time.Second,
	}

	// Start server in goroutine
	go func() {
		log.Printf("LaTeX compilation server starting on port %s", port)
		log.Printf("Max concurrent requests: %d", MaxConcurrentRequests)
		log.Printf("Health check: http://localhost:%s/health", port)
		
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("Server failed to start: %v", err)
		}
	}()

	// Wait for interrupt signal
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	log.Println("Shutting down server...")

	// Graceful shutdown
	ctx, cancel := context.WithTimeout(context.Background(), ShutdownTimeout)
	defer cancel()

	if err := srv.Shutdown(ctx); err != nil {
		log.Fatal("Server forced to shutdown:", err)
	}

	log.Println("Server exited")
}

func setupRouter() *gin.Engine {
	// Set Gin mode
	if os.Getenv("GIN_MODE") == "" {
		gin.SetMode(gin.ReleaseMode)
	}

	router := gin.Default()

	// CORS middleware
	router.Use(corsMiddleware())

	// Routes
	router.GET("/health", internal.HealthHandler)
	router.POST("/compile", internal.CompileHandler)

	return router
}

func corsMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Writer.Header().Set("Access-Control-Allow-Origin", "*")
		c.Writer.Header().Set("Access-Control-Allow-Methods", "POST, GET, OPTIONS")
		c.Writer.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")
		
		if c.Request.Method == "OPTIONS" {
			c.AbortWithStatus(204)
			return
		}
		
		c.Next()
	}
}

func worker(id int) {
	log.Printf("Worker %d started", id)
	for job := range requestQueue {
		internal.HandleCompilation(job)
	}
	log.Printf("Worker %d stopped", id)
}

