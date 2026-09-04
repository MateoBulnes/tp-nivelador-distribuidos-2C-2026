
package protocol

import (
	"encoding/binary"
	"fmt"
	"io"

	"github.com/7574-sistemas-distribuidos/tp-nivelador/src/bets"
	"github.com/7574-sistemas-distribuidos/tp-nivelador/src/safe_socket"
)

const (
	msgHello    byte = 0x01
	msgBet      byte = 0x02
	msgAck      byte = 0x03
	msgFinished byte = 0x04
	msgWinners  byte = 0x05
	msgError    byte = 0x06
)

const (
	uint16Size = 2
	uint32Size = 4
	headerSize = 1 + uint32Size
	maxPayloadSize = 64 * 1024
	minBetSize = 3 + 2*uint32Size
)

type Protocol struct {
	conn io.ReadWriter
	buf  []byte
}

func New(conn io.ReadWriter) *Protocol {
	return &Protocol{conn: conn}
}

func (protocol *Protocol) SendHello(agencyId uint16) error {
	protocol.beginMessage(msgHello)
	protocol.buf = appendUint16(protocol.buf, agencyId)
	return protocol.endMessage()
}

func (protocol *Protocol) SendBet(bet bets.Bet) error {
	if err := bet.Validate(); err != nil {
		return err
	}

	protocol.beginMessage(msgBet)
	protocol.buf = appendBet(protocol.buf, bet)
	return protocol.endMessage()
}

func (protocol *Protocol) SendFinished() error {
	protocol.beginMessage(msgFinished)
	return protocol.endMessage()
}

func (protocol *Protocol) RecvAck() error {
	msgType, payload, err := protocol.recv()
	if err != nil {
		return err
	}

	return expect(msgAck, msgType, payload)
}

func (protocol *Protocol) RecvWinners() ([]bets.Bet, error) {
	msgType, payload, err := protocol.recv()
	if err != nil {
		return nil, err
	}

	if err := expect(msgWinners, msgType, payload); err != nil {
		return nil, err
	}

	return decodeWinners(payload)
}

func (protocol *Protocol) beginMessage(msgType byte) {
	protocol.buf = append(protocol.buf[:0], msgType, 0, 0, 0, 0)
}

func (protocol *Protocol) endMessage() error {
	payloadSize := len(protocol.buf) - headerSize
	if payloadSize > maxPayloadSize {
		return fmt.Errorf(
			"payload of %d bytes exceeds the %d bytes limit",
			payloadSize, maxPayloadSize,
		)
	}

	binary.BigEndian.PutUint32(protocol.buf[1:headerSize], uint32(payloadSize))
	return safe_socket.SendAll(protocol.conn, protocol.buf)
}

func (protocol *Protocol) recv() (byte, []byte, error) {
	header, err := safe_socket.RecvAll(protocol.conn, headerSize)
	if err != nil {
		return 0, nil, err
	}

	msgType := header[0]
	payloadSize := binary.BigEndian.Uint32(header[1:headerSize])
	if payloadSize > maxPayloadSize {
		return 0, nil, fmt.Errorf(
			"peer announced a payload of %d bytes, over the %d bytes limit",
			payloadSize, maxPayloadSize,
		)
	}

	payload, err := safe_socket.RecvAll(protocol.conn, int(payloadSize))
	if err != nil {
		return 0, nil, err
	}

	return msgType, payload, nil
}

func expect(expected byte, msgType byte, payload []byte) error {
	switch msgType {
	case expected:
		return nil
	case msgError:
		return fmt.Errorf("server reported an error: %s", payload)
	default:
		return fmt.Errorf("unexpected message type %#x, expected %#x", msgType, expected)
	}
}

func appendUint16(dst []byte, value uint16) []byte {
	var encoded [uint16Size]byte
	binary.BigEndian.PutUint16(encoded[:], value)
	return append(dst, encoded[:]...)
}

func appendUint32(dst []byte, value uint32) []byte {
	var encoded [uint32Size]byte
	binary.BigEndian.PutUint32(encoded[:], value)
	return append(dst, encoded[:]...)
}

func appendText(dst []byte, text []byte) []byte {
	dst = append(dst, byte(len(text)))
	return append(dst, text...)
}

func appendBet(dst []byte, bet bets.Bet) []byte {
	dst = appendText(dst, bet.FirstName)
	dst = appendText(dst, bet.LastName)
	dst = appendUint32(dst, bet.Document)
	dst = appendText(dst, bet.Birthdate)
	return appendUint32(dst, bet.Number)
}

func decodeWinners(payload []byte) ([]bets.Bet, error) {
	dec := decoder{payload: payload}

	amount, err := dec.uint32()
	if err != nil {
		return nil, err
	}

	maxAmount := (len(payload) - uint32Size) / minBetSize
	if int(amount) > maxAmount {
		return nil, fmt.Errorf(
			"winners message announces %d bets but its payload holds at most %d",
			amount, maxAmount,
		)
	}

	winners := make([]bets.Bet, 0, amount)
	for range int(amount) {
		winner, err := dec.bet()
		if err != nil {
			return nil, err
		}

		winners = append(winners, winner)
	}

	return winners, dec.expectEnd()
}

type decoder struct {
	payload []byte
	offset  int
}

func (dec *decoder) take(size int) ([]byte, error) {
	end := dec.offset + size
	if end > len(dec.payload) {
		return nil, fmt.Errorf(
			"payload ended after %d bytes while reading %d more",
			dec.offset, size,
		)
	}

	chunk := dec.payload[dec.offset:end]
	dec.offset = end
	return chunk, nil
}

func (dec *decoder) uint32() (uint32, error) {
	chunk, err := dec.take(uint32Size)
	if err != nil {
		return 0, err
	}

	return binary.BigEndian.Uint32(chunk), nil
}

func (dec *decoder) text() ([]byte, error) {
	size, err := dec.take(1)
	if err != nil {
		return nil, err
	}

	return dec.take(int(size[0]))
}

func (dec *decoder) bet() (bets.Bet, error) {
	firstName, err := dec.text()
	if err != nil {
		return bets.Bet{}, err
	}

	lastName, err := dec.text()
	if err != nil {
		return bets.Bet{}, err
	}

	document, err := dec.uint32()
	if err != nil {
		return bets.Bet{}, err
	}

	birthdate, err := dec.text()
	if err != nil {
		return bets.Bet{}, err
	}

	number, err := dec.uint32()
	if err != nil {
		return bets.Bet{}, err
	}

	return bets.Bet{
		FirstName: firstName,
		LastName:  lastName,
		Document:  document,
		Birthdate: birthdate,
		Number:    number,
	}, nil
}

func (dec *decoder) expectEnd() error {
	if dec.offset != len(dec.payload) {
		return fmt.Errorf(
			"payload has %d trailing bytes",
			len(dec.payload)-dec.offset,
		)
	}

	return nil
}
