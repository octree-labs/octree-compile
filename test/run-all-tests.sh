#!/bin/bash

# Simple test runner for both servers
# Runs existing test scripts on TeX Live and Tectonic servers

GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
CYAN='\033[0;36m'
RED='\033[0;31m'
NC='\033[0m'

TEXLIVE_SERVER="138.197.13.3:3001"
TECTONIC_SERVER="167.172.16.84:3001"
TIMESTAMP=$(date +%s)
RESULTS_DIR="/tmp/test-comparison-$TIMESTAMP"

mkdir -p "$RESULTS_DIR"

echo -e "${BLUE}╔════════════════════════════════════════════════════════╗${NC}"
echo -e "${BLUE}║     Running All Tests on Both Servers                 ║${NC}"
echo -e "${BLUE}╚════════════════════════════════════════════════════════╝${NC}"
echo ""
echo -e "${CYAN}TeX Live Server:  ${TEXLIVE_SERVER}${NC}"
echo -e "${CYAN}Tectonic Server:  ${TECTONIC_SERVER}${NC}"
echo -e "${CYAN}Results Directory: ${RESULTS_DIR}${NC}"
echo ""

cd "$(dirname "$0")"

# Function to run a test script on a server
run_test() {
    local script=$1
    local server=$2
    local server_name=$3
    local output_file=$4
    
    echo -e "${YELLOW}Running ${script} on ${server_name}...${NC}"
    
    # Run the test with the API URL as argument
    if [[ "$script" == *"multifile.sh" ]]; then
        # test_multifile.sh uses BASE_URL format
        "./$script" "http://${server}" > "$output_file" 2>&1
    else
        # Other scripts use API format (with /compile)
        "./$script" "http://${server}/compile" > "$output_file" 2>&1
    fi
    
    if [ $? -eq 0 ]; then
        echo -e "${GREEN}✓ Complete${NC}\n"
    else
        echo -e "${RED}✗ Failed (check logs)${NC}\n"
    fi
}

echo -e "${BLUE}═══════════════════════════════════════════════════════${NC}"
echo -e "${BLUE}Phase 1: Load Tests${NC}"
echo -e "${BLUE}═══════════════════════════════════════════════════════${NC}"
echo ""

run_test "load-test.sh" "$TEXLIVE_SERVER" "texlive" "$RESULTS_DIR/load-test-texlive.txt"
run_test "load-test.sh" "$TECTONIC_SERVER" "tectonic" "$RESULTS_DIR/load-test-tectonic.txt"

echo -e "${BLUE}═══════════════════════════════════════════════════════${NC}"
echo -e "${BLUE}Phase 2: Functional Tests${NC}"
echo -e "${BLUE}═══════════════════════════════════════════════════════${NC}"
echo ""

# Run all specified test scripts
for test in test_multifile.sh test-compilation.sh test-edge-cases.sh test-file-types.sh test-incremental.sh test-tjsass.sh; do
    if [ -f "$test" ]; then
        run_test "$test" "$TEXLIVE_SERVER" "texlive" "$RESULTS_DIR/${test%.sh}-texlive.txt"
        run_test "$test" "$TECTONIC_SERVER" "tectonic" "$RESULTS_DIR/${test%.sh}-tectonic.txt"
    else
        echo -e "${YELLOW}⚠ Skipping $test (not found)${NC}\n"
    fi
done

echo -e "${BLUE}╔════════════════════════════════════════════════════════╗${NC}"
echo -e "${BLUE}║                  Results Summary                       ║${NC}"
echo -e "${BLUE}╚════════════════════════════════════════════════════════╝${NC}"
echo ""

# Extract and compare key metrics from load tests
echo -e "${CYAN}Load Test Results:${NC}"
echo ""
echo "Sequential Baseline:"
tl_seq=$(grep "Sequential average:" "$RESULTS_DIR/load-test-texlive.txt" 2>/dev/null | grep -o '[0-9]*ms' | head -1)
tc_seq=$(grep "Sequential average:" "$RESULTS_DIR/load-test-tectonic.txt" 2>/dev/null | grep -o '[0-9]*ms' | head -1)
echo "  TeX Live:  ${tl_seq:-N/A}"
echo "  Tectonic:  ${tc_seq:-N/A}"
echo ""

echo "Cache Hit Performance:"
tl_cache=$(grep "Average per hit:" "$RESULTS_DIR/load-test-texlive.txt" 2>/dev/null | grep -o '[0-9]*ms' | head -1)
tc_cache=$(grep "Average per hit:" "$RESULTS_DIR/load-test-tectonic.txt" 2>/dev/null | grep -o '[0-9]*ms' | head -1)
echo "  TeX Live:  ${tl_cache:-N/A}"
echo "  Tectonic:  ${tc_cache:-N/A}"
echo ""

echo "Incremental Compilation:"
tl_incr=$(grep "Incremental average:" "$RESULTS_DIR/load-test-texlive.txt" 2>/dev/null | grep -o '[0-9]*ms' | head -1)
tc_incr=$(grep "Incremental average:" "$RESULTS_DIR/load-test-tectonic.txt" 2>/dev/null | grep -o '[0-9]*ms' | head -1)
echo "  TeX Live:  ${tl_incr:-N/A}"
echo "  Tectonic:  ${tc_incr:-N/A}"
echo ""

echo -e "${GREEN}All results saved to: ${RESULTS_DIR}${NC}"
echo ""
echo "View detailed results:"
echo "  cat $RESULTS_DIR/load-test-texlive.txt"
echo "  cat $RESULTS_DIR/load-test-tectonic.txt"
echo ""

