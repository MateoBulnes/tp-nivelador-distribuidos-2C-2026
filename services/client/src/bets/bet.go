package bets

import (
	"fmt"
	"math"
)

const recordFieldsAmount = 5
const maxTextFieldSize = 255

type Bet struct {
	FirstName []byte
	LastName  []byte
	Birthdate []byte
	Document  uint32
	Number    uint32
}

func (bet Bet) Validate() error {
	if err := validateTextField("first_name", bet.FirstName); err != nil {
		return err
	}
	if err := validateTextField("last_name", bet.LastName); err != nil {
		return err
	}
	return validateTextField("birthdate", bet.Birthdate)
}

func validateTextField(name string, field []byte) error {
	if len(field) > maxTextFieldSize {
		return fmt.Errorf(
			"%s is %d bytes long and the protocol allows up to %d",
			name, len(field), maxTextFieldSize,
		)
	}

	return nil
}

func parseRecord(record []byte) (Bet, error) {
	fields, err := splitFields(record)
	if err != nil {
		return Bet{}, err
	}

	document, err := parseUint32(fields[2])
	if err != nil {
		return Bet{}, fmt.Errorf("invalid document: %w", err)
	}

	number, err := parseUint32(fields[4])
	if err != nil {
		return Bet{}, fmt.Errorf("invalid number: %w", err)
	}

	return Bet{
		FirstName: fields[0],
		LastName:  fields[1],
		Document:  document,
		Birthdate: fields[3],
		Number:    number,
	}, nil
}

func splitFields(record []byte) ([recordFieldsAmount][]byte, error) {
	var fields [recordFieldsAmount][]byte

	start := 0
	amount := 0
	for i := 0; i <= len(record); i++ {
		if i < len(record) && record[i] != ',' {
			continue
		}

		if amount == recordFieldsAmount {
			return fields, fmt.Errorf("record has more than %d fields", recordFieldsAmount)
		}

		fields[amount] = record[start:i]
		amount++
		start = i + 1
	}

	if amount != recordFieldsAmount {
		return fields, fmt.Errorf("record has %d fields, expected %d", amount, recordFieldsAmount)
	}

	return fields, nil
}

func parseUint32(field []byte) (uint32, error) {
	if len(field) == 0 {
		return 0, fmt.Errorf("field is empty")
	}

	var value uint64
	for _, char := range field {
		if char < '0' || char > '9' {
			return 0, fmt.Errorf("%q is not a number", field)
		}

		value = value*10 + uint64(char-'0')
		if value > math.MaxUint32 {
			return 0, fmt.Errorf("%q does not fit in a 32 bit unsigned integer", field)
		}
	}

	return uint32(value), nil
}
