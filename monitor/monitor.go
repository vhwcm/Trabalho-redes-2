package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"net"
	"os"
	"os/exec"
	"os/signal"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
	"trabalho-redes/rede"
)

const (
	portaMonitor         = 9000
	intervaloRecuperacao = 5 * time.Second
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
	tabela = &TabelaServidores{
		TotalConexoes: 0,
		Servidores:    make([]*ServidorStatus, 0),
	}
	mu sync.Mutex

	procMu               sync.Mutex
	processosRecuperados = make(map[int]*exec.Cmd)
)

func compilarServidor() (string, error) {
	tmpFile, err := os.CreateTemp("", "servidor_monitor_bin_*")
	if err != nil {
		return "", fmt.Errorf("erro ao criar binário temporário do servidor: %w", err)
	}
	tmpBin := tmpFile.Name()
	tmpFile.Close()

	fmt.Println("[MONITOR] Compilando binário do servidor para auto-recuperação...")
	buildCmd := exec.Command("go", "build", "-o", tmpBin, "./servidor")
	buildCmd.Stdout = os.Stdout
	buildCmd.Stderr = os.Stderr
	if err := buildCmd.Run(); err != nil {
		os.Remove(tmpBin)
		return "", fmt.Errorf("erro ao compilar ./servidor: %w", err)
	}

	return tmpBin, nil
}

func responderTabela(conn net.Conn) {
	dadosJSON, _ := json.Marshal(tabela)
	resp := &rede.Mensagem{
		Tipo:     rede.TipoResposta,
		Porta:    portaMonitor,
		Conteudo: string(dadosJSON),
	}
	rede.EnviarMensagem(conn, resp)
}

func tratarRequisicao(conn net.Conn) {
	defer conn.Close()

	msg, err := rede.LerMensagem(conn)
	if err != nil {
		return
	}

	mu.Lock()
	defer mu.Unlock()

	partes := strings.Fields(msg.Conteudo)
	if len(partes) == 0 {
		responderTabela(conn)
		return
	}

	switch partes[0] {
	case "GET_STATUS":
		responderTabela(conn)

	case "SET_STATUS":
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
		resp := &rede.Mensagem{Tipo: rede.TipoResposta, Porta: portaMonitor, Conteudo: "OK"}
		rede.EnviarMensagem(conn, resp)

	case "INC_CONEXAO":
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
		resp := &rede.Mensagem{Tipo: rede.TipoResposta, Porta: portaMonitor, Conteudo: "OK"}
		rede.EnviarMensagem(conn, resp)

	case "DEC_CONEXAO":
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
		resp := &rede.Mensagem{Tipo: rede.TipoResposta, Porta: portaMonitor, Conteudo: "OK"}
		rede.EnviarMensagem(conn, resp)

	default:
		responderTabela(conn)
	}
}

// checarServidor envia PING com timeout para verificar se o servidor está de pé e obter suas conexões ativas
func checarServidor(portaServidor int) bool {
	status := "DOWN"
	conexoes := 0

	conn, err := rede.ConectarTCPTimeout(portaServidor, 1*time.Second)
	if err == nil {
		_ = conn.SetDeadline(time.Now().Add(1 * time.Second))
		pingMsg := &rede.Mensagem{
			Tipo:     rede.TipoPing,
			Porta:    portaServidor,
			Conteudo: "PING",
		}
		if err := rede.EnviarMensagem(conn, pingMsg); err == nil {
			if resp, err := rede.LerMensagem(conn); err == nil {
				if qtd, convErr := strconv.Atoi(strings.TrimSpace(resp.Conteudo)); convErr == nil && qtd >= 0 {
					status = "UP"
					conexoes = qtd
				}
			}
		}
		_ = conn.Close()
	}

	var statusAnterior string
	mu.Lock()
	for _, s := range tabela.Servidores {
		if s.Porta == portaServidor {
			statusAnterior = s.Status
			s.Status = status
			s.Conexoes = conexoes
			break
		}
	}
	mu.Unlock()

	if statusAnterior != "" && statusAnterior != status {
		rede.Log("MONITOR", "Servidor :%d mudou status: %s -> %s (conexões=%d)", portaServidor, statusAnterior, status, conexoes)
	}

	return status == "UP"
}

func iniciarMonitoramento(totalServidores, portaBase, intervalo int, stopChan <-chan struct{}) {
	ticker := time.NewTicker(time.Duration(intervalo) * time.Second)
	defer ticker.Stop()

	for i := 0; i < totalServidores; i++ {
		go checarServidor(portaBase + i)
	}

	for {
		select {
		case <-stopChan:
			return
		case <-ticker.C:
			for i := 0; i < totalServidores; i++ {
				go checarServidor(portaBase + i)
			}
		}
	}
}

func subirServidor(servidorBin string, porta int, procWg *sync.WaitGroup) {
	cmd := exec.Command(servidorBin, "-port", strconv.Itoa(porta))
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if err := cmd.Start(); err != nil {
		rede.LogErro("MONITOR", "Falha ao reiniciar servidor na porta %d: %v", porta, err)
		return
	}

	procMu.Lock()
	processosRecuperados[porta] = cmd
	procMu.Unlock()

	procWg.Add(1)
	go func(p int, c *exec.Cmd) {
		defer procWg.Done()
		_ = c.Wait()
		procMu.Lock()
		if processosRecuperados[p] == c {
			delete(processosRecuperados, p)
		}
		procMu.Unlock()
	}(porta, cmd)

	time.Sleep(200 * time.Millisecond)
	checarServidor(porta)
}

func iniciarRecuperacao(servidorBin string, stopChan <-chan struct{}, procWg *sync.WaitGroup) {
	ticker := time.NewTicker(intervaloRecuperacao)
	defer ticker.Stop()

	for {
		select {
		case <-stopChan:
			return
		case <-ticker.C:
			mu.Lock()
			var portasCaidas []int
			for _, s := range tabela.Servidores {
				if s.Status == "DOWN" {
					portasCaidas = append(portasCaidas, s.Porta)
				}
			}
			mu.Unlock()

			for _, porta := range portasCaidas {
				select {
				case <-stopChan:
					return
				default:
				}

				if !checarServidor(porta) {
					rede.Log("MONITOR", "Servidor na porta %d está DOWN. Subindo novamente...", porta)
					subirServidor(servidorBin, porta, procWg)
				}
			}
		}
	}
}

func main() {
	totalServidores := flag.Int("n", 3, "Número de servidores backend monitorados")
	portaBase := flag.Int("base", 8081, "Porta inicial dos servidores")
	intervalo := flag.Int("intervalo", 2, "Intervalo em segundos entre checagens")
	flag.Parse()

	servidorBin, err := compilarServidor()
	if err != nil {
		rede.LogErro("MONITOR", "%v", err)
		os.Exit(1)
	}
	defer os.Remove(servidorBin)

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

	listener, err := rede.CriarListenerTCP(portaMonitor)
	if err != nil {
		rede.LogErro("MONITOR", "Não foi possível iniciar na porta %d: %v", portaMonitor, err)
		os.Exit(1)
	}

	rede.Log("MONITOR", "Ativo na porta fixa %d gerenciando %d servidores (portas %d a %d)...",
		portaMonitor, *totalServidores, *portaBase, *portaBase+*totalServidores-1)

	stopChan := make(chan struct{})
	var procWg sync.WaitGroup

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)

	go func() {
		<-sigChan
		rede.Log("MONITOR", "Encerrando...")
		close(stopChan)
		_ = listener.Close()

		procMu.Lock()
		for _, cmd := range processosRecuperados {
			if cmd.Process != nil {
				_ = cmd.Process.Signal(syscall.SIGTERM)
			}
		}
		procMu.Unlock()
	}()

	go iniciarMonitoramento(*totalServidores, *portaBase, *intervalo, stopChan)
	go iniciarRecuperacao(servidorBin, stopChan, &procWg)

	for {
		conn, err := listener.Accept()
		if err != nil {
			if errors.Is(err, net.ErrClosed) {
				break
			}
			continue
		}
		go tratarRequisicao(conn)
	}

	procWg.Wait()
	rede.Log("MONITOR", "Encerrado.")
}
