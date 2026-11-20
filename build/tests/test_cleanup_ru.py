import cleanup_ru


def test_suggest_uses_frequency_for_n_p_confusion():
    text = " ".join(["познанию"] * 5 + ["познапию"])
    assert ("познапию", "познанию", 1) in cleanup_ru.suggest(text)


def test_suggest_skips_forms_that_are_both_frequent():
    text = " ".join(["познанию"] * 5 + ["познапию"] * 5)
    assert cleanup_ru.suggest(text) == []


def test_suggest_fixes_greek_pi_from_frequency():
    text = " ".join(["познание"] * 5 + ["познаπие"])
    assert ("познаπие", "познание", 1) in cleanup_ru.suggest(text)


def test_suggest_fixes_latin_letter_inside_russian_word():
    text = " ".join(["познание"] * 5 + ["познaние"])
    assert ("познaние", "познание", 1) in cleanup_ru.suggest(text)


def test_suggest_leaves_unknown_terms_alone():
    text = "энтелехпя энтелехпя"
    assert all(bad != "энтелехпя" for bad, _, _ in cleanup_ru.suggest(text))


def chapter(root, name, text):
    path = root / name
    path.parent.mkdir(parents=True, exist_ok=True)
    path.write_text(text, encoding="utf-8")
    return path


def test_fix_only_applies_unambiguous_replacement(tmp_path):
    clean = chapter(tmp_path, "a.md", "\n".join(["познанию"] * 5 + ["познапию"]) + "\n")
    ambiguous = chapter(tmp_path, "b.md", "\n".join(["нога"] * 5 + ["пога"] * 5) + "\n")
    term = chapter(tmp_path, "c.md", "энтелехпя\n")

    rc = cleanup_ru.main(["--report", str(tmp_path), "--fix"])

    assert rc == 0
    assert "познапию" not in clean.read_text(encoding="utf-8")
    assert clean.read_text(encoding="utf-8").count("познанию") == 6
    assert "пога" in ambiguous.read_text(encoding="utf-8")
    assert term.read_text(encoding="utf-8") == "энтелехпя\n"


def test_report_does_not_write_files(tmp_path):
    path = chapter(tmp_path, "a.md", "\n".join(["познанию"] * 5 + ["познапию"]) + "\n")
    before = path.read_bytes()
    assert cleanup_ru.main(["--report", str(tmp_path)]) == 0
    assert path.read_bytes() == before


def test_fix_preserves_case_and_punctuation(tmp_path):
    path = chapter(tmp_path, "a.md", "Познанию, познанию, познанию, познанию, познанию. Познапию!\n")
    assert cleanup_ru.main(["--report", str(tmp_path), "--fix"]) == 0
    assert path.read_text(encoding="utf-8").endswith("Познанию!\n")
