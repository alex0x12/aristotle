// internal/persona/persona.go
package persona

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// Load склеивает файлы персоны по возрастанию имени. Порядок фиксирован
// намеренно: результат входит в кэшируемый префикс запроса.
func Load(dir string) (string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return "", fmt.Errorf("персона %s: %w", dir, err)
	}

	names := make([]string, 0, len(entries))
	for _, e := range entries {
		name := e.Name()
		// Имена с точки отбрасываем: блокировочный симлинк редактора
		// (.#04-doctrine.md) прошёл бы фильтр по расширению, а чтение по нему
		// упало бы — и промпт не собрался бы только из-за открытого редактора.
		if e.IsDir() || strings.HasPrefix(name, ".") || !strings.HasSuffix(name, ".md") {
			continue
		}
		names = append(names, name)
	}
	sort.Strings(names)
	if len(names) == 0 {
		return "", fmt.Errorf("персона %s: нет ни одного .md файла", dir)
	}

	parts := make([]string, 0, len(names))
	for _, name := range names {
		data, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			return "", err
		}
		parts = append(parts, strings.TrimSpace(string(data)))
	}
	return strings.Join(parts, "\n\n"), nil
}
