// Package setup provides embeddings setup functionality for Ancora.
//
// This file handles:
//   - Checking if embeddings are already configured
//   - Downloading the nomic-embed-text GGUF model from HuggingFace
//   - Verifying llama.cpp CLI availability
//   - Guiding users through llama.cpp installation
package setup

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/Syfra3/ancora/internal/embed"
)

// EmbeddingsSetupResult holds the outcome of embeddings setup.
type EmbeddingsSetupResult struct {
	ModelInstalled bool
	CLIAvailable   bool
	ModelPath      string
	CLIPath        string
	Message        string
}

// Hugging Face model URL for nomic-embed-text-v1.5 Q4_K_M GGUF
// This is the quantized 4-bit version (~270MB) from nomic-ai
const nomicModelURL = "https://huggingface.co/nomic-ai/nomic-embed-text-v1.5-GGUF/resolve/main/nomic-embed-text-v1.5.Q4_K_M.gguf"

var (
	// Injected for testing
	httpGet          = http.Get
	osCreate         = os.Create
	ioRead           = io.Copy
	osMkdirAll       = os.MkdirAll
	osStat           = os.Stat
	execLookPath     = exec.LookPath
	execCommand      = exec.Command
	embedModelPath   = embed.ModelInstallPath
	embedModelFile   = embed.ModelFileName
	runtimeGOOSEmbed = runtime.GOOS
	testEmbedderFn   = testEmbedder
)

// CheckEmbeddingsStatus checks if embeddings are already set up.
// Returns the current status without making any changes.
func CheckEmbeddingsStatus() (*EmbeddingsSetupResult, error) {
	result := &EmbeddingsSetupResult{}

	// Check model file
	modelPath := filepath.Join(embedModelPath(), embedModelFile)
	if _, err := osStat(modelPath); err == nil {
		result.ModelInstalled = true
		result.ModelPath = modelPath
	}

	// Check CLI availability
	cliPath, err := resolveEmbedCLI()
	if err == nil {
		result.CLIAvailable = true
		result.CLIPath = cliPath
	}

	// Build status message
	if result.ModelInstalled && result.CLIAvailable {
		result.Message = "Embeddings fully configured"
	} else if result.ModelInstalled && !result.CLIAvailable {
		result.Message = "Model installed but llama-embedding CLI not found"
	} else if !result.ModelInstalled && result.CLIAvailable {
		result.Message = "llama-embedding CLI found but model not installed"
	} else {
		result.Message = "Embeddings not configured"
	}

	return result, nil
}

// DownloadModel downloads the nomic-embed-text GGUF model from HuggingFace.
// Returns the path where the model was saved.
func DownloadModel() (string, error) {
	modelDir := embedModelPath()
	if err := osMkdirAll(modelDir, 0755); err != nil {
		return "", fmt.Errorf("create model directory: %w", err)
	}

	modelPath := filepath.Join(modelDir, embedModelFile)

	// Check if already exists
	if _, err := osStat(modelPath); err == nil {
		return modelPath, nil // Already downloaded
	}

	fmt.Printf("Downloading nomic-embed-text-v1.5 model (~270MB)...\n")
	fmt.Printf("From: %s\n", nomicModelURL)
	fmt.Printf("To:   %s\n", modelPath)

	// Download
	resp, err := httpGet(nomicModelURL)
	if err != nil {
		return "", fmt.Errorf("download request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("download failed with status: %d %s", resp.StatusCode, resp.Status)
	}

	// Create temporary file
	tmpPath := modelPath + ".tmp"
	out, err := osCreate(tmpPath)
	if err != nil {
		return "", fmt.Errorf("create temp file: %w", err)
	}

	// Download with progress (basic - could be enhanced with progress bar)
	written, err := ioRead(out, resp.Body)
	out.Close()

	if err != nil {
		os.Remove(tmpPath)
		return "", fmt.Errorf("download failed: %w", err)
	}

	fmt.Printf("Downloaded %d bytes\n", written)

	// Rename to final location
	if err := os.Rename(tmpPath, modelPath); err != nil {
		os.Remove(tmpPath)
		return "", fmt.Errorf("finalize model file: %w", err)
	}

	return modelPath, nil
}

// resolveEmbedCLI finds the llama-embedding binary in PATH.
// Tries multiple common names used by llama.cpp distributions.
func resolveEmbedCLI() (string, error) {
	candidates := []string{
		"llama-embedding",
		"llama.cpp-embedding",
		"embedding",
		"llama-cli", // Some distributions use this
	}
	for _, name := range candidates {
		if p, err := execLookPath(name); err == nil {
			return p, nil
		}
	}
	return "", embed.ErrEmbedderUnavailable
}

// GetLlamaCppInstallInstructions returns platform-specific installation
// instructions for llama.cpp CLI binaries.
func GetLlamaCppInstallInstructions() string {
	switch runtimeGOOSEmbed {
	case "darwin": // macOS
		return `
Install llama.cpp on macOS:

Option 1: Homebrew (recommended)
  brew install llama.cpp

Option 2: Build from source
  git clone https://github.com/ggerganov/llama.cpp.git
  cd llama.cpp
  make llama-embedding
  sudo cp llama-embedding /usr/local/bin/

After installation, verify with:
  llama-embedding --help
`

	case "linux":
		return `
Install llama.cpp on Linux:

Option 1: Build from source
  git clone https://github.com/ggerganov/llama.cpp.git
  cd llama.cpp
  make llama-embedding
  sudo cp llama-embedding /usr/local/bin/

Option 2: Use pre-built binaries (if available)
  Check releases at: https://github.com/ggerganov/llama.cpp/releases

After installation, verify with:
  llama-embedding --help
`

	case "windows":
		return `
Install llama.cpp on Windows:

Option 1: Pre-built binaries
  1. Download from: https://github.com/ggerganov/llama.cpp/releases
  2. Extract llama-embedding.exe
  3. Add to PATH or place in C:\Windows\System32\

Option 2: Build with MSVC or MinGW
  git clone https://github.com/ggerganov/llama.cpp.git
  cd llama.cpp
  cmake -B build
  cmake --build build --config Release

After installation, verify with:
  llama-embedding.exe --help
`

	default:
		return `
Install llama.cpp:

Build from source:
  git clone https://github.com/ggerganov/llama.cpp.git
  cd llama.cpp
  make llama-embedding

Verify installation:
  llama-embedding --help

Visit: https://github.com/ggerganov/llama.cpp
`
	}
}

// SetupEmbeddings is the complete embeddings setup flow.
// It checks status, optionally downloads the model, and guides through CLI installation.
func SetupEmbeddings(interactive bool) (*EmbeddingsSetupResult, error) {
	status, err := CheckEmbeddingsStatus()
	if err != nil {
		return nil, err
	}

	// If everything is already set up, verify it works
	if status.ModelInstalled && status.CLIAvailable {
		// Test the embedder to ensure it actually works
		if err := testEmbedderFn(); err != nil {
			status.CLIAvailable = false
			status.Message = fmt.Sprintf("llama-embedding test failed: %v", err)
			// Continue to show instructions
		} else {
			status.Message = "Embeddings fully configured and tested"
			return status, nil
		}
	}

	// Download model if needed
	if !status.ModelInstalled {
		if interactive {
			fmt.Printf("\nEmbeddings model not found.\n")
			fmt.Printf("Would you like to download nomic-embed-text-v1.5? (~270MB) [Y/n]: ")
			var answer string
			fmt.Scanln(&answer)
			answer = strings.TrimSpace(strings.ToLower(answer))

			if answer == "n" || answer == "no" {
				status.Message = "Model download skipped by user"
				return status, nil
			}
		}

		modelPath, err := DownloadModel()
		if err != nil {
			return nil, fmt.Errorf("model download failed: %w", err)
		}

		status.ModelInstalled = true
		status.ModelPath = modelPath
		fmt.Printf("✓ Model downloaded successfully\n\n")
	}

	// Check CLI and provide instructions if needed
	if !status.CLIAvailable {
		fmt.Printf("\nllama-embedding CLI not found in PATH.\n")
		fmt.Printf("This is required to generate embeddings for semantic search.\n")
		fmt.Println(GetLlamaCppInstallInstructions())

		if interactive {
			fmt.Printf("\nAfter installing llama.cpp, press Enter to continue or Ctrl+C to exit...")
			fmt.Scanln()

			// Re-check after user claims they installed it
			if cliPath, err := resolveEmbedCLI(); err == nil {
				// Test it actually works
				if err := testEmbedderFn(); err != nil {
					status.Message = fmt.Sprintf("llama-embedding found but test failed: %v", err)
					fmt.Printf("⚠️  %s\n", status.Message)
					return status, nil
				}
				status.CLIAvailable = true
				status.CLIPath = cliPath
				fmt.Printf("✓ llama-embedding found and tested: %s\n", cliPath)
			} else {
				status.Message = "llama-embedding still not found in PATH"
				return status, nil
			}
		}
	}

	// Final status
	if status.ModelInstalled && status.CLIAvailable {
		status.Message = "Embeddings fully configured and tested"
	} else {
		status.Message = "Embeddings setup incomplete"
	}

	return status, nil
}

// testEmbedder tests that the embedder actually works by generating a test embedding.
func testEmbedder() error {
	embedder, err := embed.New()
	if err != nil {
		return err
	}

	// Try to embed a simple test string
	vec, err := embedder.Embed("test")
	if err != nil {
		return fmt.Errorf("embedding generation failed: %w", err)
	}

	if len(vec) != embed.Dims {
		return fmt.Errorf("expected %d dimensions, got %d", embed.Dims, len(vec))
	}

	return nil
}

// InstallLlamaCpp attempts to install llama.cpp using the system package manager.
// Returns the path to the installed binary or an error with instructions.
func InstallLlamaCpp() (string, error) {
	// First check if already installed
	if path, err := resolveEmbedCLI(); err == nil {
		return path, nil
	}

	switch runtimeGOOSEmbed {
	case "darwin", "linux":
		// Try Homebrew (works on both macOS and Linux)
		brewPath, err := execLookPath("brew")
		if err != nil {
			return "", fmt.Errorf("homebrew not found. Install manually:\n%s", GetLlamaCppInstallInstructions())
		}

		fmt.Println("Installing llama.cpp via Homebrew...")
		cmd := execCommand(brewPath, "install", "llama.cpp")
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr

		if err := cmd.Run(); err != nil {
			return "", fmt.Errorf("homebrew install failed: %w\n%s", err, GetLlamaCppInstallInstructions())
		}

		// Verify installation
		path, err := resolveEmbedCLI()
		if err != nil {
			return "", fmt.Errorf("llama.cpp installed but binary not found in PATH")
		}

		return path, nil

	case "windows":
		// Windows requires manual installation
		return "", fmt.Errorf("automatic installation not supported on Windows\n%s", GetLlamaCppInstallInstructions())

	default:
		return "", fmt.Errorf("automatic installation not supported on %s\n%s", runtimeGOOSEmbed, GetLlamaCppInstallInstructions())
	}
}

// CheckLlamaCpp checks if llama-embedding CLI is available.
func CheckLlamaCpp() (bool, string) {
	path, err := resolveEmbedCLI()
	if err != nil {
		return false, ""
	}
	return true, path
}
