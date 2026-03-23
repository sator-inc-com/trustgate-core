//go:build !nollm

package detector

import (
	"os"
	"path/filepath"
	"runtime"
)

// onnxRuntimeLibName returns the platform-specific library filename.
func onnxRuntimeLibName() string {
	switch runtime.GOOS {
	case "windows":
		return "onnxruntime.dll"
	case "darwin":
		return "libonnxruntime.dylib"
	default:
		return "libonnxruntime.so"
	}
}

// onnxRuntimeLibPaths returns candidate paths to search for the ONNX Runtime library.
func onnxRuntimeLibPaths() []string {
	libName := onnxRuntimeLibName()
	var paths []string

	// 1. Same directory as the executable
	if exe, err := os.Executable(); err == nil {
		paths = append(paths, filepath.Join(filepath.Dir(exe), libName))
	}

	// 2. Current working directory
	if cwd, err := os.Getwd(); err == nil {
		paths = append(paths, filepath.Join(cwd, libName))
	}

	// 3. Platform-specific locations
	switch runtime.GOOS {
	case "windows":
		if programFiles := os.Getenv("ProgramFiles"); programFiles != "" {
			paths = append(paths, filepath.Join(programFiles, "TrustGate", libName))
		}
		if localAppData := os.Getenv("LOCALAPPDATA"); localAppData != "" {
			paths = append(paths, filepath.Join(localAppData, "TrustGate", libName))
		}
	case "darwin":
		paths = append(paths,
			filepath.Join("/usr/local/lib", libName),
			filepath.Join("/opt/homebrew/lib", libName),
		)
		if home, err := os.UserHomeDir(); err == nil {
			paths = append(paths, filepath.Join(home, "Library", "Application Support", "TrustGate", libName))
		}
	default: // linux
		paths = append(paths,
			filepath.Join("/usr/lib", libName),
			filepath.Join("/usr/local/lib", libName),
		)
	}

	return paths
}
