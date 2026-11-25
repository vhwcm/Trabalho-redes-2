package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"net"
	"os"
	"os/signal"
	"sort"
	"strings"
	"sync"
	"syscall"
	"time"
	"trabalho-redes/rede"
)

const (
	portaMonitor             = 9000
	politicaRoundRobin       = "round-robin"
	politicaLeastConnections = "least-connections"
)

type ServidorStatus struct {
	ID       int    `json:"id"`
	IP       string `json:"ip"`
	Porta    int    `json:"porta"`
	Status   string `json:"status"`
	Conexoes int    `json:"conexoes"`
}

type TabelaServidores struct {
	TotalConexoes int               `json:"total_conexoes"`
	Servidores    []*ServidorStatus `json:"servidores"`
}

var (
	rrMu    sync.Mutex
	rrIndex int
)


// tem mais de um tipo de entrada certa
func normalizarPolitica(valor string) (string, error) {
	switch strings.ToLower(strings.TrimSpace(valor)) {
	case "round-robin", "round_robin", "rr":
		return politicaRoundRobin, nil
	case "least-connections", "least_connections", "lc":
		return politicaLeastConnections, nil
	default:
		return "", fmt.Errorf("política inválida: %q (use 'round-robin' ou 'least-connections')", valor)
	}
}

// consultar tabela concecta no monitor, manda "GET_STATUS" e recebe um json com 
// os servidores, o status e as conexoes de cada um

func consultarTabela() (*TabelaServidores, error) {
	conn, err := rede.ConectarTCPTimeout(portaMonitor, 2*time.Second)
	if err != nil {
		return nil, err
	}
	defer conn.Close()

	_ = conn.SetDeadline(time.Now().Add(2 * time.Second))

	req := &rede.Mensagem{
		Tipo:     rede.TipoRequisicao,
		Porta:    portaMonitor,
		Conteudo: "GET_STATUS",
	}
	if err := rede.EnviarMensagem(conn, req); err != nil {
		return nil, err
	}

	// espera a mensagem do monitor
	resp, err := rede.LerMensagem(conn)
	if err != nil {
		return nil, err
	}

	// preenche uma tabela com a resposta do monitor
	var tabela TabelaServidores
	if err := json.Unmarshal([]byte(resp.Conteudo), &tabela); err != nil {
		return nil, fmt.Errorf("erro ao decodificar tabela de servidores: %w", err)
	}

	return &tabela, nil
}


func notificarConexao(comando string, portaServidor int) {

	// nao retorna nada ,se nao conseguir falar com o monitor,apenas sai em silencio
	//Se o monitor estiver fora, o contador nao atualiza nada e ninguem eh avisado
	conn, err := rede.ConectarTCPTimeout(portaMonitor, 1*time.Second)
	if err != nil {
		return
	}
	defer conn.Close()

	_ = conn.SetDeadline(time.Now().Add(1 * time.Second))

	//informa o monitor que houve uma conexao
	msg := &rede.Mensagem{
		Tipo:     rede.TipoRequisicao,
		Porta:    portaMonitor,
		Conteudo: fmt.Sprintf("%s %d", comando, portaServidor),
	}
	if err := rede.EnviarMensagem(conn, msg); err == nil {
		_, _ = rede.LerMensagem(conn)
	}
}

// filtra so os servidores UP e ordena por porta
// gira a lista com um indice global (rrIndex), que eh o Round-Robin
func ordenarCandidatos(servidores []*ServidorStatus, politica string) []*ServidorStatus {
	var ativos []*ServidorStatus
	for _, s := range servidores {
		if strings.ToUpper(s.Status) == "UP" {
			ativos = append(ativos, s)
		}
	}

	if len(ativos) == 0 {
		return nil
	}

	//ordena as portas
	sort.Slice(ativos, func(i, j int) bool {
		return ativos[i].Porta < ativos[j].Porta
	})

	// incrementa o indice global para a proxima chamada
	rrMu.Lock()
	start := rrIndex % len(ativos)
	rrIndex++
	rrMu.Unlock()

	//nao entendi essa parte direito
	rotacionados := make([]*ServidorStatus, 0, len(ativos))
	rotacionados = append(rotacionados, ativos[start:]...)
	rotacionados = append(rotacionados, ativos[:start]...)

	if politica == politicaLeastConnections {
		sort.SliceStable(rotacionados, func(i, j int) bool {
			return rotacionados[i].Conexoes < rotacionados[j].Conexoes
		})
	}

	return rotacionados
}

//retorna a conexao, o servidor escolhido e o erro.Percorre os candidatos em ordem 
//e retorna o primeiro que aceitar
func conectarServidorDisponivel(candidatos []*ServidorStatus) (net.Conn, *ServidorStatus, error) {
	for _, s := range candidatos {
		conn, err := rede.ConectarTCPTimeout(s.Porta, 2*time.Second)
		if err == nil {
			return conn, s, nil
		}
		rede.Log("LB AVISO", "Falha ao conectar no servidor :%d (%v), tentando próximo...", s.Porta, err)
	}
	return nil, nil, errors.New("nenhum servidor UP aceitou conexão")
}

//sync.waitgroup, todas as goroutines compartilham o mesmo contador
func atenderCliente(connClient net.Conn, portaLB int, politica string, wg *sync.WaitGroup) {
	defer wg.Done()
	defer connClient.Close()

	tabela, err := consultarTabela()
	if err != nil {
		rede.LogErro("LB", "Falha ao consultar monitor na porta %d: %v", portaMonitor, err)
		_ = rede.EnviarMensagem(connClient, &rede.Mensagem{
			Tipo:     rede.TipoResposta,
			Porta:    portaLB,
			Conteudo: "ERRO: monitor indisponivel",
		})
		return
	}

	candidatos := ordenarCandidatos(tabela.Servidores, politica)
	if len(candidatos) == 0 {
		rede.Log("LB AVISO", "Nenhum servidor UP disponível para atender cliente %s.", connClient.RemoteAddr().String())
		_ = rede.EnviarMensagem(connClient, &rede.Mensagem{
			Tipo:     rede.TipoResposta,
			Porta:    portaLB,
			Conteudo: "ERRO: nenhum servidor disponivel",
		})
		return
	}

	connServer, escolhido, err := conectarServidorDisponivel(candidatos)
	if err != nil {
		rede.LogErro("LB", "%v", err)
		_ = rede.EnviarMensagem(connClient, &rede.Mensagem{
			Tipo:     rede.TipoResposta,
			Porta:    portaLB,
			Conteudo: "ERRO: nenhum servidor disponivel",
		})
		return
	}
	defer connServer.Close()

	rede.Log("LB", "Nova conexão (%s) -> Servidor :%d [política=%s, conexões=%d]",
		connClient.RemoteAddr().String(), escolhido.Porta, politica, escolhido.Conexoes)

	//avisa o monitor que ha uma conexao a mais e agenda o aviso de "menos uma" pra quandp
	// a funcao terminar
	notificarConexao("INC_CONEXAO", escolhido.Porta)
	defer notificarConexao("DEC_CONEXAO", escolhido.Porta)


	//1-le uma mensagem do cliente
	// 2-encaminha para o servidor
	// 3- le a resposta do servidor
	// 4-devolve para o cliente
	for {
		msg, err := rede.LerMensagem(connClient)
		if err != nil {
			return
		}

		if err := rede.EnviarMensagem(connServer, msg); err != nil {
			_ = rede.EnviarMensagem(connClient, &rede.Mensagem{
				Tipo:     rede.TipoResposta,
				Porta:    portaLB,
				Conteudo: fmt.Sprintf("ERRO: falha ao encaminhar para servidor :%d", escolhido.Porta),
			})
			return
		}

		resp, err := rede.LerMensagem(connServer)
		if err != nil {
			_ = rede.EnviarMensagem(connClient, &rede.Mensagem{
				Tipo:     rede.TipoResposta,
				Porta:    portaLB,
				Conteudo: fmt.Sprintf("ERRO: servidor :%d desconectou", escolhido.Porta),
			})
			return
		}

		if err := rede.EnviarMensagem(connClient, resp); err != nil {
			return
		}
	}
}

func main() {
	portaLB := flag.Int("port", 8080, "Porta TCP do Load Balancer")
	politicaFlag := flag.String("politica", politicaLeastConnections, "Política de balanceamento: 'round-robin' ou 'least-connections'")
	flag.Parse()

	politica, err := normalizarPolitica(*politicaFlag)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}

	if *portaLB <= 0 {
		fmt.Fprintln(os.Stderr, "Erro: informe uma porta válida para o Load Balancer.")
		os.Exit(1)
	}

	listener, err := rede.CriarListenerTCP(*portaLB)
	if err != nil {
		rede.LogErro("LB", "Não foi possível iniciar na porta %d: %v", *portaLB, err)
		os.Exit(1)
	}

	rede.Log("LB", "Ativo na porta :%d | Política: %s | Monitor: :%d", *portaLB, politica, portaMonitor)

	// encerramento com ctrl c
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)

	go func() {
		<-sigChan
		rede.Log("LB", "Encerrando...")
		_ = listener.Close()
	}()

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
		go atenderCliente(conn, *portaLB, politica, &wg)
	}

	// bloqeia ate o contador chegar a zero, ou seja, ate todos os clientes terminarem
	wg.Wait()
	rede.Log("LB", "Encerrado.")
}