package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"net"
	"sync"
	"time"
	"trabalho-redes/rede"
)

type ServidorStatus struct {
	ID       int    `json:"id"`
	IP       string `json:"ip"`
	Porta    int    `json:"porta"`
	Status   string `json:"status"` // "UP" ou "DOWN"
	Conexoes int    `json:"conexoes"`
}

type TabelaServidores struct {
	TotalConexoes int               `json:"total_conexoes"`
	Servidores    []*ServidorStatus `json:"servidores"`
}

var (
	tabela = &TabelaServidores{
		TotalConexoes: 0,
		Servidores:    make([]*ServidorStatus, 0),
	}
	mu sync.Mutex // Protege a tabela contra acessos concorrentes
)

// checarServidor tenta apenas abrir e fechar a conexão TCP para saber se o servidor está ativo
func checarServidor(s *ServidorStatus) {
	conn, err := rede.ConectarTCP(s.Porta)
	mu.Lock()
	if err != nil {
		s.Status = "DOWN"
	} else {
		s.Status = "UP"
		conn.Close()
	}
	mu.Unlock()
}

// monitorarLoop roda em segundo plano checando a conectividade dos servidores a cada 2 segundos
func monitorarLoop() {
	for {
		for _, s := range tabela.Servidores {
			go checarServidor(s)
		}
		time.Sleep(2 * time.Second)
	}
}

// iniciaServidorStatusTCP escuta na porta 9000 e devolve a tabela atualizada
func iniciaServidorStatusTCP(porta int) {
	listener, err := rede.CriarListenerTCP(porta)
	if err != nil {
		fmt.Printf("[MONITOR ERRO] Falha ao abrir porta de status %d: %v\n", porta, err)
		return
	}
	defer listener.Close()

	fmt.Printf("[MONITOR] Serviço de status TCP ativo na porta %d\n", porta)

	for {
		conn, err := listener.Accept()
		if err != nil {
			continue
		}

		go func(c net.Conn) {
			defer c.Close()

			_, err := rede.LerMensagem(c)
			if err != nil {
				return
			}

			mu.Lock()
			dadosJSON, err := json.Marshal(tabela)
			mu.Unlock()

			if err != nil {
				return
			}

			resposta := &rede.Mensagem{
				Tipo:     rede.TipoResposta,
				Porta:    porta,
				Conteudo: string(dadosJSON),
			}
			rede.EnviarMensagem(c, resposta)
		}(conn)
	}
}

func main() {
	totalServidores := flag.Int("n", 3, "Número de servidores a monitorar")
	portaBase := flag.Int("base", 8081, "Porta inicial dos servidores")
	portaStatus := flag.Int("status-port", 9000, "Porta onde o monitor responde status")
	flag.Parse()

	// Inicializa a tabela no monitor
	for i := 0; i < *totalServidores; i++ {
		porta := *portaBase + i
		tabela.Servidores = append(tabela.Servidores, &ServidorStatus{
			ID:       i + 1,
			IP:       "127.0.0.1",
			Porta:    porta,
			Status:   "DOWN",
			Conexoes: 0,
		})
	}

	fmt.Printf("[MONITOR] Monitorando %d servidores a partir da porta %d...\n", *totalServidores, *portaBase)

	// Inicia a verificação contínua de status
	go monitorarLoop()

	// Inicia o endpoint TCP na porta 9000 para responder o status da tabela
	iniciaServidorStatusTCP(*portaStatus)
}
