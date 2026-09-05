import socket

import logger
import protocol
from lottery import Lottery


class Server:
    def __init__(
        self, server_host: str, server_port: int, lottery: Lottery
    ) -> None:
        self.server_host = server_host
        self.server_port = server_port
        self.lottery = lottery

    def run(self) -> None:
        action = "accept-connection"
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

                self._handle_client(client_socket)

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
            self.lottery.store_bets(bets)
            protocol.send_message(client_socket, protocol.MSG_ACK)
            bets_amount += len(bets)

    def _send_winners(self, client_socket: socket.socket, agency_id: int) -> None:
        action = "draw-winners"
        logger.info(action, logger.LogResult.in_progress, "agency-id", agency_id)

        winners = [
            bet
            for bet in self.lottery.load_bets()
            if bet.agency_id == agency_id and self.lottery.has_won(bet)
        ]

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
