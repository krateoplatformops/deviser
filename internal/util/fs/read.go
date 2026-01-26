package fs

import (
	"io"
	"io/fs"
)

// ReadFile reads and returns the content of the named file.
func ReadFile(fsys fs.FS, name string) ([]byte, error) {
	fin, err := fsys.Open(name)
	if err != nil {
		return nil, err
	}
	defer fin.Close()

	return io.ReadAll(fin)
}
