// Package fsutil — запись файлов пользователя без полузаписанного состояния.
package fsutil

import (
	"os"
	"path/filepath"
)

// WriteAtomic пишет data во временный файл рядом с path и переименовывает его
// поверх path: прерванная запись не оставляет полупустой конфиг или файл
// ключей. Права выставляются до записи содержимого — ключ ни мгновения не
// лежит на диске с правами шире perm. Каталог создаётся с правами 0700.
func WriteAtomic(path string, data []byte, perm os.FileMode) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	f, err := os.CreateTemp(dir, "."+filepath.Base(path)+".*")
	if err != nil {
		return err
	}
	tmp := f.Name()
	defer os.Remove(tmp) // после удачного rename файла уже нет — ошибка не важна
	if err := f.Chmod(perm); err != nil {
		f.Close()
		return err
	}
	if _, err := f.Write(data); err != nil {
		f.Close()
		return err
	}
	if err := f.Sync(); err != nil {
		f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}
