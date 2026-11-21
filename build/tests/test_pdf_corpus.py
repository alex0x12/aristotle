import pytest

from pdf_corpus import load_works


VOL1 = (
    "metaphysics:\n"
    "  title: \"Метафизика\"\n"
    "  volume: 1\n"
    "  first: 66\n"
    "  last: 369\n"
    "  expect: \"КНИГА ПЕРВАЯ\"\n"
    "  translator: \"А. В. Кубицкий\"\n"
    "  year: \"1934\"\n"
    "  group: metaphysics\n"
    "  heads: [\"МЕТАФИЗИКА\"]\n"
)


def test_loads_valid_directory(tmp_path):
    (tmp_path / "vol1.yaml").write_text(VOL1, encoding="utf-8")

    works = load_works(tmp_path)

    assert list(works) == ["metaphysics"]
    cfg = works["metaphysics"]
    assert cfg["title"] == "Метафизика"
    assert cfg["volume"] == 1
    assert cfg["first"] == 66
    assert cfg["last"] == 369
    assert cfg["expect"] == "КНИГА ПЕРВАЯ"
    assert cfg["translator"] == "А. В. Кубицкий"
    assert cfg["year"] == "1934"
    assert cfg["group"] == "metaphysics"
    assert cfg["heads"] == ["МЕТАФИЗИКА"]


def test_style_and_ocr_defaults(tmp_path):
    (tmp_path / "vol1.yaml").write_text(VOL1, encoding="utf-8")

    cfg = load_works(tmp_path)["metaphysics"]

    assert cfg["style"] == "glava"
    assert cfg["ocr"] is False


def test_keeps_explicit_style_and_ocr(tmp_path):
    (tmp_path / "vol4.yaml").write_text(
        "magna-moralia:\n"
        "  title: \"Большая этика\"\n"
        "  volume: 4\n"
        "  first: 296\n"
        "  last: 374\n"
        "  expect: \"КНИГА ПЕРВАЯ\"\n"
        "  translator: \"Т. А. Миллер\"\n"
        "  year: \"1983\"\n"
        "  group: ethics\n"
        "  heads: [\"БОЛЬШАЯ ЭТИКА\"]\n"
        "  style: numbered\n"
        "  ocr: true\n",
        encoding="utf-8",
    )

    cfg = load_works(tmp_path)["magna-moralia"]

    assert cfg["style"] == "numbered"
    assert cfg["ocr"] is True


def test_duplicate_slug_in_two_files_raises(tmp_path):
    (tmp_path / "a.yaml").write_text(VOL1, encoding="utf-8")
    (tmp_path / "b.yaml").write_text(VOL1, encoding="utf-8")

    with pytest.raises(ValueError) as err:
        load_works(tmp_path)

    message = str(err.value)
    assert "metaphysics" in message
    assert "a.yaml" in message
    assert "b.yaml" in message


def test_missing_required_field_raises(tmp_path):
    (tmp_path / "vol1.yaml").write_text(
        "metaphysics:\n"
        "  title: \"Метафизика\"\n"
        "  volume: 1\n"
        "  first: 66\n"
        "  last: 369\n"
        "  expect: \"КНИГА ПЕРВАЯ\"\n"
        "  year: \"1934\"\n"
        "  group: metaphysics\n"
        "  heads: [\"МЕТАФИЗИКА\"]\n",
        encoding="utf-8",
    )

    with pytest.raises(ValueError) as err:
        load_works(tmp_path)

    message = str(err.value)
    assert "vol1.yaml" in message
    assert "metaphysics" in message
    assert "translator" in message


def test_first_must_not_exceed_last(tmp_path):
    (tmp_path / "vol1.yaml").write_text(
        VOL1.replace("first: 66", "first: 400"), encoding="utf-8")

    with pytest.raises(ValueError, match="first"):
        load_works(tmp_path)


def test_page_numbers_must_be_integers(tmp_path):
    (tmp_path / "vol1.yaml").write_text(
        VOL1.replace("first: 66", "first: \"66\""), encoding="utf-8")

    with pytest.raises(ValueError, match="first"):
        load_works(tmp_path)


def test_heads_must_be_non_empty_list_of_strings(tmp_path):
    (tmp_path / "vol1.yaml").write_text(
        VOL1.replace("heads: [\"МЕТАФИЗИКА\"]", "heads: []"), encoding="utf-8")

    with pytest.raises(ValueError, match="heads"):
        load_works(tmp_path)
