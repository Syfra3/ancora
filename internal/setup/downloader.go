package setup

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"time"
)

const (
	// ModelURL is the HuggingFace download URL for the nomic-embed model.
	ModelURL = "https://huggingface.co/nomic-ai/nomic-embed-text-v1.5-GGUF/resolve/main/nomic-embed-text-v1.5.Q4_K_M.gguf"

	// ModelSHA256 is the expected checksum for integrity verification.
	// TODO: Add actual checksum after downloading the model manually for verification.
	ModelSHA256 = "" // Empty for now — add in follow-up if verification is critical
)

// DownloadProgress represents the current download state.
type DownloadProgress struct {
	BytesDownloaded int64
	TotalBytes      int64
	Speed           float64 // bytes per second
	StartTime       time.Time
	Complete        bool
	Error           error
}

// Downloader handles model file downloads with progress tracking.
type Downloader struct {
	URL         string
	Destination string
	Progress    chan DownloadProgress
}

// NewDownloader creates a downloader for the embedding model.
func NewDownloader(destPath string) *Downloader {
	return &Downloader{
		URL:         ModelURL,
		Destination: destPath,
		Progress:    make(chan DownloadProgress, 10),
	}
}

// Download fetches the model file and reports progress.
// Runs in a goroutine and sends updates via the Progress channel.
func (d *Downloader) Download() error {
	defer close(d.Progress)

	// Create destination directory
	if err := os.MkdirAll(filepath.Dir(d.Destination), 0755); err != nil {
		d.Progress <- DownloadProgress{Error: fmt.Errorf("create dir: %w", err)}
		return err
	}

	// Check if already exists
	if info, err := os.Stat(d.Destination); err == nil {
		// File exists, report immediate completion
		d.Progress <- DownloadProgress{
			BytesDownloaded: info.Size(),
			TotalBytes:      info.Size(),
			Speed:           0,
			Complete:        true,
		}
		return nil
	}

	// Create temporary file
	tmpPath := d.Destination + ".tmp"
	out, err := os.Create(tmpPath)
	if err != nil {
		d.Progress <- DownloadProgress{Error: fmt.Errorf("create file: %w", err)}
		return err
	}
	defer func() {
		out.Close()
		if err != nil {
			os.Remove(tmpPath) // cleanup on error
		}
	}()

	// HTTP GET
	resp, err := http.Get(d.URL)
	if err != nil {
		d.Progress <- DownloadProgress{Error: fmt.Errorf("http get: %w", err)}
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		err := fmt.Errorf("http status %d", resp.StatusCode)
		d.Progress <- DownloadProgress{Error: err}
		return err
	}

	totalBytes := resp.ContentLength
	startTime := time.Now()

	// Progress reader
	var bytesDownloaded int64
	buf := make([]byte, 32*1024) // 32KB chunks

	ticker := time.NewTicker(200 * time.Millisecond)
	defer ticker.Stop()

	for {
		n, readErr := resp.Body.Read(buf)
		if n > 0 {
			if _, writeErr := out.Write(buf[:n]); writeErr != nil {
				d.Progress <- DownloadProgress{Error: fmt.Errorf("write: %w", writeErr)}
				return writeErr
			}
			bytesDownloaded += int64(n)

			// Send progress updates at most every 200ms
			select {
			case <-ticker.C:
				elapsed := time.Since(startTime).Seconds()
				speed := float64(bytesDownloaded) / elapsed
				d.Progress <- DownloadProgress{
					BytesDownloaded: bytesDownloaded,
					TotalBytes:      totalBytes,
					Speed:           speed,
					StartTime:       startTime,
				}
			default:
				// Don't block on channel send
			}
		}
		if readErr == io.EOF {
			break
		}
		if readErr != nil {
			d.Progress <- DownloadProgress{Error: fmt.Errorf("read: %w", readErr)}
			return readErr
		}
	}

	// Final progress update
	elapsed := time.Since(startTime).Seconds()
	speed := float64(bytesDownloaded) / elapsed
	d.Progress <- DownloadProgress{
		BytesDownloaded: bytesDownloaded,
		TotalBytes:      totalBytes,
		Speed:           speed,
		StartTime:       startTime,
	}

	// Atomic rename (complete download)
	if err := os.Rename(tmpPath, d.Destination); err != nil {
		d.Progress <- DownloadProgress{Error: fmt.Errorf("rename: %w", err)}
		return err
	}

	// Optional: verify checksum if provided
	if ModelSHA256 != "" {
		if err := verifyChecksum(d.Destination, ModelSHA256); err != nil {
			d.Progress <- DownloadProgress{Error: fmt.Errorf("checksum failed: %w", err)}
			return err
		}
	}

	d.Progress <- DownloadProgress{
		BytesDownloaded: bytesDownloaded,
		TotalBytes:      totalBytes,
		Speed:           speed,
		Complete:        true,
	}

	return nil
}

// verifyChecksum validates the downloaded file against expected SHA256.
func verifyChecksum(path, expectedHex string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()

	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return err
	}

	actual := hex.EncodeToString(h.Sum(nil))
	if actual != expectedHex {
		return fmt.Errorf("checksum mismatch: expected %s, got %s", expectedHex, actual)
	}

	return nil
}

// FormatBytes converts bytes to human-readable format (MB, GB).
func FormatBytes(bytes int64) string {
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

// FormatSpeed converts bytes/sec to human-readable format.
func FormatSpeed(bytesPerSec float64) string {
	return FormatBytes(int64(bytesPerSec)) + "/s"
}
