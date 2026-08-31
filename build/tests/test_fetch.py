# build/tests/test_fetch.py
from pathlib import Path
import pytest
import urllib.error
import fetch


def test_rejects_http(tmp_path):
    with pytest.raises(fetch.FetchError, match="https"):
        fetch.fetch("http://classics.mit.edu/Aristotle/poetics.mb.txt", tmp_path)


def test_caches_and_reuses(tmp_path, monkeypatch):
    calls = []

    def fake_get(url):
        calls.append(url)
        return "Provided by The Internet Classics Archive.\nтело"

    monkeypatch.setattr(fetch, "_get", fake_get)
    url = "https://classics.mit.edu/Aristotle/poetics.mb.txt"

    first = fetch.fetch(url, tmp_path)
    second = fetch.fetch(url, tmp_path)

    assert first == second
    assert calls == [url], "второй вызов не должен ходить в сеть"


def test_detects_homepage_substitution(tmp_path, monkeypatch):
    monkeypatch.setattr(fetch, "_get", lambda url: "<HTML><BODY>главная</BODY></HTML>")
    with pytest.raises(fetch.FetchError, match="подмен"):
        fetch.fetch("https://classics.mit.edu/Aristotle/poetics.mb.txt", tmp_path)


def test_handles_http_error(tmp_path, monkeypatch):
    url = "https://classics.mit.edu/Aristotle/nonexistent.txt"

    def fake_get_404(url):
        raise urllib.error.HTTPError(url, 404, "Not Found", {}, None)

    monkeypatch.setattr(fetch, "_get", fake_get_404)

    with pytest.raises(fetch.FetchError, match="HTTP 404"):
        fetch.fetch(url, tmp_path)

    # Проверяем, что в кэш ничего не записалось
    from pathlib import Path
    cache_files = list(Path(tmp_path).glob("*.txt"))
    assert len(cache_files) == 0, "кэш не должен содержать файлы при ошибке"


def test_handles_url_error(tmp_path, monkeypatch):
    url = "https://invalid.example.com/text.txt"

    def fake_get_error(url):
        raise urllib.error.URLError("Network is unreachable")

    monkeypatch.setattr(fetch, "_get", fake_get_error)

    with pytest.raises(fetch.FetchError, match="ошибка при соединении"):
        fetch.fetch(url, tmp_path)

    # Проверяем, что в кэш ничего не записалось
    from pathlib import Path
    cache_files = list(Path(tmp_path).glob("*.txt"))
    assert len(cache_files) == 0, "кэш не должен содержать файлы при ошибке"
