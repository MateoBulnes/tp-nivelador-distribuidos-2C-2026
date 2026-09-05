import threading

from lottery import Bet, Lottery


class BetStore:

    def __init__(self, lottery: Lottery) -> None:
        self._lottery = lottery
        self._lock = threading.Lock()

    def store_bets(self, bets: list[Bet]) -> None:
        with self._lock:
            self._lottery.store_bets(bets)

    def draw_winners(self, agency_id: int) -> list[Bet]:
        """Devuelve las apuestas ganadoras de una agencia.

        La comprension se evalua completa dentro de la seccion critica a
        proposito: `load_bets` es un generador y, devuelto sin consumir,
        recorreria el archivo de forma perezosa una vez liberado el lock.
        """
        with self._lock:
            return [
                bet
                for bet in self._lottery.load_bets()
                if bet.agency_id == agency_id and self._lottery.has_won(bet)
            ]
