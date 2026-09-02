package bets

import (
	"fmt"
	"os"
)


type Writer struct {
	file *os.File
	buffer []byte
}

func NewWriter(path string) (*Writer, error) {
	file, err := os.Create(path)
	if err != nil {
		return nil, err
	}

	return &Writer{file: file}, nil
}

func (writer *Writer) WriteRecord(record []byte) error {
	writer.buffer = append(writer.buffer[:0], record...)
	writer.buffer = append(writer.buffer, '\n')

	written, err := writer.file.Write(writer.buffer)
	if err != nil {
		return err
	}
	if written != len(writer.buffer) {
		return fmt.Errorf(
			"short write on %s: %d bytes written out of %d",
			writer.file.Name(), written, len(writer.buffer),
		)
	}

	return nil
}

func (writer *Writer) Close() error {
	return writer.file.Close()
}
