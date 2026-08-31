# build/tests/test_fetch.py
from pathlib import Path
import pytest
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
