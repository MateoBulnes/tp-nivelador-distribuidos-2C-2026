import os
import select
import socket
import threading

import logger
import protocol

from .bet_store import BetStore
from .quorum import AgencyQuorum


class _ClientConnection:
    """Un cliente aceptado: su socket y el hilo que lo atiende."""

    def __init__(self, client_socket: socket.socket) -> None:
        self.socket = client_socket
        self.thread = None


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
        self._connections: list[_ClientConnection] = []
        self._connections_lock = threading.Lock()
        self._shutdown = threading.Event()
        self._wakeup_reader, self._wakeup_writer = os.pipe()
        os.set_blocking(self._wakeup_writer, False)

    def request_shutdown(self) -> None:
        """Pide el cierre del servidor. Corre dentro del handler de SIGTERM."""
        self._shutdown.set()

        try:
            os.write(self._wakeup_writer, b"\0")
        except OSError:
            pass

    def close(self) -> None:
        """Libera el pipe de despertar, que tambien son descriptores adquiridos."""
        os.close(self._wakeup_reader)
        os.close(self._wakeup_writer)

    def run(self) -> None:
        try:
            with socket.socket(socket.AF_INET, socket.SOCK_STREAM) as server_socket:
                server_socket.bind((self.server_host, self.server_port))
                server_socket.listen()

                self._accept_loop(server_socket)
        finally:
            self._shutdown_connections()

    def _accept_loop(self, server_socket: socket.socket) -> None:
        action = "accept-connection"

        while not self._shutdown.is_set():
            logger.info(action, logger.LogResult.in_progress)

            if not self._wait_for_connection(server_socket):
                return

            try:
                client_socket, _ = server_socket.accept()
            except Exception as e:
                logger.error(action, logger.LogResult.fail, "err", e)
                raise e
            logger.info(action, logger.LogResult.success)

            self._spawn_client_thread(client_socket)

    def _wait_for_connection(self, server_socket: socket.socket) -> bool:
        """Espera una conexion pendiente o el pedido de cierre.

        Devuelve True si hay una conexion para aceptar y False si hay que cerrar.
        """
        readable, _, _ = select.select(
            [server_socket, self._wakeup_reader], [], []
        )

        return self._wakeup_reader not in readable

    def _spawn_client_thread(self, client_socket: socket.socket) -> None:
        connection = _ClientConnection(client_socket)
        connection.thread = threading.Thread(
            target=self._handle_client, args=(connection,)
        )

        with self._connections_lock:
            self._connections.append(connection)

        try:
            connection.thread.start()
        except Exception as e:
            self._close_connection(connection)
            raise e

    def _shutdown_connections(self) -> None:
        """Despierta a los hilos de cliente bloqueados y espera a que terminen.

        Los hilos pueden estar bloqueados en dos lugares distintos y cada uno
        necesita su propio mecanismo: los que esperan el sorteo salen por el
        `abort` del quorum, y los que esperan mensajes de su cliente salen
        porque se les rompe la conexion.

        El join se hace fuera del lock a proposito: cada hilo toma ese mismo
        lock para desregistrarse al terminar, asi que esperarlos con el lock
        tomado seria un deadlock inmediato.
        """
        self.quorum.abort()

        with self._connections_lock:
            for connection in self._connections:
                self._break_connection(connection.socket)

            threads = [connection.thread for connection in self._connections]

        for thread in threads:
            thread.join()

    @staticmethod
    def _break_connection(client_socket: socket.socket) -> None:
        """Rompe una conexion para desbloquear al hilo que la esta leyendo."""
        try:
            client_socket.shutdown(socket.SHUT_RDWR)
        except OSError:
            # El peer ya cerro su lado: la conexion ya esta rota, que es
            # justamente el efecto que se buscaba.
            pass

    def _close_connection(self, connection: _ClientConnection) -> None:
        """Cierra el socket de una conexion y la saca del registro."""
        with self._connections_lock:
            self._connections.remove(connection)

            connection.socket.close()

    def _handle_client(self, connection: _ClientConnection) -> None:
        action = "handle-client"
        client_socket = connection.socket
        agency_id = None
        bets_amount = 0

        try:
            agency_id = self._recv_hello(client_socket)
            logger.info(action, logger.LogResult.in_progress, "agency-id", agency_id)

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
            if self._shutdown.is_set():
                logger.info(
                    "shutdown-client",
                    logger.LogResult.success,
                    "agency-id",
                    agency_id,
                    "bets-amount",
                    bets_amount,
                )
            else:
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
        finally:
            self._close_connection(connection)

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
