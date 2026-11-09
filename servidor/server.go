package main

import (
	"flag"
	"fmt"
	"net"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"trabalho-redes/rede"
)

const response = "KamehamehameHAAAAAAAAAA"

func iniciaServidorIndividual(id, porta int) {
	listener, err := rede.CriarListenerTCP(porta)
	if err != nil {
		fmt.Printf("[ERRO] Falha ao iniciar servidor ID %d na porta %d: %v\n", id, porta, err)
		return
	}
	defer listener.Close()

	fmt.Printf("[Servidor ID %d] Ativo e ouvindo na porta %d\n", id, porta)

	mensagemResp := &rede.Mensagem{
		Tipo:     rede.TipoResposta,
		Porta:    porta,
		Conteudo: response,
	}

	for {
		conn, err := listener.Accept()
		if err != nil {
			continue
		}

		go func(c net.Conn) {
			defer c.Close()

			msg, err := rede.LerMensagem(c)
			if err != nil {
				return
			}

			fmt.Printf("[Servidor :%d] Requisição atendida: %+v\n", porta, msg)
			rede.EnviarMensagem(c, mensagemResp)
		}(conn)
	}
}

func main() {
	flagN := flag.Int("n", 0, "Numero de servidores a serem criados")
	portaBase := flag.Int("base", 8081, "Porta inicial dos servidores")
	flag.Parse()

	totalServidores := *flagN

	if totalServidores <= 0 && len(flag.Args()) > 0 {
		var err error
		totalServidores, err = strconv.Atoi(flag.Arg(0))
		if err != nil || totalServidores <= 0 {
			fmt.Println("Erro: informe um número válido de servidores.")
			os.Exit(1)
		}
	}

	if totalServidores <= 0 {
		fmt.Println("Uso: go run ./servidor <numero_de_servidores>")
		os.Exit(1)
	}

	fmt.Printf("Iniciando %d servidores simples a partir da porta %d...\n\n", totalServidores, *portaBase)

	for i := 0; i < totalServidores; i++ {
		porta := *portaBase + i
		go iniciaServidorIndividual(i+1, porta)
	}

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)

	fmt.Println("Servidores em execução. Pressione Ctrl+C para encerrar.")
	<-sigChan

	fmt.Println("\nTodos os servidores foram encerrados.")
}
