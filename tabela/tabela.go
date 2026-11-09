package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"net"
	"strings"
	"sync"
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
	mu sync.Mutex
)

func tratarRequisicao(conn net.Conn, portaTabela int) {
	defer conn.Close()

	msg, err := rede.LerMensagem(conn)
	if err != nil {
		return
	}

	mu.Lock()
	defer mu.Unlock()

	partes := strings.Split(msg.Conteudo, " ")
	comando := partes[0]

	switch comando {
	case "GET_STATUS":
		dadosJSON, _ := json.Marshal(tabela)
		resp := &rede.Mensagem{
			Tipo:     rede.TipoResposta,
			Porta:    portaTabela,
			Conteudo: string(dadosJSON),
		}
		rede.EnviarMensagem(conn, resp)

	case "SET_STATUS":
		// Formato enviado pelo monitor: "SET_STATUS <porta> <UP|DOWN>"
		if len(partes) >= 3 {
			var portaAlvo int
			fmt.Sscanf(partes[1], "%d", &portaAlvo)
			novoStatus := partes[2]

			for _, s := range tabela.Servidores {
				if s.Porta == portaAlvo {
					s.Status = novoStatus
					break
				}
			}
		}
		resp := &rede.Mensagem{Tipo: rede.TipoResposta, Porta: portaTabela, Conteudo: "OK"}
		rede.EnviarMensagem(conn, resp)

	case "INC_CONEXAO":
		// Formato enviado pelo LB: "INC_CONEXAO <porta>"
		if len(partes) >= 2 {
			var portaAlvo int
			fmt.Sscanf(partes[1], "%d", &portaAlvo)
			for _, s := range tabela.Servidores {
				if s.Porta == portaAlvo {
					s.Conexoes++
					tabela.TotalConexoes++
					break
				}
			}
		}
		resp := &rede.Mensagem{Tipo: rede.TipoResposta, Porta: portaTabela, Conteudo: "OK"}
		rede.EnviarMensagem(conn, resp)

	case "DEC_CONEXAO":
		// Formato enviado pelo LB: "DEC_CONEXAO <porta>"
		if len(partes) >= 2 {
			var portaAlvo int
			fmt.Sscanf(partes[1], "%d", &portaAlvo)
			for _, s := range tabela.Servidores {
				if s.Porta == portaAlvo && s.Conexoes > 0 {
					s.Conexoes--
					break
				}
			}
		}
		resp := &rede.Mensagem{Tipo: rede.TipoResposta, Porta: portaTabela, Conteudo: "OK"}
		rede.EnviarMensagem(conn, resp)

	default:
		// Se vier comando desconhecido, devolve o estado atual da tabela
		dadosJSON, _ := json.Marshal(tabela)
		resp := &rede.Mensagem{
			Tipo:     rede.TipoResposta,
			Porta:    portaTabela,
			Conteudo: string(dadosJSON),
		}
		rede.EnviarMensagem(conn, resp)
	}
}

func main() {
	portaTabela := flag.Int("port", 9000, "Porta TCP do processo Tabela de Servidores")
	totalServidores := flag.Int("n", 3, "Número de servidores backend gerenciados")
	portaBase := flag.Int("base", 8081, "Porta inicial dos servidores")
	flag.Parse()

	// Inicializa a tabela em memória
	for i := 0; i < *totalServidores; i++ {
		porta := *portaBase + i
		tabela.Servidores = append(tabela.Servidores, &ServidorStatus{
			ID:       i + 1,
			IP:       "127.0.0.1",
			Porta:    porta,
			Status:   "DOWN", // Inicialmente DOWN até o monitor checar
			Conexoes: 0,
		})
	}

	listener, err := rede.CriarListenerTCP(*portaTabela)
	if err != nil {
		fmt.Printf("[TABELA ERRO] Não foi possível iniciar na porta %d: %v\n", *portaTabela, err)
		return
	}
	defer listener.Close()

	fmt.Printf("[TABELA] Processo ativo na porta %d gerenciando %d servidores...\n", *portaTabela, *totalServidores)

	for {
		conn, err := listener.Accept()
		if err != nil {
			continue
		}
		go tratarRequisicao(conn, *portaTabela)
	}
}
