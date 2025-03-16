# Quickwipe
 
A secure and efficient block device wiping tool written in Go.

## Features

- Fast block device wiping using cryptographically secure random data
- Direct I/O support for improved performance
- Built-in write speed benchmarking
- Configurable buffer sizes to optimize for different systems
- Skip-factor option for quicker wiping (trading security for speed)
- Auto-skip calculation to target a specific completion time
- Real-time progress tracking with speed and ETA display
- Multiple safety confirmation prompts to prevent accidental data loss

## Installation

### Using Go

```bash
go install github.com/f0o/quickwipe@latest
```

### From Source

```bash
git clone https://github.com/f0o/quickwipe.git
cd quickwipe
go build -o quickwipe
```

## Usage

```bash
# Basic usage - wipe a device completely
sudo ./quickwipe -device /dev/sdX

# Quick wipe by only writing every 10th block
sudo ./quickwipe -device /dev/sdX -skip 10

# Auto-determine skip factor to complete in about 20 hours
sudo ./quickwipe -device /dev/sdX -auto-skip

# Specify custom target time for auto-skip (e.g., 5 hours)
sudo ./quickwipe -device /dev/sdX -auto-skip -target-hours 5

# Use a larger buffer for potentially faster wiping
sudo ./quickwipe -device /dev/sdX -buffer 8388608  # 8 MB buffer

# Skip confirmation prompts (use with caution!)
sudo ./quickwipe -device /dev/sdX -force
```

## Command Line Options

| Flag | Description | Default |
|------|-------------|---------|
| `-device` | Path to block device (required) | - |
| `-buffer` | Buffer size in bytes | 4 MB |
| `-skip` | Only write every Nth block (1 = wipe all) | 1 |
| `-auto-skip` | Auto-determine skip factor | false |
| `-target-hours` | Target completion time for auto-skip | 20.0 |
| `-force` | Skip confirmation prompts | false |

## How It Works

Go Wiper performs secure data wiping by:

1. Opening the specified block device with direct I/O when available
2. Filling a memory-aligned buffer with cryptographically secure random data
3. Writing this random data over the entire device (or every Nth block if skip factor > 1)
4. Using synchronized writes to ensure data is properly committed to the physical media

When using the auto-skip feature, Go Wiper first performs a benchmark to determine the write speed of your device, then calculates a skip factor that will allow the operation to complete in approximately the target time.

## Safety Considerations

- **IMPORTANT**: This tool permanently and irreversibly destroys all data on the specified device
- Multiple confirmation prompts help prevent accidental data loss
- The tool verifies that the provided path looks like a block device (starts with `/dev/`)
- Use the `-force` flag with extreme caution - it bypasses safety confirmations

## Requirements

- Go 1.23 or higher
- Root/sudo access (typically required for raw block device access)
- Linux operating system (for direct I/O support)

## Disclaimer

THIS SOFTWARE IS PROVIDED "AS IS" WITHOUT WARRANTY OF ANY KIND. THE AUTHOR IS NOT RESPONSIBLE FOR ANY DATA LOSS OR DAMAGES RESULTING FROM THE USE OF THIS SOFTWARE.
