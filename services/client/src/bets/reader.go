package bets

import (
	"bufio"
	"os"
)

type Reader struct {
	file    *os.File
	scanner *bufio.Scanner
}

func NewReader(path string) (*Reader, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}

	return &Reader{file: file, scanner: bufio.NewScanner(file)}, nil
}

func (reader *Reader) Next() bool {
	for reader.scanner.Scan() {
		if len(reader.scanner.Bytes()) == 0 {
			continue
		}
		return true
	}

	return false
}

func (reader *Reader) Bet() (Bet, error) {
	return parseRecord(reader.scanner.Bytes())
}

func (reader *Reader) Err() error {
	return reader.scanner.Err()
}

func (reader *Reader) Close() error {
	return reader.file.Close()
}
