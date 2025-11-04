#!/bin/bash

# Test script for multi-file LaTeX compilation
# This demonstrates the new multi-file API

# Accept base URL as argument, default to localhost
BASE_URL="${1:-http://localhost:3001}"

echo "======================================"
echo "Multi-File LaTeX Compilation Tests"
echo "======================================"
echo

# Test 1: Single file (backward compatibility)
echo "Test 1: Single file mode (backward compatible)"
echo "----------------------------------------------"
curl -X POST "$BASE_URL/compile" \
  -H "Content-Type: application/json" \
  -d '{
    "content": "\\documentclass{article}\\begin{document}Hello World!\\end{document}"
  }' \
  -o /tmp/test1.pdf \
  -w "\nStatus: %{http_code}\nTime: %{time_total}s\n\n"

# Test 2: Multi-file simple (no bibliography)
echo "Test 2: Multi-file simple (fast path)"
echo "--------------------------------------"
curl -X POST "$BASE_URL/compile" \
  -H "Content-Type: application/json" \
  -d '{
    "files": [
      {
        "path": "main.tex",
        "content": "\\documentclass{article}\\begin{document}\\section{Main}\\input{chapter1}\\end{document}"
      },
      {
        "path": "chapter1.tex",
        "content": "This is chapter 1 content."
      }
    ]
  }' \
  -o /tmp/test2.pdf \
  -w "\nStatus: %{http_code}\nTime: %{time_total}s\n\n"

# Test 3: Multi-file with cross-references (two passes)
echo "Test 3: Multi-file with cross-references (two passes)"
echo "------------------------------------------------------"
curl -X POST "$BASE_URL/compile" \
  -H "Content-Type: application/json" \
  -d '{
    "files": [
      {
        "path": "main.tex",
        "content": "\\documentclass{article}\\begin{document}\\section{Test}\\label{sec:test}See Section~\\ref{sec:test}.\\end{document}"
      }
    ]
  }' \
  -o /tmp/test3.pdf \
  -w "\nStatus: %{http_code}\nTime: %{time_total}s\n\n"

# Test 4: Multi-file with bibliography (full pipeline)
echo "Test 4: Multi-file with bibliography (full pipeline)"
echo "-----------------------------------------------------"
curl -X POST "$BASE_URL/compile" \
  -H "Content-Type: application/json" \
  -d '{
    "files": [
      {
        "path": "main.tex",
        "content": "\\documentclass{article}\\begin{document}\\section{Test}Citation here~\\cite{test2024}.\\bibliographystyle{plain}\\bibliography{refs}\\end{document}"
      },
      {
        "path": "refs.bib",
        "content": "@article{test2024,author={Test Author},title={Test Paper},year={2024}}"
      }
    ]
  }' \
  -o /tmp/test4.pdf \
  -w "\nStatus: %{http_code}\nTime: %{time_total}s\n\n"

# Test 5: Complex multi-file with custom package
echo "Test 5: Complex multi-file with custom package"
echo "-----------------------------------------------"
curl -X POST "$BASE_URL/compile" \
  -H "Content-Type: application/json" \
  -d '{
    "files": [
      {
        "path": "main.tex",
        "content": "\\documentclass{article}\\usepackage{custom}\\begin{document}\\customcmd{Hello}\\input{chapters/intro}\\end{document}"
      },
      {
        "path": "chapters/intro.tex",
        "content": "\\section{Introduction}This is the intro chapter."
      },
      {
        "path": "custom.sty",
        "content": "\\ProvidesPackage{custom}\\newcommand{\\customcmd}[1]{\\textbf{#1}}\\endinput"
      }
    ]
  }' \
  -o /tmp/test5.pdf \
  -w "\nStatus: %{http_code}\nTime: %{time_total}s\n\n"

echo "======================================"
echo "Tests Complete!"
echo "Output files: /tmp/test1.pdf through /tmp/test5.pdf"
echo "======================================"

