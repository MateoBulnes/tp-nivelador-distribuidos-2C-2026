import os
import sys
import tempfile

import logger
import server
from lottery import Lottery


class ConfigError(Exception):
    """Falta una variable de entorno obligatoria o su valor es invalido."""


def required_env(key: str) -> str:
    value = os.environ.get(key, "")
    if not value:
        raise ConfigError(f"{key} environment variable is required")

    return value


def required_env_positive_int(key: str) -> int:
    value = required_env(key)

    try:
        parsed = int(value)
    except ValueError as e:
        raise ConfigError(f"{key} must be an integer: {e}") from e

    if parsed < 1:
        raise ConfigError(f"{key} must be greater than zero, got {parsed}")

    return parsed


def load_config() -> tuple[str, int, int]:
    return (
        required_env("SERVER_HOST"),
        required_env_positive_int("SERVER_PORT"),
        required_env_positive_int("AGENCY_QUORUM_MIN"),
    )


def main():
    logger.init()

    try:
        server_host, server_port, agency_quorum_min = load_config()
    except ConfigError as e:
        logger.error("load-config", logger.LogResult.fail, "err", e)
        return 1

    storage_fd, storage_path = tempfile.mkstemp(prefix="bets-", suffix=".csv")
    os.close(storage_fd)

    try:
        s = server.Server(
            server_host,
            server_port,
            server.BetStore(Lottery(storage_path)),
            server.AgencyQuorum(agency_quorum_min),
        )
        s.run()
    except Exception as e:
        logger.error("server-run", logger.LogResult.fail, "err", e)
        return 1
    finally:
        os.remove(storage_path)

    return 0


if __name__ == "__main__":
    sys.exit(main())
