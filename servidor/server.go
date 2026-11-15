package main

import (
	"errors"
	"flag"
	"fmt"
	"net"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"trabalho-redes/rede"
)

const response = "KamehamehameHAAAAAAAAAA"

var conexoesAtivas atomic.Int64

func atenderConexao(c net.Conn, porta int, mensagemResp *rede.Mensagem, wg *sync.WaitGroup) {
	defer wg.Done()
	defer c.Close()

	msg, err := rede.LerMensagem(c)
	if err != nil {
		return
	}

	if msg.Tipo == rede.TipoPing || strings.TrimSpace(msg.Conteudo) == "PING" {
		respPing := &rede.Mensagem{
			Tipo:     rede.TipoResposta,
			Porta:    porta,
			Conteudo: strconv.FormatInt(conexoesAtivas.Load(), 10),
		}
		_ = rede.EnviarMensagem(c, respPing)
		return
	}

	conexoesAtivas.Add(1)
	defer conexoesAtivas.Add(-1)

	tag := fmt.Sprintf("Servidor :%d", porta)
	for {
		rede.Log(tag, "Requisição atendida (conexões ativas: %d): %+v", conexoesAtivas.Load(), msg)
		if err := rede.EnviarMensagem(c, mensagemResp); err != nil {
			return
		}

		msg, err = rede.LerMensagem(c)
		if err != nil {
			return
		}
	}
}

func main() {
	flagPorta := flag.Int("port", 8081, "Porta TCP do servidor")
	flag.Parse()

	porta := *flagPorta
	if flag.NArg() > 0 {
		val, err := strconv.Atoi(flag.Arg(0))
		if err != nil || val <= 0 {
			fmt.Fprintln(os.Stderr, "Erro: informe uma porta válida para o servidor.")
			os.Exit(1)
		}
		porta = val
	}

	if porta <= 0 {
		fmt.Fprintln(os.Stderr, "Uso: go run ./servidor [-port <porta>]")
		os.Exit(1)
	}

	tag := fmt.Sprintf("Servidor :%d", porta)
	listener, err := rede.CriarListenerTCP(porta)
	if err != nil {
		rede.LogErro(tag, "Falha ao iniciar na porta %d: %v", porta, err)
		os.Exit(1)
	}

	rede.Log(tag, "Ativo e ouvindo...")

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)

	go func() {
		<-sigChan
		rede.Log(tag, "Encerrando...")
		_ = listener.Close()
	}()

	mensagemResp := &rede.Mensagem{
		Tipo:     rede.TipoResposta,
		Porta:    porta,
		Conteudo: response,
	}

	var wg sync.WaitGroup
	for {
		conn, err := listener.Accept()
		if err != nil {
			if errors.Is(err, net.ErrClosed) {
				break
			}
			continue
		}

		wg.Add(1)
		go atenderConexao(conn, porta, mensagemResp, &wg)
	}

	wg.Wait()
	rede.Log(tag, "Encerrado.")
}
