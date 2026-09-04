package bets

import (
	"fmt"
	"os"
	"strconv"
)

type Writer struct {
	file   *os.File
	buffer []byte
}

func NewWriter(path string) (*Writer, error) {
	// Se trunca el archivo para que una corrida no deje sus registros pegados
	// a los de la anterior.
	file, err := os.Create(path)
	if err != nil {
		return nil, err
	}

	return &Writer{file: file}, nil
}

func (writer *Writer) WriteBet(bet Bet) error {
	writer.buffer = append(writer.buffer[:0], bet.FirstName...)
	writer.buffer = append(writer.buffer, ',')
	writer.buffer = append(writer.buffer, bet.LastName...)
	writer.buffer = append(writer.buffer, ',')
	writer.buffer = strconv.AppendUint(writer.buffer, uint64(bet.Document), 10)
	writer.buffer = append(writer.buffer, ',')
	writer.buffer = append(writer.buffer, bet.Birthdate...)
	writer.buffer = append(writer.buffer, ',')
	writer.buffer = strconv.AppendUint(writer.buffer, uint64(bet.Number), 10)
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
