package internal

import (
	"encoding/base64"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
)

// SyncTexHandler handles SyncTeX synchronization requests
func SyncTexHandler(c *gin.Context) {
	var req SyncTexRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, ErrorResponse{
			Error:   "Invalid request",
			Message: "Could not parse JSON payload",
		})
		return
	}

	// Validate direction
	if req.Direction != "forward" && req.Direction != "backward" {
		c.JSON(http.StatusBadRequest, ErrorResponse{
			Error:   "Invalid request",
			Message: "Direction must be 'forward' or 'backward'",
		})
		return
	}

	// Validate synctex data
	if req.SyncTexData == "" {
		c.JSON(http.StatusBadRequest, ErrorResponse{
			Error:   "Invalid request",
			Message: "synctexData is required (base64 encoded .synctex.gz content)",
		})
		return
	}

	// Create temp directory
	tempDir, err := os.MkdirTemp("", "synctex-")
	if err != nil {
		c.JSON(http.StatusInternalServerError, ErrorResponse{
			Error:   "Internal error",
			Message: "Failed to create temporary directory",
		})
		return
	}
	defer os.RemoveAll(tempDir)

	// Decode and write synctex data
	synctexBytes, err := base64.StdEncoding.DecodeString(req.SyncTexData)
	if err != nil {
		c.JSON(http.StatusBadRequest, ErrorResponse{
			Error:   "Invalid request",
			Message: "Invalid base64 encoding for synctexData",
		})
		return
	}

	// Determine PDF name (default to "output" if not provided)
	pdfName := req.PDFName
	if pdfName == "" {
		pdfName = "output"
	}
	// Remove .pdf extension if present
	pdfName = strings.TrimSuffix(pdfName, ".pdf")

	// Write synctex file
	synctexPath := filepath.Join(tempDir, pdfName+".synctex.gz")
	if err := os.WriteFile(synctexPath, synctexBytes, 0644); err != nil {
		c.JSON(http.StatusInternalServerError, ErrorResponse{
			Error:   "Internal error",
			Message: "Failed to write synctex file",
		})
		return
	}

	// Create a dummy PDF file (synctex needs it to exist)
	pdfPath := filepath.Join(tempDir, pdfName+".pdf")
	if err := os.WriteFile(pdfPath, []byte("%PDF-1.4"), 0644); err != nil {
		c.JSON(http.StatusInternalServerError, ErrorResponse{
			Error:   "Internal error",
			Message: "Failed to create PDF placeholder",
		})
		return
	}

	var output []byte
	var cmdErr error

	if req.Direction == "forward" {
		// Forward sync: source → PDF
		if req.File == "" || req.Line == 0 {
			c.JSON(http.StatusBadRequest, ErrorResponse{
				Error:   "Invalid request",
				Message: "Forward sync requires 'file' and 'line' parameters",
			})
			return
		}

		// synctex view -i line:column:file -o output.pdf
		column := req.Column
		if column == 0 {
			column = 1 // Default to column 1
		}

		inputSpec := fmt.Sprintf("%d:%d:%s", req.Line, column, req.File)
		cmd := exec.Command("synctex", "view",
			"-i", inputSpec,
			"-o", pdfPath,
		)
		cmd.Dir = tempDir
		output, cmdErr = cmd.CombinedOutput()

	} else {
		// Backward sync: PDF → source
		if req.Page == 0 {
			c.JSON(http.StatusBadRequest, ErrorResponse{
				Error:   "Invalid request",
				Message: "Backward sync requires 'page' parameter",
			})
			return
		}

		// synctex edit -o page:x:y:file.pdf
		outputSpec := fmt.Sprintf("%d:%f:%f:%s", req.Page, req.X, req.Y, pdfPath)
		cmd := exec.Command("synctex", "edit",
			"-o", outputSpec,
		)
		cmd.Dir = tempDir
		output, cmdErr = cmd.CombinedOutput()
	}

	rawOutput := string(output)

	// Parse the output
	if req.Direction == "forward" {
		result := parseForwardSyncOutput(rawOutput)
		result.RawOutput = rawOutput
		if cmdErr != nil && result.Page == 0 {
			result.Success = false
			result.Error = fmt.Sprintf("SyncTeX failed: %v", cmdErr)
		} else {
			result.Success = result.Page > 0
		}
		c.JSON(http.StatusOK, result)
	} else {
		result := parseBackwardSyncOutput(rawOutput)
		result.RawOutput = rawOutput
		if cmdErr != nil && result.File == "" {
			result.Success = false
			result.Error = fmt.Sprintf("SyncTeX failed: %v", cmdErr)
		} else {
			result.Success = result.File != ""
		}
		c.JSON(http.StatusOK, result)
	}
}

// parseForwardSyncOutput parses synctex view output
// Output format:
// SyncTeX result begin
// Output:...
// Page:1
// x:100.00
// y:200.00
// h:100.00
// v:200.00
// W:50.00
// H:20.00
// ...
// SyncTeX result end
func parseForwardSyncOutput(output string) SyncTexResponse {
	var result SyncTexResponse

	pagePattern := regexp.MustCompile(`(?m)^Page:(\d+)`)
	xPattern := regexp.MustCompile(`(?m)^x:([\d.]+)`)
	yPattern := regexp.MustCompile(`(?m)^y:([\d.]+)`)
	wPattern := regexp.MustCompile(`(?m)^W:([\d.]+)`)
	hPattern := regexp.MustCompile(`(?m)^H:([\d.]+)`)

	if matches := pagePattern.FindStringSubmatch(output); matches != nil {
		result.Page, _ = strconv.Atoi(matches[1])
	}
	if matches := xPattern.FindStringSubmatch(output); matches != nil {
		result.X, _ = strconv.ParseFloat(matches[1], 64)
	}
	if matches := yPattern.FindStringSubmatch(output); matches != nil {
		result.Y, _ = strconv.ParseFloat(matches[1], 64)
	}
	if matches := wPattern.FindStringSubmatch(output); matches != nil {
		result.Width, _ = strconv.ParseFloat(matches[1], 64)
	}
	if matches := hPattern.FindStringSubmatch(output); matches != nil {
		result.Height, _ = strconv.ParseFloat(matches[1], 64)
	}

	return result
}

// parseBackwardSyncOutput parses synctex edit output
// Output format:
// SyncTeX result begin
// Output:...
// Input:main.tex
// Line:42
// Column:-1
// ...
// SyncTeX result end
func parseBackwardSyncOutput(output string) SyncTexResponse {
	var result SyncTexResponse

	inputPattern := regexp.MustCompile(`(?m)^Input:(.+)$`)
	linePattern := regexp.MustCompile(`(?m)^Line:(\d+)`)
	columnPattern := regexp.MustCompile(`(?m)^Column:(-?\d+)`)

	if matches := inputPattern.FindStringSubmatch(output); matches != nil {
		result.File = strings.TrimSpace(matches[1])
	}
	if matches := linePattern.FindStringSubmatch(output); matches != nil {
		result.Line, _ = strconv.Atoi(matches[1])
	}
	if matches := columnPattern.FindStringSubmatch(output); matches != nil {
		result.Column, _ = strconv.Atoi(matches[1])
	}

	return result
}
