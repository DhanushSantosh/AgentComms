package contamination

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRepositoryIsProjectAgnostic(t *testing.T) {
	root := filepath.Join("..", "..")
	forbidden := []string{"r" + "eqai", "d:" + "\\work\\projects\\r" + "eqai"}
	_ = filepath.Walk(root, func(p string, i os.FileInfo, e error) error {
		if e != nil || i.IsDir() || strings.Contains(p, string(filepath.Separator)+".git"+string(filepath.Separator)) || strings.Contains(p, string(filepath.Separator)+".tools"+string(filepath.Separator)) {
			return nil
		}
		b, e := os.ReadFile(p)
		if e != nil {
			return e
		}
		x := strings.ToLower(string(b))
		for _, f := range forbidden {
			if strings.Contains(x, f) {
				t.Errorf("forbidden project-specific content in %s", p)
			}
		}
		return nil
	})
}
