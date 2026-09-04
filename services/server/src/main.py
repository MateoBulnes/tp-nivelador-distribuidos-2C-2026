import os
import sys
import tempfile

import logger
import server
from lottery import Lottery

SERVER_HOST = os.environ["SERVER_HOST"]
SERVER_PORT = int(os.environ["SERVER_PORT"])


def main():
    logger.init()

    storage_fd, storage_path = tempfile.mkstemp(prefix="bets-", suffix=".csv")
    os.close(storage_fd)

    try:
        s = server.Server(SERVER_HOST, SERVER_PORT, Lottery(storage_path))
        s.run()
    except Exception as e:
        logger.error("server-run", logger.LogResult.fail, "err", e)
        return 1
    finally:
        os.remove(storage_path)

    return 0


if __name__ == "__main__":
    sys.exit(main())
