package main

import (
	"flag"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"strconv"
	"sync"
	"syscall"
)

func main() {
	flagN := flag.Int("n", 0, "Número de processos de servidor a iniciar")
	portaBase := flag.Int("base", 8081, "Porta inicial dos servidores")
	flag.Parse()

	n := *flagN
	if n <= 0 && flag.NArg() > 0 {
		val, err := strconv.Atoi(flag.Arg(0))
		if err != nil || val <= 0 {
			fmt.Fprintln(os.Stderr, "Erro: informe um número válido de servidores.")
			os.Exit(1)
		}
		n = val
	}

	if n <= 0 || *portaBase <= 0 {
		fmt.Fprintln(os.Stderr, "Uso: go run ./scripts/iniciar_servidores -n <numero_de_servidores> [-base <porta_base>]")
		os.Exit(1)
	}

	tmpFile, err := os.CreateTemp("", "servidor_bin_*")
	if err != nil {
		fmt.Fprintf(os.Stderr, "Erro ao criar arquivo temporário: %v\n", err)
		os.Exit(1)
	}
	tmpBin := tmpFile.Name()
	tmpFile.Close()
	defer os.Remove(tmpBin)

	fmt.Println("Compilando servidor...")
	buildCmd := exec.Command("go", "build", "-o", tmpBin, "./servidor")
	buildCmd.Stdout = os.Stdout
	buildCmd.Stderr = os.Stderr
	if err := buildCmd.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "Erro ao compilar servidor: %v\n", err)
		os.Exit(1)
	}

	var cmds []*exec.Cmd
	var wg sync.WaitGroup

	fmt.Printf("Iniciando %d processos de servidor a partir da porta %d...\n", n, *portaBase)
	for i := 0; i < n; i++ {
		porta := *portaBase + i
		cmd := exec.Command(tmpBin, "-port", strconv.Itoa(porta))
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr

		if err := cmd.Start(); err != nil {
			fmt.Fprintf(os.Stderr, "Erro ao iniciar servidor na porta %d: %v\n", porta, err)
			continue
		}

		cmds = append(cmds, cmd)
		wg.Add(1)
		go func(c *exec.Cmd) {
			defer wg.Done()
			_ = c.Wait()
		}(cmd)
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
		fmt.Println("\nEncerrando servidores...")
		for _, cmd := range cmds {
			if cmd.Process != nil {
				_ = cmd.Process.Signal(syscall.SIGTERM)
			}
		}
		wg.Wait()
		fmt.Println("Todos os processos de servidor foram encerrados.")
	case <-done:
	}
}
