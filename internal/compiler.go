package internal

import (
	"log"
	"strings"
	"time"

	"github.com/google/uuid"
)

const (
	MaxLogChars  = 5000
	LogTailLines = 80
)

var historyDir string

// SetHistoryDir sets the directory for compilation history logs.
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
	effectiveFiles := files
	if len(effectiveFiles) == 0 && content != "" {
		effectiveFiles = []FileEntry{{Path: "main.tex", Content: content}}
	}

	var cacheSession *CacheSession
	if projectID != "" && len(effectiveFiles) > 0 {
		cacheSession = AcquireCacheSession(projectID)
		if cacheSession != nil {
			defer cacheSession.Release()
			if cached := cacheSession.TryServeCachedResult(effectiveFiles, c.RequestID, enqueuedAt); cached != nil {
				return cached
			}
		}
	}

	decision := AnalyzeEngineRequirements(effectiveFiles)

	if decision.RequiresClassic {
		if len(decision.Reasons) > 0 {
			log.Printf("[%s] Engine classifier: routing to TeX Live (%s)", c.RequestID, strings.Join(decision.Reasons, "; "))
		} else {
			log.Printf("[%s] Engine classifier: routing to TeX Live (no reasons provided)", c.RequestID)
		}
		return c.compileWithTexlive(content, files, enqueuedAt, projectID, cacheSession)
	}

	if len(decision.Reasons) > 0 {
		log.Printf("[%s] Engine classifier: attempting Tectonic (notes: %s)", c.RequestID, strings.Join(decision.Reasons, "; "))
	} else {
		log.Printf("[%s] Engine classifier: attempting Tectonic", c.RequestID)
	}

	result := CompileWithTectonic(c.RequestID, effectiveFiles, enqueuedAt, projectID, cacheSession)
	if result.Success {
		return result
	}

	log.Printf("[%s] Tectonic failed (%s); falling back to TeX Live", c.RequestID, result.ErrorMessage)
	return c.compileWithTexlive(content, files, enqueuedAt, projectID, cacheSession)
}
