#!/bin/bash

# Comprehensive server comparison: TeX Live vs Tectonic
# Tests both functional correctness and performance

GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
CYAN='\033[0;36m'
RED='\033[0;31m'
NC='\033[0m'

TEXLIVE_SERVER="138.197.13.3:3001"
TECTONIC_SERVER="167.172.16.84:3001"
TIMESTAMP=$(date +%s)
RESULTS_DIR="/tmp/server-comparison-$TIMESTAMP"

mkdir -p "$RESULTS_DIR"

echo -e "${BLUE}╔════════════════════════════════════════════════════════╗${NC}"
echo -e "${BLUE}║     LaTeX Compilation Server Comparison               ║${NC}"
echo -e "${BLUE}║     TeX Live vs Tectonic Performance Test             ║${NC}"
echo -e "${BLUE}╚════════════════════════════════════════════════════════╝${NC}"
echo ""
echo -e "${CYAN}TeX Live Server:  ${TEXLIVE_SERVER}${NC}"
echo -e "${CYAN}Tectonic Server:  ${TECTONIC_SERVER}${NC}"
echo -e "${CYAN}Results Directory: ${RESULTS_DIR}${NC}"
echo ""

# Function to test a simple compilation
test_simple_compile() {
    local server=$1
    local name=$2
    local output_file=$3
    
    local start=$(date +%s%3N)
    local http_code=$(curl -X POST "http://${server}/compile" \
        -H "Content-Type: application/json" \
        -d '{"content":"\\documentclass{article}\\begin{document}Hello World!\\end{document}"}' \
        --output "$output_file" \
        --silent \
        --write-out "%{http_code}")
    local end=$(date +%s%3N)
    local duration=$((end - start))
    
    if [ "$http_code" = "200" ] && [ -f "$output_file" ]; then
        local size=$(stat -f%z "$output_file" 2>/dev/null || stat -c%s "$output_file" 2>/dev/null)
        echo "$duration,$size,success"
    else
        echo "$duration,0,failed"
    fi
}

# Function to test multi-file compilation
test_multifile_compile() {
    local server=$1
    local output_file=$2
    
    local start=$(date +%s%3N)
    local http_code=$(curl -X POST "http://${server}/compile" \
        -H "Content-Type: application/json" \
        -d '{
            "files": [
                {"path": "main.tex", "content": "\\documentclass{article}\\begin{document}\\section{Main}\\input{chapter1}\\end{document}"},
                {"path": "chapter1.tex", "content": "This is chapter 1 content."}
            ]
        }' \
        --output "$output_file" \
        --silent \
        --write-out "%{http_code}")
    local end=$(date +%s%3N)
    local duration=$((end - start))
    
    if [ "$http_code" = "200" ] && [ -f "$output_file" ]; then
        local size=$(stat -f%z "$output_file" 2>/dev/null || stat -c%s "$output_file" 2>/dev/null)
        echo "$duration,$size,success"
    else
        echo "$duration,0,failed"
    fi
}

# Function to test bibliography compilation
test_bibliography_compile() {
    local server=$1
    local output_file=$2
    
    local start=$(date +%s%3N)
    local http_code=$(curl -X POST "http://${server}/compile" \
        -H "Content-Type: application/json" \
        -d '{
            "files": [
                {"path": "main.tex", "content": "\\documentclass{article}\\begin{document}Citation~\\cite{test2024}.\\bibliographystyle{plain}\\bibliography{refs}\\end{document}"},
                {"path": "refs.bib", "content": "@article{test2024,author={Test},title={Test},year={2024}}"}
            ]
        }' \
        --output "$output_file" \
        --silent \
        --write-out "%{http_code}")
    local end=$(date +%s%3N)
    local duration=$((end - start))
    
    if [ "$http_code" = "200" ] && [ -f "$output_file" ]; then
        local size=$(stat -f%z "$output_file" 2>/dev/null || stat -c%s "$output_file" 2>/dev/null)
        echo "$duration,$size,success"
    else
        echo "$duration,0,failed"
    fi
}

echo -e "${YELLOW}═══════════════════════════════════════════════════════${NC}"
echo -e "${YELLOW}Phase 1: Functional Tests${NC}"
echo -e "${YELLOW}═══════════════════════════════════════════════════════${NC}"
echo ""

# Test 1: Simple Compilation
echo -e "${CYAN}Test 1: Simple Document Compilation${NC}"
echo -n "  TeX Live...  "
result_texlive=$(test_simple_compile "$TEXLIVE_SERVER" "TeX Live" "$RESULTS_DIR/texlive-simple.pdf")
time_tl=$(echo $result_texlive | cut -d',' -f1)
status_tl=$(echo $result_texlive | cut -d',' -f3)
echo -e "${time_tl}ms - ${status_tl}"

echo -n "  Tectonic...  "
result_tectonic=$(test_simple_compile "$TECTONIC_SERVER" "Tectonic" "$RESULTS_DIR/tectonic-simple.pdf")
time_tc=$(echo $result_tectonic | cut -d',' -f1)
status_tc=$(echo $result_tectonic | cut -d',' -f3)
echo -e "${time_tc}ms - ${status_tc}"

if [ $time_tc -lt $time_tl ]; then
    improvement=$(echo "scale=1; ($time_tl - $time_tc) * 100 / $time_tl" | bc)
    echo -e "${GREEN}  → Tectonic ${improvement}% faster${NC}"
else
    diff=$(echo "scale=1; ($time_tc - $time_tl) * 100 / $time_tl" | bc)
    echo -e "${YELLOW}  → TeX Live ${diff}% faster${NC}"
fi
echo ""

# Test 2: Multi-file Compilation
echo -e "${CYAN}Test 2: Multi-file Document${NC}"
echo -n "  TeX Live...  "
result_texlive=$(test_multifile_compile "$TEXLIVE_SERVER" "$RESULTS_DIR/texlive-multifile.pdf")
time_tl=$(echo $result_texlive | cut -d',' -f1)
status_tl=$(echo $result_texlive | cut -d',' -f3)
echo -e "${time_tl}ms - ${status_tl}"

echo -n "  Tectonic...  "
result_tectonic=$(test_multifile_compile "$TECTONIC_SERVER" "$RESULTS_DIR/tectonic-multifile.pdf")
time_tc=$(echo $result_tectonic | cut -d',' -f1)
status_tc=$(echo $result_tectonic | cut -d',' -f3)
echo -e "${time_tc}ms - ${status_tc}"

if [ $time_tc -lt $time_tl ]; then
    improvement=$(echo "scale=1; ($time_tl - $time_tc) * 100 / $time_tl" | bc)
    echo -e "${GREEN}  → Tectonic ${improvement}% faster${NC}"
else
    diff=$(echo "scale=1; ($time_tc - $time_tl) * 100 / $time_tl" | bc)
    echo -e "${YELLOW}  → TeX Live ${diff}% faster${NC}"
fi
echo ""

# Test 3: Bibliography Compilation
echo -e "${CYAN}Test 3: Document with Bibliography${NC}"
echo -n "  TeX Live...  "
result_texlive=$(test_bibliography_compile "$TEXLIVE_SERVER" "$RESULTS_DIR/texlive-bib.pdf")
time_tl=$(echo $result_texlive | cut -d',' -f1)
status_tl=$(echo $result_texlive | cut -d',' -f3)
echo -e "${time_tl}ms - ${status_tl}"

echo -n "  Tectonic...  "
result_tectonic=$(test_bibliography_compile "$TECTONIC_SERVER" "$RESULTS_DIR/tectonic-bib.pdf")
time_tc=$(echo $result_tectonic | cut -d',' -f1)
status_tc=$(echo $result_tectonic | cut -d',' -f3)
echo -e "${time_tc}ms - ${status_tc}"

if [ "$status_tc" = "success" ]; then
    if [ $time_tc -lt $time_tl ]; then
        improvement=$(echo "scale=1; ($time_tl - $time_tc) * 100 / $time_tl" | bc)
        echo -e "${GREEN}  → Tectonic ${improvement}% faster${NC}"
    else
        diff=$(echo "scale=1; ($time_tc - $time_tl) * 100 / $time_tl" | bc)
        echo -e "${YELLOW}  → TeX Live ${diff}% faster${NC}"
    fi
else
    echo -e "${YELLOW}  → Tectonic failed, fell back to TeX Live${NC}"
fi
echo ""

echo -e "${YELLOW}═══════════════════════════════════════════════════════${NC}"
echo -e "${YELLOW}Phase 2: Load Tests${NC}"
echo -e "${YELLOW}═══════════════════════════════════════════════════════${NC}"
echo ""

# Run load tests on both servers
echo -e "${CYAN}Running load tests on TeX Live server...${NC}"
cd "$(dirname "$0")"
sed "s|API=.*|API=\"http://$TEXLIVE_SERVER/compile\"|" load-test.sh > /tmp/load-test-texlive.sh
chmod +x /tmp/load-test-texlive.sh
/tmp/load-test-texlive.sh > "$RESULTS_DIR/load-test-texlive.txt" 2>&1
echo -e "${GREEN}✓ TeX Live load test complete${NC}"
echo ""

echo -e "${CYAN}Running load tests on Tectonic server...${NC}"
sed "s|API=.*|API=\"http://$TECTONIC_SERVER/compile\"|" load-test.sh > /tmp/load-test-tectonic.sh
chmod +x /tmp/load-test-tectonic.sh
/tmp/load-test-tectonic.sh > "$RESULTS_DIR/load-test-tectonic.txt" 2>&1
echo -e "${GREEN}✓ Tectonic load test complete${NC}"
echo ""

echo -e "${BLUE}╔════════════════════════════════════════════════════════╗${NC}"
echo -e "${BLUE}║                  Results Summary                       ║${NC}"
echo -e "${BLUE}╚════════════════════════════════════════════════════════╝${NC}"
echo ""

# Extract key metrics from load tests
echo -e "${CYAN}Sequential Baseline Performance:${NC}"
tl_seq=$(grep "Sequential average:" "$RESULTS_DIR/load-test-texlive.txt" | grep -o '[0-9]*ms' | head -1)
tc_seq=$(grep "Sequential average:" "$RESULTS_DIR/load-test-tectonic.txt" | grep -o '[0-9]*ms' | head -1)
echo "  TeX Live:  $tl_seq"
echo "  Tectonic:  $tc_seq"
echo ""

echo -e "${CYAN}Cache Hit Performance:${NC}"
tl_cache=$(grep "Average per hit:" "$RESULTS_DIR/load-test-texlive.txt" | grep -o '[0-9]*ms' | head -1)
tc_cache=$(grep "Average per hit:" "$RESULTS_DIR/load-test-tectonic.txt" | grep -o '[0-9]*ms' | head -1)
echo "  TeX Live:  $tl_cache"
echo "  Tectonic:  $tc_cache"
echo ""

echo -e "${CYAN}Incremental Compilation:${NC}"
tl_incr=$(grep "Incremental average:" "$RESULTS_DIR/load-test-texlive.txt" | grep -o '[0-9]*ms' | head -1)
tc_incr=$(grep "Incremental average:" "$RESULTS_DIR/load-test-tectonic.txt" | grep -o '[0-9]*ms' | head -1)
echo "  TeX Live:  $tl_incr"
echo "  Tectonic:  $tc_incr"
echo ""

echo -e "${GREEN}All results saved to: ${RESULTS_DIR}${NC}"
echo ""
echo "Files generated:"
ls -lh "$RESULTS_DIR" | tail -n +2 | awk '{print "  " $9 " - " $5}'
echo ""

