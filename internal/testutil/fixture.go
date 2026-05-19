package testutil

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
)

// fixturePath returns the absolute path to a fixture file in testdata directory.
// It uses runtime.Caller to resolve the path relative to the test file.
func fixturePath(name string) (string, error) {
	_, file, _, ok := runtime.Caller(1)
	if !ok {
		return "", fmt.Errorf("failed to get caller")
	}
	// Navigate from test file to project root (testutil -> project root)
	projectRoot := filepath.Dir(filepath.Dir(filepath.Dir(file)))
	return filepath.Join(projectRoot, "testdata", name), nil
}

// LoadFixture reads a fixture file from testdata directory and returns its contents.
func LoadFixture(name string) ([]byte, error) {
	path, err := fixturePath(name)
	if err != nil {
		return nil, fmt.Errorf("resolve fixture path: %w", err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read fixture %s: %w", name, err)
	}
	return data, nil
}

// LoadFixtureReader returns an io.Reader for a fixture file.
func LoadFixtureReader(name string) (io.Reader, error) {
	path, err := fixturePath(name)
	if err != nil {
		return nil, fmt.Errorf("resolve fixture path: %w", err)
	}
	file, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open fixture %s: %w", name, err)
	}
	return file, nil
}
