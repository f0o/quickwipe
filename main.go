package main

import (
	"crypto/rand"
	"flag"
	"fmt"
	"os"
	"strings"
	"syscall"
	"time"
	"unsafe"
)

func main() {
	// Parse command-line arguments
	blockDevice := flag.String("device", "", "Path to block device (required)")
	bufferSize := flag.Int("buffer", 4*1024*1024, "Buffer size in bytes")
	skipFactor := flag.Int("skip", 1, "Only write every Nth block (1 = wipe all)")
	autoSkip := flag.Bool("auto-skip", false, "Auto-determine skip factor to finish in -target-hours (default: 20)")
	targetHours := flag.Float64("target-hours", 20.0, "Target completion time in hours for auto-skip")
	force := flag.Bool("force", false, "Skip confirmation prompt")
	flag.Parse()

	if *blockDevice == "" {
		fmt.Println("Error: Block device path is required")
		fmt.Println("Usage: go-wiper -device /path/to/device [-buffer N] [-skip N] [-auto-skip] [-target-hours N] [-force]")
		os.Exit(1)
	}

	if *skipFactor < 1 && !*autoSkip {
		fmt.Println("Error: Skip factor must be at least 1")
		os.Exit(1)
	}

	// Get device size
	deviceSize, err := getDeviceSize(*blockDevice)
	if err != nil {
		fmt.Printf("Error getting device size: %v\n", err)
		os.Exit(1)
	}

	// Auto-determine skip factor if requested
	if *autoSkip {
		fmt.Printf("Running write speed benchmark on %s...\n", *blockDevice)
		writeSpeed, err := benchmarkWriteSpeed(*blockDevice, *bufferSize)
		if err != nil {
			fmt.Printf("Error during benchmark: %v\n", err)
			os.Exit(1)
		}

		fmt.Printf("Benchmark complete. Write speed: %.2f MB/s\n", writeSpeed/1024/1024)

		// Calculate skip factor to complete in target hours
		targetSeconds := *targetHours * 3600
		requiredSpeed := float64(deviceSize) / targetSeconds
		calculatedSkip := int(requiredSpeed / writeSpeed)

		// Ensure minimum skip factor of 1
		if calculatedSkip < 1 {
			calculatedSkip = 1
		}

		*skipFactor = calculatedSkip
		fmt.Printf("Auto-determined skip factor: %d (estimated completion time: %.1f hours)\n",
			*skipFactor, float64(deviceSize)/(writeSpeed*float64(*skipFactor))/3600)
	}

	// Safety check - confirm device path
	if !strings.HasPrefix(*blockDevice, "/dev/") && !*force {
		fmt.Println("Warning: The provided path doesn't look like a block device (doesn't start with /dev/)")
		fmt.Println("This operation is destructive and cannot be undone.")
		fmt.Print("Continue? (y/N): ")
		var response string
		fmt.Scanln(&response)
		if !strings.HasPrefix(strings.ToLower(response), "y") {
			fmt.Println("Operation aborted.")
			os.Exit(0)
		}
	}

	skipWarning := ""
	if *skipFactor > 1 {
		skipWarning = fmt.Sprintf(" (quick wipe: only writing every %dth block)", *skipFactor)
	}

	fmt.Printf("Starting to wipe device: %s (size: %s)%s\n",
		*blockDevice, formatBytes(deviceSize), skipWarning)

	// Final confirmation
	if !*force {
		fmt.Println("WARNING: This will COMPLETELY ERASE all data on this device.")
		fmt.Println("This operation is IRREVERSIBLE.")
		fmt.Print("Are you absolutely sure you want to proceed? (type 'YES' to confirm): ")
		var response string
		fmt.Scanln(&response)
		if response != "YES" {
			fmt.Println("Operation aborted.")
			os.Exit(0)
		}
	}

	// Perform the wipe operation
	err = wipeDevice(*blockDevice, deviceSize, *bufferSize, *skipFactor)
	if err != nil {
		fmt.Printf("Error wiping device: %v\n", err)
		os.Exit(1)
	}

	fmt.Println("Device wiping completed successfully.")
}

// benchmarkWriteSpeed performs a short write test to determine write speed
func benchmarkWriteSpeed(path string, bufferSize int) (float64, error) {
	// Open the device with O_DIRECT and O_SYNC flags for direct, synchronized I/O
	file, err := os.OpenFile(path, os.O_WRONLY|syscall.O_DIRECT|syscall.O_SYNC, 0)
	if err != nil {
		// Fallback to regular I/O with sync if direct I/O is not supported
		fmt.Printf("Warning: Direct I/O not supported, falling back to synchronized buffered I/O: %v\n", err)
		file, err = os.OpenFile(path, os.O_WRONLY|syscall.O_SYNC, 0)
		if err != nil {
			return 0, err
		}
	}

	// Ensure buffer size is aligned to 4KB (typical block size)
	alignedBufferSize := (bufferSize / 4096) * 4096
	if alignedBufferSize < 4096 {
		alignedBufferSize = 4096
	}

	// Create an aligned buffer for direct I/O
	buffer, err := allocAlignedBuffer(alignedBufferSize)
	if err != nil {
		file.Close()
		return 0, fmt.Errorf("failed to allocate aligned buffer: %v", err)
	}

	// How much data to write for benchmark (10240MB by default)
	benchSize := int64(1024 * 1024 * 1024 * 10)

	// For very small devices, adjust benchmark size
	deviceSize, err := getDeviceSize(path)
	if err != nil {
		file.Close()
		return 0, err
	}

	if deviceSize < benchSize*2 {
		benchSize = deviceSize / 4 // Use at most 25% of the device for benchmarking
		if benchSize < int64(bufferSize)*2 {
			benchSize = int64(bufferSize) * 2 // Minimum two buffers
		}
	}

	fmt.Printf("Running benchmark: writing %s of random data...\n", formatBytes(benchSize))

	bytesWritten := int64(0)
	startTime := time.Now()

	// Save original position to restore after benchmark
	originalPos, err := file.Seek(0, 1) // Get current position
	if err != nil {
		file.Close()
		return 0, err
	}

	for bytesWritten < benchSize {
		// Fill buffer with random data
		_, err := rand.Read(buffer)
		if err != nil {
			file.Close()
			return 0, err
		}

		// Calculate how many bytes to write in this iteration
		writeSize := int64(bufferSize)
		if benchSize-bytesWritten < writeSize {
			writeSize = benchSize - bytesWritten
		}

		// Write the buffer to the device
		n, err := file.Write(buffer[:writeSize])
		if err != nil {
			file.Close()
			return 0, err
		}
		bytesWritten += int64(n)

		// Print progress as a simple percentage
		percentComplete := float64(bytesWritten) / float64(benchSize) * 100.0
		fmt.Printf("\r\033[K\rBenchmarking: %.1f%% complete...", percentComplete)
	}

	// Return to original position
	_, err = file.Seek(originalPos, 0)
	if err != nil {
		file.Close()
		return 0, fmt.Errorf("benchmark completed but failed to restore original position: %v", err)
	}

	// Ensure all data is flushed to disk before stopping the timer
	err = file.Sync()
	if err != nil {
		file.Close()
		return 0, fmt.Errorf("benchmark sync failed: %v", err)
	}

	file.Close()

	// Calculate speed
	elapsedTime := time.Since(startTime).Seconds()
	writeSpeed := float64(bytesWritten) / elapsedTime

	fmt.Printf("\r\033[K\rBenchmark complete: wrote %s in %.2f seconds\n",
		formatBytes(bytesWritten), elapsedTime)

	return writeSpeed, nil
}

func getDeviceSize(path string) (int64, error) {
	file, err := os.Open(path)
	if err != nil {
		return 0, err
	}
	defer file.Close()

	// For block devices, use Seek to determine the size
	size, err := file.Seek(0, 2) // Seek to end
	if err != nil {
		return 0, err
	}

	_, err = file.Seek(0, 0) // Reset to beginning
	if err != nil {
		return 0, err
	}

	return size, nil
}

func wipeDevice(path string, size int64, bufferSize int, skipFactor int) error {
	// Open the device with O_DIRECT and O_SYNC flags for direct, synchronized I/O
	file, err := os.OpenFile(path, os.O_WRONLY|syscall.O_DIRECT|syscall.O_SYNC, 0)
	if err != nil {
		// Fallback to regular I/O with sync if direct I/O is not supported
		fmt.Printf("Warning: Direct I/O not supported, falling back to synchronized buffered I/O: %v\n", err)
		file, err = os.OpenFile(path, os.O_WRONLY|syscall.O_SYNC, 0)
		if err != nil {
			return err
		}
	}
	defer file.Close()

	// Ensure buffer size is aligned to 4KB (typical block size)
	alignedBufferSize := (bufferSize / 4096) * 4096
	if alignedBufferSize < 4096 {
		alignedBufferSize = 4096
	}

	// Create an aligned buffer for direct I/O
	buffer, err := allocAlignedBuffer(alignedBufferSize)
	if err != nil {
		return fmt.Errorf("failed to allocate aligned buffer: %v", err)
	}

	// Track progress
	bytesWritten := int64(0)
	bytesProcessed := int64(0) // Track both written and skipped bytes
	startTime := time.Now()
	lastUpdateTime := startTime
	lastUpdateBytes := int64(0)

	// Speed smoothing variables
	const smoothingFactor = 0.2 // Lower = more smoothing
	smoothedSpeed := float64(0)

	// Update interval (update progress every second)
	updateInterval := time.Second

	for bytesProcessed < size {
		// Fill buffer with random data
		_, err := rand.Read(buffer)
		if err != nil {
			return err
		}

		// Calculate how many bytes to write in this iteration
		writeSize := int64(bufferSize)
		if size-bytesProcessed < writeSize {
			writeSize = size - bytesProcessed
		}

		// Write the buffer to the device
		n, err := file.Write(buffer[:writeSize])
		if err != nil {
			return err
		}
		bytesWritten += int64(n)
		bytesProcessed += int64(n)

		// Skip blocks if skipFactor > 1
		if skipFactor > 1 && bytesProcessed < size {
			skipSize := int64(bufferSize) * int64(skipFactor-1)
			// Adjust skip size if we're near the end of the device
			if bytesProcessed+skipSize > size {
				skipSize = size - bytesProcessed
			}

			// Seek forward to skip blocks
			_, err = file.Seek(skipSize, 1) // 1 means relative to current position
			if err != nil {
				return err
			}
			bytesProcessed += skipSize
		}

		// Show progress update if enough time has passed
		currentTime := time.Now()
		if currentTime.Sub(lastUpdateTime) >= updateInterval {
			// Calculate speed based on processed bytes, not just written
			elapsedUpdate := currentTime.Sub(lastUpdateTime).Seconds()
			instantSpeed := float64(bytesProcessed-lastUpdateBytes) / elapsedUpdate

			// Calculate smoothed speed using exponential moving average
			if smoothedSpeed == 0 {
				smoothedSpeed = instantSpeed // Initialize with first measurement
			} else {
				smoothedSpeed = smoothedSpeed*(1-smoothingFactor) + instantSpeed*smoothingFactor
			}

			// Calculate ETA based on smoothed speed
			remainingBytes := size - bytesProcessed
			etaSeconds := float64(remainingBytes) / smoothedSpeed
			eta := time.Duration(etaSeconds) * time.Second

			// Print progress
			percentComplete := float64(bytesProcessed) / float64(size) * 100.0

			progressInfo := fmt.Sprintf("Progress: %.2f%% (%s/%s) at %.2f MB/s, ETA: %s",
				percentComplete,
				formatBytes(bytesProcessed),
				formatBytes(size),
				instantSpeed/1024/1024, // Show current speed for reference
				formatDuration(eta))    // ETA based on smoothed speed

			if skipFactor > 1 {
				coveragePercent := float64(bytesWritten) / float64(size) * 100.0
				progressInfo += fmt.Sprintf(" (%.1f%% of bytes actually overwritten)", coveragePercent)
			}

			fmt.Printf("\r\033[K\r%s", progressInfo)

			// Update tracking variables
			lastUpdateTime = currentTime
			lastUpdateBytes = bytesProcessed
		}
	}

	// Final progress update
	totalTime := time.Since(startTime)
	averageSpeed := float64(bytesProcessed) / totalTime.Seconds()
	summaryMsg := fmt.Sprintf("\nCompleted: Processed %s in %s (average speed: %.2f MB/s)",
		formatBytes(bytesProcessed),
		formatDuration(totalTime),
		averageSpeed/1024/1024)

	if skipFactor > 1 {
		coveragePercent := float64(bytesWritten) / float64(size) * 100.0
		summaryMsg += fmt.Sprintf("\nActually overwritten: %s (%.1f%% of device)",
			formatBytes(bytesWritten), coveragePercent)
	}

	fmt.Println(summaryMsg)

	// Add a final fsync at the end to ensure all data is written to disk
	err = file.Sync()
	if err != nil {
		fmt.Printf("Warning: Final sync operation failed: %v\n", err)
	}

	return nil
}

// allocAlignedBuffer creates a memory-aligned buffer suitable for direct I/O
func allocAlignedBuffer(size int) ([]byte, error) {
	// For simplicity, allocate a larger buffer and find an aligned portion
	// This is a workaround since Go doesn't provide direct aligned allocation
	buffer := make([]byte, size+4096)

	// Calculate the offset needed to align the buffer
	offset := 4096 - (int(uintptr(unsafe.Pointer(&buffer[0]))) % 4096)
	if offset == 4096 {
		offset = 0
	}

	// Return the aligned slice
	return buffer[offset : offset+size], nil
}

func formatDuration(d time.Duration) string {
	d = d.Round(time.Second)
	h := d / time.Hour
	d -= h * time.Hour
	m := d / time.Minute
	d -= m * time.Minute
	s := d / time.Second

	if h > 0 {
		return fmt.Sprintf("%02d:%02d:%02d", h, m, s)
	}
	return fmt.Sprintf("%02d:%02d", m, s)
}

func formatBytes(bytes int64) string {
	const unit = 1024
	if bytes < unit {
		return fmt.Sprintf("%d B", bytes)
	}
	div, exp := int64(unit), 0
	for n := bytes / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(bytes)/float64(div), "KMGTPE"[exp])
}
