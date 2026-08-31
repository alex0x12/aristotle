import hashlib
import urllib.request
from pathlib import Path

UA = "aristotle-corpus-builder/0.1 (personal research)"
TIMEOUT = 60


class FetchError(Exception):
    pass


def _get(url: str) -> str:
    req = urllib.request.Request(url, headers={"User-Agent": UA})
    with urllib.request.urlopen(req, timeout=TIMEOUT) as resp:
        return resp.read().decode("utf-8", errors="replace")


def _cache_path(url: str, cache_dir: Path) -> Path:
    name = hashlib.sha256(url.encode()).hexdigest()[:16]
    return Path(cache_dir) / f"{name}.txt"


def fetch(url: str, cache_dir: Path) -> str:
    if not url.startswith("https://"):
        raise FetchError(
            f"только https: по http сайт молча отдаёт главную страницу — {url}")

    cache_dir = Path(cache_dir)
    cache_dir.mkdir(parents=True, exist_ok=True)
    path = _cache_path(url, cache_dir)
    if path.exists():
        return path.read_text(encoding="utf-8")

    body = _get(url)
    if "<HTML" in body.upper()[:400]:
        raise FetchError(
            f"подмена главной страницей вместо текста: {url}")
    path.write_text(body, encoding="utf-8")
    return body
