#!/usr/bin/env python3
"""Genera un archivo de composicion con la cantidad de clientes indicada.

Uso: ./generate-compose.py <output_file> <clients_amount>

El archivo generado se escribe pensado para vivir en la raiz del repositorio,
porque los contextos de build y los volumenes se declaran con rutas relativas a
la ubicacion del archivo de composicion.

El archivo de salida es un argumento obligatorio y no tiene valor por defecto a
proposito: un default que apunte al `docker-compose.yaml` del repositorio
invitaria a pisarlo por accidente.
"""

import sys
from pathlib import Path

SERVER_NAME = "server"
SERVER_PORT = 5678
SERVER_BUILD_CONTEXT = "./services/server"
CLIENT_BUILD_CONTEXT = "./services/client"

BATCH_SIZE = 16

INPUT_DIR = "input"
OUTPUT_DIR = "output"
INPUT_MOUNT = "/input"
OUTPUT_MOUNT = "/output"
INPUT_FILE_NAME = "input-{agency_id}.csv"
OUTPUT_FILE_NAME = "output-{agency_id}.csv"


class UsageError(Exception):
    """Los argumentos de linea de comandos son invalidos."""


def parse_positive_int(value: str, name: str) -> int:
    try:
        parsed = int(value)
    except ValueError as e:
        raise UsageError(f"{name} must be an integer: {e}") from e

    if parsed < 1:
        raise UsageError(f"{name} must be greater than zero, got {parsed}")

    return parsed


def render_server(clients_amount: int) -> str:
    """El quorum se deriva de la cantidad de clientes.

    Se deriva y no se fija para que el archivo generado sea coherente por
    construccion: un quorum mayor que la cantidad de agencias dejaria al sistema
    esperando un sorteo que no puede ocurrir.
    """
    return f"""  {SERVER_NAME}:
    build:
      context: {SERVER_BUILD_CONTEXT}
      dockerfile: Dockerfile
    container_name: {SERVER_NAME}
    ports:
      - "{SERVER_PORT}:{SERVER_PORT}"
    environment:
      - PYTHONUNBUFFERED=1
      - AGENCY_QUORUM_MIN={clients_amount}
      - SERVER_HOST={SERVER_NAME}
      - SERVER_PORT={SERVER_PORT}
"""


def render_client(agency_id: int) -> str:
    input_file = f"{INPUT_MOUNT}/{INPUT_FILE_NAME.format(agency_id=agency_id)}"
    output_file = f"{OUTPUT_MOUNT}/{OUTPUT_FILE_NAME.format(agency_id=agency_id)}"

    return f"""  client_{agency_id}:
    build:
      context: {CLIENT_BUILD_CONTEXT}
      dockerfile: Dockerfile
    container_name: client_{agency_id}
    depends_on:
      - {SERVER_NAME}
    environment:
      - AGENCY_ID={agency_id}
      - SERVER_HOST={SERVER_NAME}
      - SERVER_PORT={SERVER_PORT}
      - INPUT_FILE={input_file}
      - OUTPUT_FILE={output_file}
      - BATCH_SIZE={BATCH_SIZE}
    volumes:
      - ./{INPUT_DIR}:{INPUT_MOUNT}:ro
      - ./{OUTPUT_DIR}:{OUTPUT_MOUNT}
"""


def render_compose(clients_amount: int) -> str:
    blocks = [render_server(clients_amount)]
    blocks.extend(render_client(agency_id) for agency_id in range(clients_amount))

    return "services:\n" + "\n".join(blocks)


def warn_about_missing_inputs(clients_amount: int) -> None:
    """Avisa por stderr si falta el archivo de entrada de alguna agencia."""
    repository_root = Path(__file__).resolve().parent
    missing = [
        agency_id
        for agency_id in range(clients_amount)
        if not (
            repository_root
            / INPUT_DIR
            / INPUT_FILE_NAME.format(agency_id=agency_id)
        ).is_file()
    ]

    if missing:
        agencies = ", ".join(str(agency_id) for agency_id in missing)
        print(
            f"warning: no input file under {INPUT_DIR}/ for agencies {agencies}",
            file=sys.stderr,
        )


def main(argv: list[str]) -> int:
    try:
        if len(argv) != 3:
            raise UsageError(f"usage: {argv[0]} <output_file> <clients_amount>")

        output_file = Path(argv[1])
        clients_amount = parse_positive_int(argv[2], "clients_amount")
    except UsageError as e:
        print(f"error: {e}", file=sys.stderr)
        return 1

    warn_about_missing_inputs(clients_amount)

    content = render_compose(clients_amount)
    try:
        written = output_file.write_text(content, encoding="utf-8")
    except OSError as e:
        print(f"error: could not write {output_file}: {e}", file=sys.stderr)
        return 1

    if written != len(content):
        print(
            f"error: short write on {output_file}: "
            f"{written} of {len(content)} characters written",
            file=sys.stderr,
        )
        return 1

    print(f"{output_file}: {clients_amount} client(s) generated")
    return 0


if __name__ == "__main__":
    sys.exit(main(sys.argv))
