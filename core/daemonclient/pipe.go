package daemonclient

import (
	"fmt"
	"strings"
)

// protectedPipePrefix — пространство имён, где канал может создать лишь администратор
// или LocalSystem: на том конце служба, а не подставной процесс, которому мы отдали бы ключ.
const protectedPipePrefix = `\\.\pipe\ProtectedPrefix\Administrators\`

// pipePath превращает npipe://<имя> в путь защищённого канала. Имя — один сегмент:
// разделители позволили бы уйти из защищённого пространства имён.
func pipePath(addr string) (string, error) {
	name, ok := strings.CutPrefix(addr, "npipe://")
	if !ok || name == "" || strings.ContainsAny(name, `\/`) {
		return "", fmt.Errorf("адрес службы %q: нужен npipe://<имя> без разделителей пути", addr)
	}
	return protectedPipePrefix + name, nil
}
