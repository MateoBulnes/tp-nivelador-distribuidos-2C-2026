import socket

import safe_socket
from lottery import Bet

MSG_HELLO = 0x01
MSG_BET = 0x02
MSG_ACK = 0x03
MSG_FINISHED = 0x04
MSG_WINNERS = 0x05
MSG_ERROR = 0x06

_BYTE_ORDER = "big"
_ENCODING = "utf-8"

_UINT16_SIZE = 2
_UINT32_SIZE = 4
_MAX_UINT32 = 2**32 - 1

_HEADER_SIZE = 1 + _UINT32_SIZE

_MAX_PAYLOAD_SIZE = 64 * 1024

_MAX_TEXT_FIELD_SIZE = 255


class ProtocolError(Exception):


def send_message(sock: socket.socket, msg_type: int, payload: bytes = b"") -> None:
    if len(payload) > _MAX_PAYLOAD_SIZE:
        raise ProtocolError(
            f"payload of {len(payload)} bytes exceeds the {_MAX_PAYLOAD_SIZE} bytes limit"
        )

    header = msg_type.to_bytes(1, _BYTE_ORDER) + len(payload).to_bytes(
        _UINT32_SIZE, _BYTE_ORDER
    )
    safe_socket.send_all(sock, header + payload)


def recv_message(sock: socket.socket) -> tuple[int, bytes]:
    header = safe_socket.recv_all(sock, _HEADER_SIZE)
    msg_type = header[0]
    payload_size = int.from_bytes(header[1:], _BYTE_ORDER)

    if payload_size > _MAX_PAYLOAD_SIZE:
        raise ProtocolError(
            f"peer announced a payload of {payload_size} bytes, "
            f"over the {_MAX_PAYLOAD_SIZE} bytes limit"
        )

    return msg_type, safe_socket.recv_all(sock, payload_size)


def decode_hello(payload: bytes) -> int:
    decoder = _Decoder(payload)
    agency_id = decoder.uint16()
    decoder.expect_end()
    return agency_id


def decode_bet(payload: bytes, agency_id: int) -> Bet:
    decoder = _Decoder(payload)
    first_name = decoder.text()
    last_name = decoder.text()
    document = decoder.uint32()
    birthdate = decoder.text()
    number = decoder.uint32()
    decoder.expect_end()

    return Bet(agency_id, first_name, last_name, document, birthdate, number)


def encode_winners(winners: list[Bet]) -> bytes:
    parts = [_encode_uint32(len(winners))]
    for winner in winners:
        parts.append(_encode_bet(winner))

    return b"".join(parts)


def _encode_bet(bet: Bet) -> bytes:
    return b"".join(
        [
            _encode_text(bet.first_name),
            _encode_text(bet.last_name),
            _encode_uint32(bet.document),
            _encode_text(bet.birthdate),
            _encode_uint32(bet.number),
        ]
    )


def _encode_uint32(value: int) -> bytes:
    if value < 0 or value > _MAX_UINT32:
        raise ProtocolError(f"{value} does not fit in a 32 bit unsigned integer")

    return value.to_bytes(_UINT32_SIZE, _BYTE_ORDER)


def _encode_text(text: str) -> bytes:
    encoded = text.encode(_ENCODING)
    if len(encoded) > _MAX_TEXT_FIELD_SIZE:
        raise ProtocolError(
            f"text field is {len(encoded)} bytes long and the protocol "
            f"allows up to {_MAX_TEXT_FIELD_SIZE}"
        )

    return len(encoded).to_bytes(1, _BYTE_ORDER) + encoded


class _Decoder:

    def __init__(self, payload: bytes) -> None:
        self._payload = payload
        self._offset = 0

    def uint16(self) -> int:
        return int.from_bytes(self._take(_UINT16_SIZE), _BYTE_ORDER)

    def uint32(self) -> int:
        return int.from_bytes(self._take(_UINT32_SIZE), _BYTE_ORDER)

    def text(self) -> str:
        size = self._take(1)[0]
        return self._take(size).decode(_ENCODING)

    def expect_end(self) -> None:
        if self._offset != len(self._payload):
            raise ProtocolError(
                f"payload has {len(self._payload) - self._offset} trailing bytes"
            )

    def _take(self, size: int) -> bytes:
        end = self._offset + size
        if end > len(self._payload):
            raise ProtocolError(
                f"payload ended after {self._offset} bytes while reading {size} more"
            )

        chunk = self._payload[self._offset : end]
        self._offset = end
        return chunk
