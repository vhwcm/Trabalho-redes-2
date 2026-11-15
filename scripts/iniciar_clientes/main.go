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
	flagN := flag.Int("n", 0, "Número de processos de cliente a iniciar")
	flag.Parse()

	n := *flagN
	extraArgs := flag.Args()

	if n <= 0 && len(extraArgs) > 0 {
		val, err := strconv.Atoi(extraArgs[0])
		if err != nil || val <= 0 {
			fmt.Fprintln(os.Stderr, "Erro: informe um número válido de clientes.")
			os.Exit(1)
		}
		n = val
		extraArgs = extraArgs[1:]
	}

	if n <= 0 {
		fmt.Fprintln(os.Stderr, "Uso: go run ./scripts/iniciar_clientes -n <numero_de_clientes> [args_do_cliente...]")
		os.Exit(1)
	}

	tmpFile, err := os.CreateTemp("", "cliente_bin_*")
	if err != nil {
		fmt.Fprintf(os.Stderr, "Erro ao criar arquivo temporário: %v\n", err)
		os.Exit(1)
	}
	tmpBin := tmpFile.Name()
	tmpFile.Close()
	defer os.Remove(tmpBin)

	fmt.Println("Compilando cliente...")
	buildCmd := exec.Command("go", "build", "-o", tmpBin, "./cliente")
	buildCmd.Stdout = os.Stdout
	buildCmd.Stderr = os.Stderr
	if err := buildCmd.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "Erro ao compilar cliente: %v\n", err)
		os.Exit(1)
	}

	var cmds []*exec.Cmd
	var wg sync.WaitGroup

	fmt.Printf("Iniciando %d processos de cliente...\n", n)
	for i := 0; i < n; i++ {
		cmd := exec.Command(tmpBin, extraArgs...)
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr

		if err := cmd.Start(); err != nil {
			fmt.Fprintf(os.Stderr, "Erro ao iniciar cliente %d: %v\n", i+1, err)
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
		fmt.Println("\nEncerrando clientes...")
		for _, cmd := range cmds {
			if cmd.Process != nil {
				_ = cmd.Process.Signal(syscall.SIGTERM)
			}
		}
		wg.Wait()
		fmt.Println("Todos os processos de cliente foram encerrados.")
	case <-done:
	}
}
