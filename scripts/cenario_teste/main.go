package main

import (
	"bufio"
	"encoding/json"
	"flag"
	"fmt"
	"math"
	"net"
	"os"
	"os/exec"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
	"trabalho-redes/rede"
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

type Snapshot struct {
	ElapsedSec    float64
	TotalConexoes int
	Status        map[int]string
	Conexoes      map[int]int
}

type EventoTimeline struct {
	ElapsedSec float64
	Componente string
	Descricao  string
}

type CenarioResultado struct {
	Nome               string
	Descricao          string
	Politica           string
	NumClientes        int
	NumServidores      int
	PortaBase          int
	IntervaloMonitor   int
	DuracaoDesc        string
	TempoCompilacaoMs  float64
	TempoBootInfraMs   float64
	DuracaoTotalSec    float64
	TotalConexoesLB    int
	TotalRequisicoes   int
	ThroughputReqSec   float64
	ThroughputConnSec  float64
	ConexoesPorPorta   map[int]int
	RequisicoesPorPort map[int]int
	QuedasDetectadas   int
	Recuperacoes       int
	TempoDeteccaoSec   float64
	TempoRecuperacaoS  float64
	Snapshots          []Snapshot
	Eventos            []EventoTimeline
	LinhasLog          []string
}

var (
	reLogLine    = regexp.MustCompile(`^\[(\d{4}-\d{2}-\d{2} \d{2}:\d{2}:\d{2}\.\d{3})\] \[([^\]]+)\] (.*)$`)
	reLBRoute    = regexp.MustCompile(`Nova conexão \([^)]+\) -> Servidor :(\d+) \[política=([^,]+), conexões=(\d+)\]`)
	reSrvPort    = regexp.MustCompile(`^Servidor :(\d+)$`)
	reMonChange  = regexp.MustCompile(`Servidor :(\d+) mudou status: (\w+) -> (\w+) \(conexões=(\d+)\)`)
	reMonRecover = regexp.MustCompile(`Servidor na porta (\d+) está DOWN\. Subindo novamente\.\.\.`)
)

func compilarBinario(prefixo, pacote string) (string, time.Duration, error) {
	inicio := time.Now()
	tmpFile, err := os.CreateTemp("", prefixo+"_*")
	if err != nil {
		return "", 0, fmt.Errorf("erro ao criar temporário para %s: %w", pacote, err)
	}
	tmpBin := tmpFile.Name()
	tmpFile.Close()

	cmd := exec.Command("go", "build", "-o", tmpBin, pacote)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		os.Remove(tmpBin)
		return "", 0, fmt.Errorf("erro ao compilar %s: %w", pacote, err)
	}
	return tmpBin, time.Since(inicio), nil
}

func consultarMonitor() (*TabelaServidores, error) {
	conn, err := rede.ConectarTCPTimeout(9000, 300*time.Millisecond)
	if err != nil {
		return nil, err
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(300 * time.Millisecond))

	if err := rede.EnviarMensagem(conn, &rede.Mensagem{
		Tipo:     rede.TipoRequisicao,
		Porta:    9000,
		Conteudo: "GET_STATUS",
	}); err != nil {
		return nil, err
	}

	resp, err := rede.LerMensagem(conn)
	if err != nil {
		return nil, err
	}

	var t TabelaServidores
	if err := json.Unmarshal([]byte(resp.Conteudo), &t); err != nil {
		return nil, err
	}
	return &t, nil
}

func aguardarPortasLivres(portas []int, timeout time.Duration) {
	fim := time.Now().Add(timeout)
	for time.Now().Before(fim) {
		todasLivres := true
		for _, p := range portas {
			c, err := net.DialTimeout("tcp", fmt.Sprintf("127.0.0.1:%d", p), 80*time.Millisecond)
			if err == nil {
				_ = c.Close()
				todasLivres = false
				break
			}
		}
		if todasLivres {
			return
		}
		time.Sleep(150 * time.Millisecond)
	}
}

func aguardarTodosServidoresUP(esperados int, timeout time.Duration) time.Duration {
	inicio := time.Now()
	fim := inicio.Add(timeout)
	for time.Now().Before(fim) {
		t, err := consultarMonitor()
		if err == nil {
			up := 0
			for _, s := range t.Servidores {
				if s.Status == "UP" {
					up++
				}
			}
			if up >= esperados {
				return time.Since(inicio)
			}
		}
		time.Sleep(100 * time.Millisecond)
	}
	return time.Since(inicio)
}

func matarServidorNaPorta(porta int) error {
	script := fmt.Sprintf(
		"fuser -k -KILL %d/tcp >/dev/null 2>&1 || pkill -KILL -f 'servidor_bin.*-port %d'",
		porta, porta,
	)
	cmd := exec.Command("bash", "-c", script)
	return cmd.Run()
}

func lerLinhasArquivo(caminho string) ([]string, error) {
	f, err := os.Open(caminho)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	var linhas []string
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		linhas = append(linhas, sc.Text())
	}
	return linhas, sc.Err()
}

func analisarLogsCenario(res *CenarioResultado, inicioCenario time.Time) {
	var tQueda, tRecuperacao time.Time

	for _, linha := range res.LinhasLog {
		m := reLogLine.FindStringSubmatch(linha)
		if m == nil {
			continue
		}
		ts, err := time.ParseInLocation("2006-01-02 15:04:05.000", m[1], time.Local)
		if err != nil {
			continue
		}
		elapsed := ts.Sub(inicioCenario).Seconds()
		if elapsed < 0 {
			elapsed = 0
		}
		comp := m[2]
		msg := m[3]

		if route := reLBRoute.FindStringSubmatch(msg); route != nil {
			porta, _ := strconv.Atoi(route[1])
			res.ConexoesPorPorta[porta]++
			res.TotalConexoesLB++
			res.Eventos = append(res.Eventos, EventoTimeline{
				ElapsedSec: elapsed,
				Componente: "LB",
				Descricao:  fmt.Sprintf("Conexão direcionada -> :%d (conexões prévias=%s)", porta, route[3]),
			})
		}

		if srv := reSrvPort.FindStringSubmatch(comp); srv != nil {
			porta, _ := strconv.Atoi(srv[1])
			if strings.HasPrefix(msg, "Requisição atendida") {
				res.RequisicoesPorPort[porta]++
				res.TotalRequisicoes++
			} else if strings.Contains(msg, "Ativo e ouvindo") {
				res.Eventos = append(res.Eventos, EventoTimeline{
					ElapsedSec: elapsed,
					Componente: comp,
					Descricao:  "Servidor online e ouvindo",
				})
			}
		}

		if mon := reMonChange.FindStringSubmatch(msg); mon != nil {
			porta, _ := strconv.Atoi(mon[1])
			de := mon[2]
			para := mon[3]
			res.Eventos = append(res.Eventos, EventoTimeline{
				ElapsedSec: elapsed,
				Componente: "MONITOR",
				Descricao:  fmt.Sprintf("Porta :%d mudou %s -> %s", porta, de, para),
			})
			if de == "UP" && para == "DOWN" {
				res.QuedasDetectadas++
				if tQueda.IsZero() {
					tQueda = ts
				}
			}
			if de == "DOWN" && para == "UP" && !tQueda.IsZero() && tRecuperacao.IsZero() {
				tRecuperacao = ts
				res.TempoRecuperacaoS = tRecuperacao.Sub(tQueda).Seconds()
			}
		}

		if rec := reMonRecover.FindStringSubmatch(msg); rec != nil {
			res.Recuperacoes++
			res.Eventos = append(res.Eventos, EventoTimeline{
				ElapsedSec: elapsed,
				Componente: "MONITOR",
				Descricao:  fmt.Sprintf("Auto-recuperação disparada para porta :%s", rec[1]),
			})
		}

		if comp == "CENARIO" {
			res.Eventos = append(res.Eventos, EventoTimeline{
				ElapsedSec: elapsed,
				Componente: "CENARIO",
				Descricao:  msg,
			})
		}
	}

	if res.DuracaoTotalSec > 0 {
		res.ThroughputReqSec = float64(res.TotalRequisicoes) / res.DuracaoTotalSec
		res.ThroughputConnSec = float64(res.TotalConexoesLB) / res.DuracaoTotalSec
	}
}

func iniciarSubprocesso(bin string, args ...string) (*exec.Cmd, error) {
	cmd := exec.Command(bin, args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	return cmd, nil
}

func encerrarProcessos(cmds []*exec.Cmd, portasServidores []int) {
	for _, cmd := range cmds {
		if cmd != nil && cmd.Process != nil {
			_ = cmd.Process.Signal(syscall.SIGTERM)
		}
	}

	done := make(chan struct{})
	go func() {
		for _, cmd := range cmds {
			if cmd != nil {
				_ = cmd.Wait()
			}
		}
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(3 * time.Second):
		for _, cmd := range cmds {
			if cmd != nil && cmd.Process != nil {
				_ = cmd.Process.Kill()
			}
		}
	}

	todasPortas := append([]int{8080, 9000}, portasServidores...)
	aguardarPortasLivres(todasPortas, 3*time.Second)
}

func executarCenario(
	nome, descricao, politica string,
	numServidores, portaBase, intervaloMon int,
	servidoresBin, clientesBin, monitorBin, lbBin string,
	tempoCompMs float64,
	acaoClientes func(inicio time.Time) (numClientes int, duracaoDesc string, tempoDeteccao float64),
) *CenarioResultado {
	linhasAntes, _ := lerLinhasArquivo(rede.CaminhoLog)
	offsetLog := len(linhasAntes)

	portasSrv := make([]int, numServidores)
	for i := 0; i < numServidores; i++ {
		portasSrv[i] = portaBase + i
	}
	aguardarPortasLivres(append([]int{8080, 9000}, portasSrv...), 3*time.Second)

	inicioCenario := time.Now()
	rede.Log("CENARIO", "Iniciando %s (%s)", nome, politica)

	var cmds []*exec.Cmd

	inicioBoot := time.Now()
	cmdSrv, err := iniciarSubprocesso(servidoresBin, "-n", strconv.Itoa(numServidores), "-base", strconv.Itoa(portaBase))
	if err == nil {
		cmds = append(cmds, cmdSrv)
	}

	cmdMon, err := iniciarSubprocesso(monitorBin, "-n", strconv.Itoa(numServidores), "-base", strconv.Itoa(portaBase), "-intervalo", strconv.Itoa(intervaloMon))
	if err == nil {
		cmds = append(cmds, cmdMon)
	}

	cmdLB, err := iniciarSubprocesso(lbBin, "-politica", politica)
	if err == nil {
		cmds = append(cmds, cmdLB)
	}

	aguardarTodosServidoresUP(numServidores, 6*time.Second)
	tempoBootMs := float64(time.Since(inicioBoot).Microseconds()) / 1000.0

	var snapshots []Snapshot
	var snapMu sync.Mutex
	stopSnap := make(chan struct{})
	var snapWg sync.WaitGroup
	snapWg.Add(1)

	go func() {
		defer snapWg.Done()
		ticker := time.NewTicker(500 * time.Millisecond)
		defer ticker.Stop()
		for {
			select {
			case <-stopSnap:
				return
			case <-ticker.C:
				if t, err := consultarMonitor(); err == nil {
					st := make(map[int]string, len(t.Servidores))
					cx := make(map[int]int, len(t.Servidores))
					for _, s := range t.Servidores {
						st[s.Porta] = s.Status
						cx[s.Porta] = s.Conexoes
					}
					snapMu.Lock()
					snapshots = append(snapshots, Snapshot{
						ElapsedSec:    time.Since(inicioCenario).Seconds(),
						TotalConexoes: t.TotalConexoes,
						Status:        st,
						Conexoes:      cx,
					})
					snapMu.Unlock()
				}
			}
		}
	}()

	numClientes, duracaoDesc, tempoDeteccao := acaoClientes(inicioCenario)

	close(stopSnap)
	snapWg.Wait()

	duracaoTotal := time.Since(inicioCenario).Seconds()
	rede.Log("CENARIO", "Finalizando %s (duração=%.2fs)", nome, duracaoTotal)

	encerrarProcessos(cmds, portasSrv)

	linhasDepois, _ := lerLinhasArquivo(rede.CaminhoLog)
	var fatiaLogs []string
	if len(linhasDepois) > offsetLog {
		fatiaLogs = linhasDepois[offsetLog:]
	}

	res := &CenarioResultado{
		Nome:               nome,
		Descricao:          descricao,
		Politica:           politica,
		NumClientes:        numClientes,
		NumServidores:      numServidores,
		PortaBase:          portaBase,
		IntervaloMonitor:   intervaloMon,
		DuracaoDesc:        duracaoDesc,
		TempoCompilacaoMs:  tempoCompMs,
		TempoBootInfraMs:   tempoBootMs,
		DuracaoTotalSec:    duracaoTotal,
		ConexoesPorPorta:   make(map[int]int),
		RequisicoesPorPort: make(map[int]int),
		TempoDeteccaoSec:   tempoDeteccao,
		Snapshots:          snapshots,
		LinhasLog:          fatiaLogs,
	}

	for _, p := range portasSrv {
		res.ConexoesPorPorta[p] = 0
		res.RequisicoesPorPort[p] = 0
	}

	analisarLogsCenario(res, inicioCenario)
	return res
}

func formatarListaFloat(vals []float64) string {
	partes := make([]string, len(vals))
	for i, v := range vals {
		partes[i] = fmt.Sprintf("%.1f", v)
	}
	return "[" + strings.Join(partes, ", ") + "]"
}

func formatarListaInt(vals []int) string {
	partes := make([]string, len(vals))
	for i, v := range vals {
		partes[i] = strconv.Itoa(v)
	}
	return "[" + strings.Join(partes, ", ") + "]"
}

func formatarListaStr(vals []string) string {
	partes := make([]string, len(vals))
	for i, v := range vals {
		partes[i] = fmt.Sprintf("%q", v)
	}
	return "[" + strings.Join(partes, ", ") + "]"
}

func gerarRelatorioMarkdown(caminhoRelatorio string, resultados []*CenarioResultado, tempoTotalSuite time.Duration, temposBuild map[string]float64) error {
	var b strings.Builder

	b.WriteString("# Relatório Completo de Execução, Desempenho e Tolerância a Falhas\n\n")
	b.WriteString(fmt.Sprintf("- **Data/Hora da Execução:** `%s`\n", time.Now().Format("2006-01-02 15:04:05")))
	b.WriteString(fmt.Sprintf("- **Tempo Total da Suíte de Testes:** `%.2fs`\n", tempoTotalSuite.Seconds()))
	b.WriteString(fmt.Sprintf("- **Arquivo de Log Analisado:** [`%s`](file:///home/vhwcm/Trabalho-redes-2/%s)\n\n", rede.CaminhoLog, rede.CaminhoLog))

	b.WriteString("---\n\n")
	b.WriteString("## 1. Parâmetros Globais e Tempos de Inicialização / Compilação\n\n")
	b.WriteString("| Componente / Pacote | Porta | Tempo de Compilação (ms) | Papel na Arquitetura |\n")
	b.WriteString("| :--- | :--- | :---: | :--- |\n")
	b.WriteString(fmt.Sprintf("| `./scripts/iniciar_servidores` | `-` | `%.1f ms` | Spawner de `M` processos de servidor |\n", temposBuild["servidores"]))
	b.WriteString(fmt.Sprintf("| `./scripts/iniciar_clientes` | Efêmera | `%.1f ms` | Spawner de `N` processos de cliente |\n", temposBuild["clientes"]))
	b.WriteString(fmt.Sprintf("| `./monitor` | `:9000` (fixa) | `%.1f ms` | Health Check (`PING`), Tabela em RAM e Auto-recuperação (5s) |\n", temposBuild["monitor"]))
	b.WriteString(fmt.Sprintf("| `./loadBalance` | `:8080` | `%.1f ms` | Balanceador TCP concorrente (`Round-Robin` / `Least Connections`) |\n\n", temposBuild["lb"]))

	b.WriteString("### Resumo Comparativo dos Cenários\n\n")
	b.WriteString("| Cenário | Política | Clientes | Servidores | Tempo de Boot Infra | Duração Total | Conexões (LB) | Requisições Atendidas | Vazão (req/s) | Vazão (conn/s) | Quedas / Recuperações |\n")
	b.WriteString("| :--- | :---: | :---: | :---: | :---: | :---: | :---: | :---: | :---: | :---: | :---: |\n")

	var nomesCenarios []string
	var reqPorSegundo []float64
	var bootMsList []float64

	for _, r := range resultados {
		b.WriteString(fmt.Sprintf("| **%s** | `%s` | `%d` (%s) | `%d` (`:%d..:%d`) | `%.1f ms` | `%.2fs` | `%d` | `%d` | `%.2f req/s` | `%.2f conn/s` | `%d` / `%d` |\n",
			r.Nome, r.Politica, r.NumClientes, r.DuracaoDesc, r.NumServidores,
			r.PortaBase, r.PortaBase+r.NumServidores-1,
			r.TempoBootInfraMs, r.DuracaoTotalSec, r.TotalConexoesLB, r.TotalRequisicoes,
			r.ThroughputReqSec, r.ThroughputConnSec, r.QuedasDetectadas, r.Recuperacoes))
		nomesCenarios = append(nomesCenarios, r.Nome)
		reqPorSegundo = append(reqPorSegundo, math.Round(r.ThroughputReqSec*10)/10)
		bootMsList = append(bootMsList, math.Round(r.TempoBootInfraMs))
	}
	b.WriteString("\n")

	b.WriteString("```mermaid\n")
	b.WriteString("xychart-beta\n")
	b.WriteString("    title \"Vazao Media por Cenario (Requisicoes / segundo)\"\n")
	b.WriteString(fmt.Sprintf("    x-axis %s\n", formatarListaStr(nomesCenarios)))
	b.WriteString("    y-axis \"Requisicoes por segundo\"\n")
	b.WriteString(fmt.Sprintf("    bar %s\n", formatarListaFloat(reqPorSegundo)))
	b.WriteString("```\n\n")

	b.WriteString("---\n\n")

	for idx, r := range resultados {
		b.WriteString(fmt.Sprintf("## %d. Detalhamento — %s\n\n", idx+2, r.Nome))
		b.WriteString(fmt.Sprintf("> %s\n\n", r.Descricao))

		b.WriteString("### Parâmetros e Métricas de Velocidade\n\n")
		b.WriteString(fmt.Sprintf("- **Política de Balanceamento:** `%s`\n", r.Politica))
		b.WriteString(fmt.Sprintf("- **Servidores Monitorados:** `%d` (portas `:%d` a `:%d`, intervalo do monitor `%ds`)\n",
			r.NumServidores, r.PortaBase, r.PortaBase+r.NumServidores-1, r.IntervaloMonitor))
		b.WriteString(fmt.Sprintf("- **Clientes Simulados:** `%d` (`%s`)\n", r.NumClientes, r.DuracaoDesc))
		b.WriteString(fmt.Sprintf("- **Tempo de Inicialização (Boot até todos os servidores `UP`):** `%.1f ms`\n", r.TempoBootInfraMs))
		b.WriteString(fmt.Sprintf("- **Duração Total do Cenário:** `%.2f s`\n", r.DuracaoTotalSec))
		b.WriteString(fmt.Sprintf("- **Velocidade / Vazão Média:** `%.2f req/s` (`%d` requisições totais) | `%.2f conexões/s` (`%d` conexões totais)\n",
			r.ThroughputReqSec, r.TotalRequisicoes, r.ThroughputConnSec, r.TotalConexoesLB))
		if r.QuedasDetectadas > 0 || r.Recuperacoes > 0 {
			b.WriteString(fmt.Sprintf("- **Tolerância a Falhas:** `%d` queda(s) detectada(s) | `%d` auto-recuperação(ões) executada(s) | Tempo entre detecção `DOWN` e retorno `UP`: `%.2f s`\n",
				r.QuedasDetectadas, r.Recuperacoes, r.TempoRecuperacaoS))
		}
		b.WriteString("\n")

		// Tabela de distribuição por servidor
		b.WriteString("### Distribuição de Carga por Servidor\n\n")
		b.WriteString("| Servidor (Porta) | Conexões Recebidas (LB) | % das Conexões | Requisições Atendidas | % das Requisições |\n")
		b.WriteString("| :--- | :---: | :---: | :---: | :---: |\n")

		portas := make([]int, 0, len(r.ConexoesPorPorta))
		for p := range r.ConexoesPorPorta {
			portas = append(portas, p)
		}
		sort.Ints(portas)

		var labelsPortas []string
		var connVals []int
		var reqVals []int

		for _, p := range portas {
			conns := r.ConexoesPorPorta[p]
			reqs := r.RequisicoesPorPort[p]
			pctConn := 0.0
			if r.TotalConexoesLB > 0 {
				pctConn = (float64(conns) * 100.0) / float64(r.TotalConexoesLB)
			}
			pctReq := 0.0
			if r.TotalRequisicoes > 0 {
				pctReq = (float64(reqs) * 100.0) / float64(r.TotalRequisicoes)
			}
			b.WriteString(fmt.Sprintf("| `127.0.0.1:%d` | `%d` | `%.1f%%` | `%d` | `%.1f%%` |\n",
				p, conns, pctConn, reqs, pctReq))
			labelsPortas = append(labelsPortas, fmt.Sprintf(":%d", p))
			connVals = append(connVals, conns)
			reqVals = append(reqVals, reqs)
		}
		b.WriteString("\n")

		b.WriteString("```mermaid\n")
		b.WriteString("xychart-beta\n")
		b.WriteString(fmt.Sprintf("    title \"Distribuicao por Servidor - %s (Conexoes vs Requisicoes)\"\n", r.Nome))
		b.WriteString(fmt.Sprintf("    x-axis %s\n", formatarListaStr(labelsPortas)))
		b.WriteString("    y-axis \"Quantidade\"\n")
		b.WriteString(fmt.Sprintf("    bar %s\n", formatarListaInt(connVals)))
		b.WriteString(fmt.Sprintf("    line %s\n", formatarListaInt(reqVals)))
		b.WriteString("```\n\n")

		// Gráfico de linha do tempo de conexões ativas
		if len(r.Snapshots) > 0 {
			b.WriteString("### Evolução Temporal de Conexões Ativas e Status dos Servidores\n\n")

			var labelsTempo []string
			var somaAtivas []int
			var servidoresUP []int

			passo := 1
			if len(r.Snapshots) > 20 {
				passo = len(r.Snapshots) / 20
			}

			for i := 0; i < len(r.Snapshots); i += passo {
				s := r.Snapshots[i]
				labelsTempo = append(labelsTempo, fmt.Sprintf("%.1fs", s.ElapsedSec))
				ativas := 0
				up := 0
				for _, p := range portas {
					ativas += s.Conexoes[p]
					if s.Status[p] == "UP" {
						up++
					}
				}
				somaAtivas = append(somaAtivas, ativas)
				servidoresUP = append(servidoresUP, up)
			}

			b.WriteString("```mermaid\n")
			b.WriteString("xychart-beta\n")
			b.WriteString(fmt.Sprintf("    title \"Linha do Tempo - Conexoes Ativas (barra) e Servidores UP (linha) [%s]\"\n", r.Nome))
			b.WriteString(fmt.Sprintf("    x-axis %s\n", formatarListaStr(labelsTempo)))
			b.WriteString("    y-axis \"Quantidade\"\n")
			b.WriteString(fmt.Sprintf("    bar %s\n", formatarListaInt(somaAtivas)))
			b.WriteString(fmt.Sprintf("    line %s\n", formatarListaInt(servidoresUP)))
			b.WriteString("```\n\n")
		}

		// Tabela de linha do tempo de eventos críticos extraídos do log.txt
		b.WriteString("### Linha do Tempo de Eventos Extraídos do `log.txt`\n\n")
		b.WriteString("| Tempo Relativo (s) | Componente | Evento |\n")
		b.WriteString("| :---: | :---: | :--- |\n")
		maxEventos := len(r.Eventos)
		if maxEventos > 30 {
			maxEventos = 30
		}
		for i := 0; i < maxEventos; i++ {
			ev := r.Eventos[i]
			b.WriteString(fmt.Sprintf("| `+%.3fs` | `%s` | %s |\n", ev.ElapsedSec, ev.Componente, ev.Descricao))
		}
		b.WriteString("\n---\n\n")
	}

	return os.WriteFile(caminhoRelatorio, []byte(b.String()), 0o644)
}

func main() {
	portaBase := flag.Int("base", 8081, "Porta base dos servidores nos testes")
	outRelatorio := flag.String("out", "RELATORIO_TESTES.md", "Arquivo Markdown de saída do relatório")
	flag.Parse()

	inicioSuite := time.Now()
	_ = os.WriteFile(rede.CaminhoLog, nil, 0o644)

	fmt.Println("==================================================================")
	fmt.Println("  SUÍTE COMPLETA DE TESTES, QUEDAS E BENCHMARK - BALANCEADOR TCP")
	fmt.Println("==================================================================")

	temposBuild := make(map[string]float64)

	servidoresBin, d1, err := compilarBinario("srv_script", "./scripts/iniciar_servidores")
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	defer os.Remove(servidoresBin)
	temposBuild["servidores"] = float64(d1.Microseconds()) / 1000.0

	clientesBin, d2, err := compilarBinario("cli_script", "./scripts/iniciar_clientes")
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	defer os.Remove(clientesBin)
	temposBuild["clientes"] = float64(d2.Microseconds()) / 1000.0

	monitorBin, d3, err := compilarBinario("mon_bin", "./monitor")
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	defer os.Remove(monitorBin)
	temposBuild["monitor"] = float64(d3.Microseconds()) / 1000.0

	lbBin, d4, err := compilarBinario("lb_bin", "./loadBalance")
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	defer os.Remove(lbBin)
	temposBuild["lb"] = float64(d4.Microseconds()) / 1000.0

	buildTotalMs := temposBuild["servidores"] + temposBuild["clientes"] + temposBuild["monitor"] + temposBuild["lb"]

	var resultados []*CenarioResultado

	// CENÁRIO 1: Round-Robin com carga uniforme
	fmt.Println("\n>>> Executando Cenário 1: Round-Robin (6 clientes, 3 servidores)...")
	c1 := executarCenario(
		"Cenario 1 - Round-Robin",
		"Valida a distribuição circular uniforme de 6 clientes simultâneos entre 3 servidores backend usando a política Round-Robin.",
		"round-robin",
		3, *portaBase, 1,
		servidoresBin, clientesBin, monitorBin, lbBin,
		buildTotalMs,
		func(inicio time.Time) (int, string, float64) {
			cmdCli := exec.Command(clientesBin, "-n", "6", "--", "-k", "3", "-intervalo", "400")
			cmdCli.Stdout = os.Stdout
			cmdCli.Stderr = os.Stderr
			_ = cmdCli.Run()
			return 6, "fixa 3s", 0
		},
	)
	resultados = append(resultados, c1)

	// CENÁRIO 2: Least Connections com cargas assimétricas e chegadas escalonadas
	fmt.Println("\n>>> Executando Cenário 2: Least Connections Assimétrico (2 ondas de clientes, 3 servidores)...")
	c2 := executarCenario(
		"Cenario 2 - Least Connections",
		"Valida a política Least Connections com clientes de durações variáveis (2s a 5s) chegando em duas ondas, direcionando novas conexões para os servidores menos ocupados.",
		"least-connections",
		3, *portaBase, 1,
		servidoresBin, clientesBin, monitorBin, lbBin,
		buildTotalMs,
		func(inicio time.Time) (int, string, float64) {
			rede.Log("CENARIO", "Disparando Onda 1: 2 clientes longos (5s)")
			onda1 := exec.Command(clientesBin, "-n", "2", "--", "-k", "5", "-intervalo", "400")
			onda1.Stdout = os.Stdout
			onda1.Stderr = os.Stderr
			_ = onda1.Start()

			time.Sleep(1500 * time.Millisecond)

			rede.Log("CENARIO", "Disparando Onda 2: 4 clientes de duração variável (2s a 3s)")
			onda2 := exec.Command(clientesBin, "-n", "4", "--", "-k-min", "2", "-k-max", "3", "-intervalo", "400")
			onda2.Stdout = os.Stdout
			onda2.Stderr = os.Stderr
			_ = onda2.Run()
			_ = onda1.Wait()

			return 6, "ondas 5s + 2..3s", 0
		},
	)
	resultados = append(resultados, c2)

	// CENÁRIO 3: Simulação de Queda de Servidor e Auto-recuperação pelo Monitor (5s)
	fmt.Println("\n>>> Executando Cenário 3: Tolerância a Falhas e Auto-Recuperação (Queda + Recovery 5s)...")
	c3 := executarCenario(
		"Cenario 3 - Falha e Auto-Recuperacao",
		"Simula a queda abrupta (SIGKILL) do servidor primário durante a execução, verificando a detecção de falha pelo Monitor (PING), o redirecionamento de novos clientes para servidores ativos e a auto-recuperação em 5 segundos.",
		"least-connections",
		3, *portaBase, 1,
		servidoresBin, clientesBin, monitorBin, lbBin,
		buildTotalMs,
		func(inicio time.Time) (int, string, float64) {
			portaAlvo := *portaBase
			rede.Log("CENARIO", "Disparando Onda 1: 3 clientes iniciais (3s)")
			onda1 := exec.Command(clientesBin, "-n", "3", "--", "-k", "3", "-intervalo", "400")
			onda1.Stdout = os.Stdout
			onda1.Stderr = os.Stderr
			_ = onda1.Start()

			time.Sleep(1200 * time.Millisecond)

			tKill := time.Now()
			rede.Log("CENARIO", "FALHA INJETADA: derrubando servidor na porta :%d (SIGKILL)", portaAlvo)
			_ = matarServidorNaPorta(portaAlvo)

			// Aguarda monitor detectar DOWN
			var tempoDeteccao float64
			for i := 0; i < 35; i++ {
				if t, err := consultarMonitor(); err == nil {
					for _, s := range t.Servidores {
						if s.Porta == portaAlvo && s.Status == "DOWN" {
							tempoDeteccao = time.Since(tKill).Seconds()
							break
						}
					}
				}
				if tempoDeteccao > 0 {
					break
				}
				time.Sleep(100 * time.Millisecond)
			}

			rede.Log("CENARIO", "Disparando Onda 2 (durante falha de :%d, detectada em %.2fs): 2 clientes (1s)", portaAlvo, tempoDeteccao)
			onda2 := exec.Command(clientesBin, "-n", "2", "--", "-k", "1", "-intervalo", "300")
			onda2.Stdout = os.Stdout
			onda2.Stderr = os.Stderr
			_ = onda2.Run()
			_ = onda1.Wait()

			// Aguarda monitor auto-recuperar o servidor caído (ciclo de 5s)
			aguardarTodosServidoresUP(3, 7*time.Second)

			rede.Log("CENARIO", "Disparando Onda 3 (após auto-recuperação de :%d): 3 clientes (2s)", portaAlvo)
			onda3 := exec.Command(clientesBin, "-n", "3", "--", "-k", "2", "-intervalo", "400")
			onda3.Stdout = os.Stdout
			onda3.Stderr = os.Stderr
			_ = onda3.Run()

			return 8, "3 ondas (pré-queda, durante queda, pós-recuperação)", tempoDeteccao
		},
	)
	resultados = append(resultados, c3)

	tempoTotalSuite := time.Since(inicioSuite)
	if err := gerarRelatorioMarkdown(*outRelatorio, resultados, tempoTotalSuite, temposBuild); err != nil {
		fmt.Fprintf(os.Stderr, "Erro ao gerar relatório: %v\n", err)
		os.Exit(1)
	}

	fmt.Println("\n==================================================================")
	fmt.Printf("  SUÍTE FINALIZADA EM %.2fs! RELATÓRIO GERADO EM: %s\n", tempoTotalSuite.Seconds(), *outRelatorio)
	fmt.Println("==================================================================")
}
