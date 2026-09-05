import threading


class AgencyQuorum:
    """Coordina la espera del minimo de agencias necesario para sortear.

    Cada hilo de cliente registra su agencia al recibir el mensaje de fin y
    queda bloqueado hasta que la cantidad de agencias que terminaron alcanza el
    minimo configurado. El conjunto de agencias lo tocan todos los hilos, asi
    que solo se accede a el con el lock de la condicion tomado.
    """

    def __init__(self, minimum: int) -> None:
        self.minimum = minimum
        self._finished_agencies: set[int] = set()
        self._condition = threading.Condition()

    def register(self, agency_id: int) -> int:
        """Anota que una agencia termino de cargar sus apuestas.

        Devuelve cuantas agencias terminaron hasta el momento. Se cuentan
        agencias distintas y no mensajes recibidos, que es lo que pide el
        enunciado: una agencia que se reconectara no debe contar dos veces.
        """
        with self._condition:
            self._finished_agencies.add(agency_id)

            self._condition.notify_all()

            return len(self._finished_agencies)

    def wait_until_reached(self) -> None:
        """Bloquea al hilo hasta que se alcance el quorum.

        Separar el registro de la espera es seguro porque el predicado es
        monotono: una vez alcanzado el quorum no vuelve a perderse. Si se
        alcanzara entre las dos llamadas, `wait_for` lo detecta al evaluar el
        predicado bajo el lock y devuelve sin bloquear, con lo cual no hay
        forma de perderse la notificacion.
        """
        with self._condition:
            self._condition.wait_for(self._is_reached)

    def _is_reached(self) -> bool:
        """Predicado del quorum. Se evalua con el lock de la condicion tomado."""
        return len(self._finished_agencies) >= self.minimum
