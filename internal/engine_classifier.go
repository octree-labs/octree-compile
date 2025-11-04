package internal

import (
	"fmt"
	"regexp"
	"strings"
)

// EngineDecision captures routing hints about which LaTeX engine should run.
type EngineDecision struct {
	RequiresClassic bool
	Reasons         []string
}

var (
	engineDirectiveRegex = regexp.MustCompile(`(?m)^%\s*!TEX\s+program\s*=\s*([^\s]+)`) // % !TEX program = xelatex
	usePackageRegex      = regexp.MustCompile(`\\usepackage(?:\[[^\]]*\])?\{([^}]*)\}`)
	shellEscapeSignals   = []string{
		`\\write18`,
		`%!TEX enableShellEscape`,
		`% !TEX enableShellEscape`,
	}
	shellEscapePackages = []string{
		"minted",
		"pythontex",
		"pygmentex",
		"gnuplottex",
		"shellesc",
	}
	unsupportedPackages = []string{
		"auto-pst-pdf",
		"pstool",
		"pstricks",
		"tex4ht",
	}
	biberHints = []string{
		"backend=biber",
		"%!BIB program = biber",
		"% !BIB program = biber",
	}
)

// AnalyzeEngineRequirements determines whether a project should fall back to the
// classic TeX Live toolchain based on heuristics.
//
// Criteria (OR):
//   - Engine directives requesting xelatex, lualatex, latexmk, etc.
//   - Shell-escape directives or packages known to require --shell-escape.
//   - Explicitly unsupported packages.
//   - Presence of .bib files alongside hints that biber is required.
func AnalyzeEngineRequirements(files []FileEntry) EngineDecision {
	decision := EngineDecision{RequiresClassic: false, Reasons: []string{}}

	hasBibFile := false
	var texLikeContents []string

	for _, file := range files {
		if file.Encoding == "base64" {
			continue
		}

		lowerPath := strings.ToLower(file.Path)

		if strings.HasSuffix(lowerPath, ".bib") {
			hasBibFile = true
		}

		if strings.HasSuffix(lowerPath, ".tex") || strings.HasSuffix(lowerPath, ".sty") || strings.HasSuffix(lowerPath, ".cls") {
			texLikeContents = append(texLikeContents, file.Content)
		}
	}

	joined := strings.Join(texLikeContents, "\n")

	if directive := detectEngineDirective(joined); directive != "" {
		if requiresClassicFromDirective(directive) {
			decision.RequiresClassic = true
			decision.Reasons = append(decision.Reasons, fmt.Sprintf("engine directive requests %s", directive))
		}
	}

	if reason := detectShellEscape(joined); reason != "" {
		decision.RequiresClassic = true
		decision.Reasons = append(decision.Reasons, reason)
	}

	if reason := detectUnsupportedPackages(joined); reason != "" {
		decision.RequiresClassic = true
		decision.Reasons = append(decision.Reasons, reason)
	}

	if hasBibFile && usesBiber(joined) {
		decision.RequiresClassic = true
		decision.Reasons = append(decision.Reasons, "project hints biber backend; classic TeX required")
	}

	return decision
}

func detectEngineDirective(content string) string {
	match := engineDirectiveRegex.FindStringSubmatch(content)
	if len(match) < 2 {
		return ""
	}
	return strings.ToLower(strings.TrimSpace(match[1]))
}

func requiresClassicFromDirective(engine string) bool {
	switch engine {
	case "pdflatex", "tectonic", "":
		return false
	default:
		return true
	}
}

func detectShellEscape(content string) string {
	for _, signal := range shellEscapeSignals {
		if strings.Contains(content, signal) {
			return "shell-escape directive detected"
		}
	}

	packages := extractPackages(content)
	for _, pkg := range shellEscapePackages {
		if packages[pkg] {
			return fmt.Sprintf("package %s requires shell-escape", pkg)
		}
	}

	return ""
}

func detectUnsupportedPackages(content string) string {
	packages := extractPackages(content)
	var flagged []string
	for _, pkg := range unsupportedPackages {
		if packages[pkg] {
			flagged = append(flagged, pkg)
		}
	}

	if len(flagged) == 0 {
		return ""
	}

	return fmt.Sprintf("uses unsupported packages: %s", strings.Join(flagged, ", "))
}

func usesBiber(content string) bool {
	for _, hint := range biberHints {
		if strings.Contains(content, hint) {
			return true
		}
	}
	return false
}

func extractPackages(content string) map[string]bool {
	result := make(map[string]bool)
	matches := usePackageRegex.FindAllStringSubmatch(content, -1)
	for _, match := range matches {
		if len(match) < 2 {
			continue
		}
		packages := strings.Split(match[1], ",")
		for _, pkg := range packages {
			trimmed := strings.ToLower(strings.TrimSpace(pkg))
			if trimmed != "" {
				result[trimmed] = true
			}
		}
	}
	return result
}
