package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"strconv"
	"sync"
	"syscall"
	"time"
	"trabalho-redes/rede"
)

func compilarBinario(prefixo, pacote string) (string, error) {
	tmpFile, err := os.CreateTemp("", prefixo+"_*")
	if err != nil {
		return "", fmt.Errorf("erro ao criar arquivo temporário para %s: %w", pacote, err)
	}
	tmpBin := tmpFile.Name()
	tmpFile.Close()

	fmt.Printf("Compilando %s...\n", pacote)
	buildCmd := exec.Command("go", "build", "-o", tmpBin, pacote)
	buildCmd.Stdout = os.Stdout
	buildCmd.Stderr = os.Stderr
	if err := buildCmd.Run(); err != nil {
		os.Remove(tmpBin)
		return "", fmt.Errorf("erro ao compilar %s: %w", pacote, err)
	}

	return tmpBin, nil
}

func iniciarProcesso(binario string, args []string, cmds *[]*exec.Cmd, wg *sync.WaitGroup) error {
	cmd := exec.Command(binario, args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if err := cmd.Start(); err != nil {
		return err
	}

	*cmds = append(*cmds, cmd)
	wg.Add(1)
	go func(c *exec.Cmd) {
		defer wg.Done()
		_ = c.Wait()
	}(cmd)

	return nil
}

func aguardarMonitorPronto(timeout time.Duration) {
	limite := time.Now().Add(timeout)
	for time.Now().Before(limite) {
		connMon, errMon := rede.ConectarTCPTimeout(9000, 200*time.Millisecond)
		if errMon == nil {
			_ = connMon.SetDeadline(time.Now().Add(300 * time.Millisecond))
			_ = rede.EnviarMensagem(connMon, &rede.Mensagem{
				Tipo:     rede.TipoRequisicao,
				Porta:    9000,
				Conteudo: "GET_STATUS",
			})
			resp, errResp := rede.LerMensagem(connMon)
			_ = connMon.Close()

			if errResp == nil {
				var tabela struct {
					Servidores []struct {
						Status string `json:"status"`
					} `json:"servidores"`
				}
				if json.Unmarshal([]byte(resp.Conteudo), &tabela) == nil {
					for _, s := range tabela.Servidores {
						if s.Status == "UP" {
							time.Sleep(200 * time.Millisecond)
							return
						}
					}
				}
			}
		}
		time.Sleep(200 * time.Millisecond)
	}
}

func main() {
	flagN := flag.Int("n", 0, "Número de processos de cliente a iniciar")
	flagM := flag.Int("m", 0, "Número de processos de servidor a iniciar")
	portaBase := flag.Int("base", 8081, "Porta inicial dos servidores")
	intervalo := flag.Int("intervalo", 2, "Intervalo em segundos entre checagens do monitor")
	politica := flag.String("politica", "least-connections", "Política do Load Balancer ('round-robin' ou 'least-connections')")
	duracao := flag.Int("k", 5, "Duração fixa em segundos que cada cliente permanece conectado")
	duracaoMin := flag.Int("k-min", 0, "Duração mínima em segundos dos clientes (opcional, para duração variável)")
	duracaoMax := flag.Int("k-max", 0, "Duração máxima em segundos dos clientes (opcional, para duração variável)")
	flag.Parse()

	n := *flagN
	m := *flagM

	if n <= 0 && m <= 0 && flag.NArg() >= 2 {
		valN, errN := strconv.Atoi(flag.Arg(0))
		valM, errM := strconv.Atoi(flag.Arg(1))
		if errN != nil || errM != nil || valN <= 0 || valM <= 0 {
			fmt.Fprintln(os.Stderr, "Erro: informe números válidos para clientes e servidores.")
			os.Exit(1)
		}
		n = valN
		m = valM
	}

	usarDuracaoVariavel := *duracaoMin > 0 || *duracaoMax > 0
	if usarDuracaoVariavel && (*duracaoMin <= 0 || *duracaoMax < *duracaoMin) {
		fmt.Fprintln(os.Stderr, "Erro: informe valores válidos com 0 < -k-min <= -k-max.")
		os.Exit(1)
	}

	if n <= 0 || m <= 0 || *portaBase <= 0 || *intervalo <= 0 || (!usarDuracaoVariavel && *duracao <= 0) {
		fmt.Fprintln(os.Stderr, "Uso: go run ./scripts/geral.go -n <clientes> -m <servidores> [-base 8081] [-intervalo 2] [-politica least-connections] [-k 5 | -k-min 2 -k-max 8]")
		os.Exit(1)
	}

	servidoresScriptBin, err := compilarBinario("iniciar_servidores_bin", "./scripts/iniciar_servidores")
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	defer os.Remove(servidoresScriptBin)

	clientesScriptBin, err := compilarBinario("iniciar_clientes_bin", "./scripts/iniciar_clientes")
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	defer os.Remove(clientesScriptBin)

	monitorBin, err := compilarBinario("monitor_bin", "./monitor")
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	defer os.Remove(monitorBin)

	lbBin, err := compilarBinario("lb_bin", "./loadBalance")
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	defer os.Remove(lbBin)

	var cmds []*exec.Cmd
	var wg sync.WaitGroup

	rede.Log("GERAL", "=== Nova execução: %d clientes, %d servidores (base=%d), política=%s ===", n, m, *portaBase, *politica)

	// 1. Iniciar M servidores usando ./scripts/iniciar_servidores
	rede.Log("GERAL", "Executando script de servidores (-n %d -base %d)...", m, *portaBase)
	servidoresArgs := []string{"-n", strconv.Itoa(m), "-base", strconv.Itoa(*portaBase)}
	if err := iniciarProcesso(servidoresScriptBin, servidoresArgs, &cmds, &wg); err != nil {
		rede.LogErro("GERAL", "Erro ao iniciar script de servidores: %v", err)
	}

	// 2. Iniciar 1 monitor (porta fixa 9000)
	rede.Log("GERAL", "Iniciando 1 monitor (porta 9000)...")
	monitorArgs := []string{"-n", strconv.Itoa(m), "-base", strconv.Itoa(*portaBase), "-intervalo", strconv.Itoa(*intervalo)}
	if err := iniciarProcesso(monitorBin, monitorArgs, &cmds, &wg); err != nil {
		rede.LogErro("GERAL", "Erro ao iniciar monitor: %v", err)
	}

	// 3. Iniciar 1 load balancer
	rede.Log("GERAL", "Iniciando 1 load balancer (política: %s)...", *politica)
	lbArgs := []string{"-politica", *politica}
	if err := iniciarProcesso(lbBin, lbArgs, &cmds, &wg); err != nil {
		rede.LogErro("GERAL", "Erro ao iniciar load balancer: %v", err)
	}

	// Aguardar monitor confirmar servidores UP antes de iniciar os clientes
	aguardarMonitorPronto(5 * time.Second)

	// 4. Iniciar N clientes usando ./scripts/iniciar_clientes
	var clientesArgs []string
	if usarDuracaoVariavel {
		rede.Log("GERAL", "Executando script de clientes (-n %d, duração variável %d-%ds)...", n, *duracaoMin, *duracaoMax)
		clientesArgs = []string{"-n", strconv.Itoa(n), "--", "-k-min", strconv.Itoa(*duracaoMin), "-k-max", strconv.Itoa(*duracaoMax)}
	} else {
		rede.Log("GERAL", "Executando script de clientes (-n %d, duração %ds)...", n, *duracao)
		clientesArgs = []string{"-n", strconv.Itoa(n), "--", "-k", strconv.Itoa(*duracao)}
	}
	if err := iniciarProcesso(clientesScriptBin, clientesArgs, &cmds, &wg); err != nil {
		rede.LogErro("GERAL", "Erro ao iniciar script de clientes: %v", err)
	}

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)

	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()

	select {
	case <-sigChan:
		rede.Log("GERAL", "Encerrando todos os processos...")
		for _, cmd := range cmds {
			if cmd.Process != nil {
				_ = cmd.Process.Signal(syscall.SIGTERM)
			}
		}
		wg.Wait()
		rede.Log("GERAL", "Todos os processos foram encerrados.")
	case <-done:
		rede.Log("GERAL", "Todos os processos finalizaram.")
	}
}
