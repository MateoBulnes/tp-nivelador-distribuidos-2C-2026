package safe_socket

import "io"

func SendAll(socket io.Writer, data []byte) error {
	for sent := 0; sent < len(data); {
		written, err := socket.Write(data[sent:])
		if err != nil {
			return err
		}

		sent += written
	}

	return nil
}

func RecvAll(socket io.Reader, size int) ([]byte, error) {
	buffer := make([]byte, size)

	for received := 0; received < size; {
		read, err := socket.Read(buffer[received:])

		received += read
		if received == size {
			break
		}

		if err != nil {
			if err == io.EOF {
				return nil, io.ErrUnexpectedEOF
			}
			return nil, err
		}
	}

	return buffer, nil
}
