import socket


class ConnectionClosedError(Exception):
    """El peer cerro la conexion antes de completar el mensaje esperado."""


def recv_all(sock: socket.socket, size: int) -> bytes:
    buffer = bytearray()

    while len(buffer) < size:
        chunk = sock.recv(size - len(buffer))

        if not chunk:
            raise ConnectionClosedError(
                f"connection closed after receiving {len(buffer)} of {size} bytes"
            )

        buffer.extend(chunk)

    return bytes(buffer)


def send_all(sock: socket.socket, data: bytes) -> None:
    view = memoryview(data)
    sent = 0

    while sent < len(data):
        sent += sock.send(view[sent:])
