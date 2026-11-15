package rede

import (
	"fmt"
	"os"
	"sync"
	"time"
)

const CaminhoLog = "log.txt"

var logMu sync.Mutex

func registrarLog(saida *os.File, componente, format string, args ...any) {
	timestamp := time.Now().Format("2006-01-02 15:04:05.000")
	mensagem := fmt.Sprintf(format, args...)
	linha := fmt.Sprintf("[%s] [%s] %s\n", timestamp, componente, mensagem)

	logMu.Lock()
	defer logMu.Unlock()

	_, _ = saida.WriteString(linha)

	f, err := os.OpenFile(CaminhoLog, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err == nil {
		_, _ = f.WriteString(linha)
		_ = f.Close()
	}
}

func Log(componente, format string, args ...any) {
	registrarLog(os.Stdout, componente, format, args...)
}

func LogErro(componente, format string, args ...any) {
	registrarLog(os.Stderr, componente, format, args...)
}
