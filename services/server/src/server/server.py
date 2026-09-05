import socket
import threading

import logger
import protocol

from .bet_store import BetStore
from .quorum import AgencyQuorum


class Server:
    def __init__(
        self,
        server_host: str,
        server_port: int,
        bet_store: BetStore,
        quorum: AgencyQuorum,
    ) -> None:
        self.server_host = server_host
        self.server_port = server_port
        self.bet_store = bet_store
        self.quorum = quorum
        self._client_threads: list[threading.Thread] = []

    def run(self) -> None:
        action = "accept-connection"
        try:
            with socket.socket(socket.AF_INET, socket.SOCK_STREAM) as server_socket:
                server_socket.bind((self.server_host, self.server_port))
                server_socket.listen()
                while True:
                    try:
                        logger.info(action, logger.LogResult.in_progress)
                        client_socket, _ = server_socket.accept()
                    except Exception as e:
                        logger.error(action, logger.LogResult.fail, "err", e)
                        raise e
                    logger.info(action, logger.LogResult.success)

                    self._spawn_client_thread(client_socket)
        finally:
            self._join_client_threads()

    def _spawn_client_thread(self, client_socket: socket.socket) -> None:
        self._client_threads = [t for t in self._client_threads if t.is_alive()]

        thread = threading.Thread(target=self._handle_client, args=(client_socket,))
        self._client_threads.append(thread)
        thread.start()

    def _join_client_threads(self) -> None:
        for thread in self._client_threads:
            thread.join()

        self._client_threads = []

    def _handle_client(self, client_socket: socket.socket) -> None:
        action = "handle-client"
        agency_id = None
        bets_amount = 0

        with client_socket:
            try:
                agency_id = self._recv_hello(client_socket)
                logger.info(
                    action, logger.LogResult.in_progress, "agency-id", agency_id
                )

                bets_amount = self._recv_bets(client_socket, agency_id)
                self._await_quorum(agency_id)
                self._send_winners(client_socket, agency_id)

                logger.info(
                    action,
                    logger.LogResult.success,
                    "agency-id",
                    agency_id,
                    "bets-amount",
                    bets_amount,
                )
            except Exception as e:
                logger.error(
                    action,
                    logger.LogResult.fail,
                    "agency-id",
                    agency_id,
                    "bets-amount",
                    bets_amount,
                    "err",
                    e,
                )
                self._notify_error(client_socket, e)

    @staticmethod
    def _recv_hello(client_socket: socket.socket) -> int:
        msg_type, payload = protocol.recv_message(client_socket)
        if msg_type != protocol.MSG_HELLO:
            raise protocol.ProtocolError(
                f"expected a hello message, got type {msg_type:#04x}"
            )

        return protocol.decode_hello(payload)

    def _recv_bets(self, client_socket: socket.socket, agency_id: int) -> int:
        bets_amount = 0

        while True:
            msg_type, payload = protocol.recv_message(client_socket)

            if msg_type == protocol.MSG_FINISHED:
                return bets_amount

            if msg_type != protocol.MSG_BATCH:
                raise protocol.ProtocolError(
                    f"expected a batch or a finished message, got type {msg_type:#04x}"
                )

            bets = protocol.decode_batch(payload, agency_id)
            self.bet_store.store_bets(bets)
            protocol.send_message(client_socket, protocol.MSG_ACK)
            bets_amount += len(bets)

    def _await_quorum(self, agency_id: int) -> None:
        action = "await-quorum"

        finished_agencies = self.quorum.register(agency_id)
        logger.info(
            action,
            logger.LogResult.in_progress,
            "agency-id",
            agency_id,
            "finished-agencies",
            finished_agencies,
            "quorum-min",
            self.quorum.minimum,
        )

        self.quorum.wait_until_reached()

        logger.info(action, logger.LogResult.success, "agency-id", agency_id)

    def _send_winners(self, client_socket: socket.socket, agency_id: int) -> None:
        action = "draw-winners"
        logger.info(action, logger.LogResult.in_progress, "agency-id", agency_id)

        winners = self.bet_store.draw_winners(agency_id)

        protocol.send_message(
            client_socket, protocol.MSG_WINNERS, protocol.encode_winners(winners)
        )

        logger.info(
            action,
            logger.LogResult.success,
            "agency-id",
            agency_id,
            "winners-amount",
            len(winners),
        )

    @staticmethod
    def _notify_error(client_socket: socket.socket, error: Exception) -> None:
        try:
            protocol.send_message(
                client_socket, protocol.MSG_ERROR, str(error).encode("utf-8")
            )
        except Exception:
            pass
