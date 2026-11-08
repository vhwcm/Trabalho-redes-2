package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"strconv"
	"syscall"
)



type Servidor struct {
	ID int
	Porta int
	IP int
	Ativo bool
	Conexoes int
}

type TabelaServidores struct {
	TotalConexoes int
	Servidores []*Servidor
}

var tabela = &TabelaServidores{
	TotalConexoes: 0,
	Servidores:    make([]*Servidor, 0),
}

func tabelaServidoresStatus(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	// Serializa a tabela e escreve diretamente no w (corpo da resposta)
	err := json.NewEncoder(w).Encode(tabela)
	if err != nil {
		http.Error(w, "Erro ao gerar JSON", http.StatusInternalServerError)
	}
}

func main() {
	flagN := flag.Int("n", 0, "Numero de servidores a serem cirados")
	portaBase := flag.Int("base",8081, "porta inicial")
	flag.Parse();
	
	totalServidores := *flagN

	if totalServidores <= 0 && len(flag.Args()) > 0 {
		var err error
		totalServidores, err = strconv.Atoi(flag.Arg(0))
		if err != nil || totalServidores <= 0 {
			fmt.Println("Número inválido de servidores")
			os.Exit(1);
		}
	}

	fmt.Printf("Iniciando %d servidores a partir da porta %d...\n\n", totalServidores, *portaBase)

	// array de comandos para que depois a gente possa cancela-los
	var comandos []*exec.Cmd

	// geração dos servidores
	for i := 0; i < totalServidores; i++ {
		porta := *portaBase + i
	
		cmd := exec.Command("go", "run", "server.go", fmt.Sprintf("-port=%d",porta))

		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr

		servidor := Servidor {
				ID: i,
				Porta: porta,
				IP: porta,
				Conexoes: 0,
		}
		if err := cmd.Start(); err != nil {
			fmt.Printf("[ERRO] Falha ao iniciar servidor na porta %d: %v\n", porta, err)
			servidor.Ativo = false
			continue
		} else {
			servidor.Ativo = true
		}

		tabela.Servidores = append(tabela.Servidores, &servidor);


		fmt.Printf("Servidor iniciado na porta %d (PID: %d)\n", porta, cmd.Process.Pid)
		
		// lista de comandos executados
		comandos = append(comandos, cmd)
	}

	http.HandleFunc("/status", tabelaServidoresStatus)
	// Inicia o servidor HTTP em segundo plano para não travar o script
	go func() {
		portaStatus := ":9000"
		if err := http.ListenAndServe(portaStatus, nil); err != nil {
			fmt.Printf("Erro no servidor de status: %v\n", err)
		}
	}()

	sigChan := make(chan os.Signal,1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)
	<-sigChan
	fmt.Println("\n encerrando....")
	for _, cmd := range comandos {
		if cmd.Process != nil {
			_ = cmd.Process.Kill()
		}
	}
	
	fmt.Println("Todos os servidores foram encerrados.")
}