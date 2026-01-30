# VHS Tape File - The Redirector TUI Demo
# https://github.com/charmbracelet/vhs
#
# Prerequisites:
#   1. Install VHS: brew install charmbracelet/tap/vhs (or see https://github.com/charmbracelet/vhs)
#   2. Build binaries: go build ./...
#   3. Start redirector: ./bin/redirector -config config.yaml
#
# Generate GIF:
#   vhs docs/tui-demo.tape

Output docs/tui-demo.gif

Set Shell "bash"
Set FontSize 14
Set Width 1200
Set Height 700
Set Theme "Catppuccin Mocha"
Set Padding 20

# Start the TUI connected to local management API
Type "bin/redirector-tui --url http://localhost:8081"
Enter
Sleep 2s

# Show the traffic view with live requests
Sleep 3s

# Sort by latency (key 3)
Type "3"
Sleep 2s

# Open filter mode
Type "f"
Sleep 500ms

# Filter for /api paths
Type "/api"
Enter
Sleep 2s

# Clear filter
Type "c"
Sleep 1s

# Switch to config view
Tab
Sleep 3s

# Switch back to traffic view
Tab
Sleep 2s

# Pause the stream
Type "p"
Sleep 2s

# Resume
Type "p"
Sleep 2s

# Quit
Type "q"
Sleep 500ms
