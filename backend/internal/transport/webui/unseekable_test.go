package webui

import (
	"io/fs"
	"testing/fstest"
)

// unseekableFS is a file system whose files cannot seek.
type unseekableFS struct{}

// Open returns a file that implements only io.Reader.
func (unseekableFS) Open(name string) (fs.File, error) {
	base, err := fstest.MapFS{"index.html": {Data: []byte("x")}}.Open(name)
	if err != nil {
		return nil, err
	}
	return readerOnly{base}, nil
}

// readerOnly hides every optional capability of a file except reading.
type readerOnly struct {
	fs.File
}
