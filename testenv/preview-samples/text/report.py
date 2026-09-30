"""Summarizes the transfers of a day."""
from dataclasses import dataclass


@dataclass
class Transfer:
    name: str
    size: int


def total(transfers: list[Transfer]) -> int:
    return sum(t.size for t in transfers)


if __name__ == "__main__":
    print(total([Transfer("a.txt", 10), Transfer("b.bin", 32)]))
