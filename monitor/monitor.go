package main

import (
	"flag"
	"fmt"
	"time"
	"trabalho-redes/rede"
)

// checarServidor testa conectividade TCP com o servidor e atualiza a Tabela
func checarServidor(portaServidor, portaTabela int) {
	// 1. Testa se o servidor responde
	conn, err := rede.ConectarTCP(portaServidor)
	status := "DOWN"
	if err == nil {
		status = "UP"
		conn.Close()
	}

	// 2. Avisa o processo da Tabela de Servidores sobre o novo status
	connTabela, err := rede.ConectarTCP(portaTabela)
	if err != nil {
		return
	}
	defer connTabela.Close()

	msg := &rede.Mensagem{
		Tipo:     rede.TipoRequisicao,
		Porta:    portaTabela,
		Conteudo: fmt.Sprintf("SET_STATUS %d %s", portaServidor, status),
	}
	rede.EnviarMensagem(connTabela, msg)
	rede.LerMensagem(connTabela) // aguarda confirmação "OK"
}

func main() {
	portaTabela := flag.Int("tabela-port", 9000, "Porta do processo Tabela de Servidores")
	totalServidores := flag.Int("n", 3, "Número de servidores a monitorar")
	portaBase := flag.Int("base", 8081, "Porta inicial dos servidores")
	intervalo := flag.Int("intervalo", 2, "Intervalo em segundos entre checagens")
	flag.Parse()

	fmt.Printf("[MONITOR] Iniciado. Checando portas %d a %d e atualizando Tabela na porta %d...\n",
		*portaBase, *portaBase+*totalServidores-1, *portaTabela)

	for {
		for i := 0; i < *totalServidores; i++ {
			porta := *portaBase + i
			go checarServidor(porta, *portaTabela)
		}
		time.Sleep(time.Duration(*intervalo) * time.Second)
	}
}
