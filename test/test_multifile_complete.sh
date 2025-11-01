#!/bin/bash

# Test the multifile-complete project using octree-compile service

echo "=== Testing Multi-File Complete Project ==="
echo ""

# Check if octree-compile service is running
if ! curl -s http://localhost:3001/health > /dev/null 2>&1; then
    echo "❌ octree-compile service is not running on localhost:3001"
    echo "Please start it first with: cd octree-compile && ./latex-compile"
    exit 1
fi

echo "✓ octree-compile service is running"
echo ""

# Use the encode_project.py script to send the project
cd "$(dirname "$0")"

echo "Encoding and sending project..."
python3 encode_project.py files/multifile-complete

if [ $? -eq 0 ]; then
    echo ""
    echo "✅ Multi-file compilation test successful!"
else
    echo ""
    echo "❌ Multi-file compilation test failed!"
    exit 1
fi

