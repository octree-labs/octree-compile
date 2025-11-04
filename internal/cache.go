package internal

import (
	"crypto/sha256"
	"encoding/hex"
	"log"
	"os"
	"sync"
	"time"
)

const (
	CacheExpirationTime = 30 * time.Minute // Evict after 30 minutes of inactivity
	MaxCachedProjects   = 15               // Maximum number of projects to cache
	CleanupInterval     = 60 * time.Minute // Run cleanup every 60 minutes
)

// CacheEntry represents a cached compilation for a project
type CacheEntry struct {
	ProjectID      string
	TempDir        string
	FileHashes     map[string]string // path -> hash
	ContentHash    string            // Hash of all file content
	LastPDFData    []byte
	LastSHA256     string
	LastAccessTime time.Time
	mutex          sync.Mutex // Lock for this cache entry
}

// CompilationCache manages cached compilation directories
type CompilationCache struct {
	entries      map[string]*CacheEntry // projectID -> CacheEntry
	projectLocks map[string]*sync.Mutex // projectID -> lock for serializing requests
	globalMutex  sync.RWMutex           // Protects the maps
}

var globalCache *CompilationCache
var cacheOnce sync.Once

// GetCache returns the global cache instance
func GetCache() *CompilationCache {
	cacheOnce.Do(func() {
		globalCache = &CompilationCache{
			entries:      make(map[string]*CacheEntry),
			projectLocks: make(map[string]*sync.Mutex),
		}
		// Start cleanup goroutine
		go globalCache.cleanupLoop()
	})
	return globalCache
}

// LockProject acquires a lock for the given project to serialize compilations
func (c *CompilationCache) LockProject(projectID string) {
	if projectID == "" {
		return
	}

	c.globalMutex.Lock()
	if _, exists := c.projectLocks[projectID]; !exists {
		c.projectLocks[projectID] = &sync.Mutex{}
	}
	lock := c.projectLocks[projectID]
	c.globalMutex.Unlock()

	lock.Lock()
}

// UnlockProject releases the lock for the given project
func (c *CompilationCache) UnlockProject(projectID string) {
	if projectID == "" {
		return
	}

	c.globalMutex.RLock()
	if lock, exists := c.projectLocks[projectID]; exists {
		lock.Unlock()
	}
	c.globalMutex.RUnlock()
}

// Get retrieves a cache entry for the given project
func (c *CompilationCache) Get(projectID string) (*CacheEntry, bool) {
	if projectID == "" {
		return nil, false
	}

	c.globalMutex.RLock()
	entry, exists := c.entries[projectID]
	if exists {
		entry.mutex.Lock()
		entry.LastAccessTime = time.Now()
		entry.mutex.Unlock()
	}
	c.globalMutex.RUnlock()

	return entry, exists
}

// Set stores or updates a cache entry for the given project
func (c *CompilationCache) Set(projectID string, entry *CacheEntry) {
	if projectID == "" {
		return
	}

	entry.LastAccessTime = time.Now()

	c.globalMutex.Lock()
	defer c.globalMutex.Unlock()

	// Check if we need to evict (LRU)
	if len(c.entries) >= MaxCachedProjects {
		// Don't evict if we're updating an existing entry
		if _, exists := c.entries[projectID]; !exists {
			c.evictOldestLocked()
		}
	}

	c.entries[projectID] = entry
}

// CheckContentHash checks if the content hash matches the cached hash
func (c *CompilationCache) CheckContentHash(projectID, contentHash string) bool {
	entry, exists := c.Get(projectID)
	if !exists {
		return false
	}

	entry.mutex.Lock()
	defer entry.mutex.Unlock()

	return entry.ContentHash == contentHash
}

// evictOldestLocked evicts the oldest cache entry (must be called with globalMutex held)
func (c *CompilationCache) evictOldestLocked() {
	var oldestID string
	var oldestTime time.Time

	for id, entry := range c.entries {
		entry.mutex.Lock()
		accessTime := entry.LastAccessTime
		entry.mutex.Unlock()

		if oldestID == "" || accessTime.Before(oldestTime) {
			oldestID = id
			oldestTime = accessTime
		}
	}

	if oldestID != "" {
		c.removeEntryLocked(oldestID)
		log.Printf("[CACHE] Evicted oldest entry: %s (LRU)", oldestID)
	}
}

// removeEntryLocked removes a cache entry and cleans up resources (must be called with globalMutex held)
func (c *CompilationCache) removeEntryLocked(projectID string) {
	if entry, exists := c.entries[projectID]; exists {
		// Clean up temp directory
		entry.mutex.Lock()
		tempDir := entry.TempDir
		entry.mutex.Unlock()

		if tempDir != "" {
			if err := os.RemoveAll(tempDir); err != nil {
				log.Printf("[CACHE] Failed to remove temp dir %s: %v", tempDir, err)
			} else {
				log.Printf("[CACHE] Cleaned up temp dir: %s", tempDir)
			}
		}

		delete(c.entries, projectID)
	}

	// Clean up lock
	delete(c.projectLocks, projectID)
}

// cleanupLoop runs periodically to evict expired cache entries
func (c *CompilationCache) cleanupLoop() {
	ticker := time.NewTicker(CleanupInterval)
	defer ticker.Stop()

	for range ticker.C {
		c.cleanup()
	}
}

// cleanup evicts expired cache entries
func (c *CompilationCache) cleanup() {
	c.globalMutex.Lock()
	defer c.globalMutex.Unlock()

	now := time.Now()
	var toRemove []string

	for id, entry := range c.entries {
		entry.mutex.Lock()
		lastAccess := entry.LastAccessTime
		entry.mutex.Unlock()

		if now.Sub(lastAccess) > CacheExpirationTime {
			toRemove = append(toRemove, id)
		}
	}

	for _, id := range toRemove {
		c.removeEntryLocked(id)
		log.Printf("[CACHE] Evicted expired entry: %s (30min timeout)", id)
	}

	if len(toRemove) > 0 {
		log.Printf("[CACHE] Cleanup completed: %d entries evicted, %d entries remain", len(toRemove), len(c.entries))
	}
}

// Stats returns cache statistics
func (c *CompilationCache) Stats() map[string]interface{} {
	c.globalMutex.RLock()
	defer c.globalMutex.RUnlock()

	return map[string]interface{}{
		"entries":           len(c.entries),
		"maxEntries":        MaxCachedProjects,
		"expirationMinutes": int(CacheExpirationTime.Minutes()),
	}
}

// HashFileContent generates a SHA256 hash of file content
func HashFileContent(content string) string {
	hash := sha256.Sum256([]byte(content))
	return hex.EncodeToString(hash[:])
}

// HashFileSet generates a SHA256 hash of all files in the set
func HashFileSet(files []FileEntry) string {
	hasher := sha256.New()

	for _, file := range files {
		// Include path and content in hash
		hasher.Write([]byte(file.Path))
		hasher.Write([]byte{0}) // Separator
		hasher.Write([]byte(file.Content))
		hasher.Write([]byte{0}) // Separator
	}

	return hex.EncodeToString(hasher.Sum(nil))
}

// CacheSession encapsulates a project-scoped cache interaction.
// It guarantees serialization via project-level locking and exposes
// helper methods for serving cached results, preparing incremental
// compilations, and persisting fresh compilation artefacts.
type CacheSession struct {
	cache     *CompilationCache
	projectID string
	locked    bool
}

// AcquireCacheSession obtains a cache session for the given project.
// The caller MUST call Release() when finished.
func AcquireCacheSession(projectID string) *CacheSession {
	if projectID == "" {
		return nil
	}

	cache := GetCache()
	cache.LockProject(projectID)

	return &CacheSession{
		cache:     cache,
		projectID: projectID,
		locked:    true,
	}
}

// Release releases the project lock held by the session.
func (s *CacheSession) Release() {
	if s == nil || !s.locked {
		return
	}

	s.cache.UnlockProject(s.projectID)
	s.locked = false
}

// TryServeCachedResult returns a cached compile result if the provided
// file set matches the cached content hash. Returns nil on cache miss.
func (s *CacheSession) TryServeCachedResult(files []FileEntry, requestID string, enqueuedAt time.Time) *CompileResult {
	if s == nil || len(files) == 0 {
		return nil
	}

	contentHash := HashFileSet(files)
	if !s.cache.CheckContentHash(s.projectID, contentHash) {
		log.Printf("[%s] Cache miss for project %s - proceeding with compilation", requestID, s.projectID)
		return nil
	}

	entry, exists := s.cache.Get(s.projectID)
	if !exists || entry == nil || len(entry.LastPDFData) == 0 {
		log.Printf("[%s] Cache hash matched but data unavailable for project %s", requestID, s.projectID)
		return nil
	}

	receivedAt := time.Now()
	queueMs := receivedAt.Sub(enqueuedAt).Milliseconds()
	completedAt := time.Now()
	durationMs := completedAt.Sub(receivedAt).Milliseconds()

	log.Printf("[%s] 🚀 UNIVERSAL CACHE HIT: returning cached PDF (%d bytes, %dms)", requestID, len(entry.LastPDFData), durationMs)

	return &CompileResult{
		RequestID:  requestID,
		Success:    true,
		PDFData:    entry.LastPDFData,
		SHA256:     entry.LastSHA256,
		QueueMs:    queueMs,
		DurationMs: durationMs,
		PDFSize:    len(entry.LastPDFData),
		CacheHit:   true,
	}
}

// PrepareIncrementalWorkspace returns details required to perform an
// incremental TeX Live compilation if a cached temp directory exists.
// It returns the temp directory, file change summary, and whether the
// invocation is incremental. On miss, the zero values are returned.
func (s *CacheSession) PrepareIncrementalWorkspace(files []FileEntry, requestID string) (string, *FileChanges, bool) {
	if s == nil || len(files) == 0 {
		return "", nil, false
	}

	entry, exists := s.cache.Get(s.projectID)
	if !exists || entry == nil || entry.TempDir == "" {
		return "", nil, false
	}

	if _, err := os.Stat(entry.TempDir); err != nil {
		log.Printf("[%s] Cached temp dir %s unavailable (%v) -- creating new workspace", requestID, entry.TempDir, err)
		return "", nil, false
	}

	log.Printf("[%s] Using cached temp directory: %s", requestID, entry.TempDir)
	changes := diffFiles(files, entry.FileHashes)
	changeCount := len(changes.Added) + len(changes.Modified) + len(changes.Deleted)
	log.Printf("[%s] File changes: %d added, %d modified, %d deleted (total: %d)",
		requestID, len(changes.Added), len(changes.Modified), len(changes.Deleted), changeCount)
	log.Printf("[%s] Change types: tex=%v bib=%v assets=%v",
		requestID, changes.HasTexChanges, changes.HasBibChanges, changes.HasAssetChanges)

	return entry.TempDir, changes, true
}

// StoreCompilation caches the successful compilation artefacts for future use.
// tempDir can be empty for engines that don't reuse workspaces (e.g. Tectonic).
func (s *CacheSession) StoreCompilation(files []FileEntry, tempDir string, pdfData []byte, sha256Hex string, requestID string, engine string) {
	if s == nil || len(files) == 0 || len(pdfData) == 0 || sha256Hex == "" {
		return
	}

	contentHash := HashFileSet(files)
	fileHashes := buildFileHashMap(files)

	entry := &CacheEntry{
		ProjectID:      s.projectID,
		TempDir:        tempDir,
		FileHashes:     fileHashes,
		ContentHash:    contentHash,
		LastPDFData:    pdfData,
		LastSHA256:     sha256Hex,
		LastAccessTime: time.Now(),
	}

	s.cache.Set(s.projectID, entry)
	if engine == "" {
		engine = "unknown"
	}
	log.Printf("[%s] ✅ Cached %s compilation result for project %s", requestID, engine, s.projectID)
}
