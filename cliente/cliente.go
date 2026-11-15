package main

import (
	"flag"
	"fmt"
	"math/rand/v2"
	"os"
	"os/signal"
	"syscall"
	"time"
	"trabalho-redes/rede"
)

func main() {
	portaLB := flag.Int("port", 8080, "Porta TCP do Load Balancer")
	duracaoSeg := flag.Int("k", 5, "Duração fixa em segundos que o cliente permanece conectado")
	duracaoMin := flag.Int("k-min", 0, "Duração mínima em segundos (opcional, para duração variável)")
	duracaoMax := flag.Int("k-max", 0, "Duração máxima em segundos (opcional, para duração variável)")
	intervaloMs := flag.Int("intervalo", 500, "Intervalo em milissegundos entre o envio de cada mensagem")
	flag.Parse()

	if *portaLB <= 0 || *intervaloMs <= 0 {
		fmt.Fprintln(os.Stderr, "Uso: go run ./cliente [-port 8080] [-k 5 | -k-min 2 -k-max 8] [-intervalo 500]")
		os.Exit(1)
	}

	duracao := *duracaoSeg
	if *duracaoMin > 0 || *duracaoMax > 0 {
		if *duracaoMin <= 0 || *duracaoMax < *duracaoMin {
			fmt.Fprintln(os.Stderr, "Erro: informe valores válidos com 0 < -k-min <= -k-max.")
			os.Exit(1)
		}
		duracao = *duracaoMin + rand.IntN(*duracaoMax-*duracaoMin+1)
	}

	if duracao <= 0 {
		fmt.Fprintln(os.Stderr, "Erro: a duração em segundos deve ser maior que zero.")
		os.Exit(1)
	}

	pid := os.Getpid()
	tag := fmt.Sprintf("Cliente %d", pid)

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)

	conn, err := rede.ConectarTCPTimeout(*portaLB, 3*time.Second)
	if err != nil {
		rede.LogErro(tag, "Erro ao conectar ao Load Balancer (:%d): %v", *portaLB, err)
		os.Exit(1)
	}
	defer conn.Close()

	rede.Log(tag, "Conectado ao Load Balancer na porta :%d por %ds...", *portaLB, duracao)

	fim := time.Now().Add(time.Duration(duracao) * time.Second)
	seq := 0

	for time.Now().Before(fim) {
		seq++
		msg := &rede.Mensagem{
			Tipo:     rede.TipoRequisicao,
			Porta:    *portaLB,
			Conteudo: fmt.Sprintf("Mensagem #%d do cliente %d", seq, pid),
		}

		if err := rede.EnviarMensagem(conn, msg); err != nil {
			rede.LogErro(tag, "Erro ao enviar req #%d: %v", seq, err)
			return
		}

		resp, err := rede.LerMensagem(conn)
		if err != nil {
			rede.LogErro(tag, "Erro ao ler resposta da req #%d: %v", seq, err)
			return
		}

		rede.Log(tag, "Req #%d <- Servidor :%d | %s", seq, resp.Porta, resp.Conteudo)

		if time.Now().Add(time.Duration(*intervaloMs) * time.Millisecond).After(fim) {
			break
		}

		select {
		case <-sigChan:
			rede.Log(tag, "Encerrado por sinal.")
			return
		case <-time.After(time.Duration(*intervaloMs) * time.Millisecond):
		}
	}

	rede.Log(tag, "Conexão finalizada após %d requisições (%ds).", seq, duracao)
}