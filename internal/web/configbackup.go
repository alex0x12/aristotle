package web

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// errBackupExists — имя копии занято: чужую копию не перезаписываем.
var errBackupExists = errors.New("копия с таким именем уже есть")

// backupCorruptConfig кладёт рядом с path копию его содержимого под именем
// <имя>.bad-<UTC ГГГГММДДTЧЧММССZ> (права 0600, создание с O_EXCL: существующий
// файл не затирается) и возвращает только имя копии, без пути. Копия записана
// на диск (Sync) до того, как исходный файл будет заменён.
func backupCorruptConfig(path string, now time.Time) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	name := fmt.Sprintf("%s.bad-%s", filepath.Base(path), now.UTC().Format("20060102T150405Z"))
	dst := filepath.Join(filepath.Dir(path), name)
	f, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if errors.Is(err, os.ErrExist) {
		return "", errBackupExists
	}
	if err != nil {
		return "", err
	}
	if _, err = f.Write(data); err == nil {
		err = f.Sync()
	}
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		os.Remove(dst) // недописанная копия хуже, чем её отсутствие: за целую её никто не примет
		return "", err
	}
	return name, nil
}
