package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"syscall"
)

type Servidor struct {
	ID       int    `json:"id"`
	IP       string `json:"ip"`
	Porta    int    `json:"porta"`
	Status   string `json:"status"`
	Conexoes int    `json:"conexoes"`
}

type TabelaServidores struct {
	TotalConexoes int         `json:"total_conexoes"`
	Servidores    []*Servidor `json:"servidores"`
}

var tabela = &TabelaServidores{
	TotalConexoes: 0,
	Servidores:    make([]*Servidor, 0),
}

func tabelaServidoresStatus(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	err := json.NewEncoder(w).Encode(tabela)
	if err != nil {
		http.Error(w, "Erro ao gerar JSON", http.StatusInternalServerError)
	}
}

func main() {
	flagN := flag.Int("n", 0, "Numero de servidores a serem criados")
	portaBase := flag.Int("base", 8081, "Porta inicial dos servidores")
	portaStatus := flag.Int("status-port", 9000, "Porta do /status")
	flag.Parse()

	totalServidores := *flagN

	if totalServidores <= 0 && len(flag.Args()) > 0 {
		var err error
		totalServidores, err = strconv.Atoi(flag.Arg(0))
		if err != nil || totalServidores <= 0 {
			fmt.Println("Erro: informe um número válido de servidores.")
			os.Exit(1)
		}
	}

	if totalServidores <= 0 {
		fmt.Println("Uso: go run ./servidor <numero_de_servidores>")
		os.Exit(1)
	}

	fmt.Printf("Iniciando %d servidores a partir da porta %d...\n\n", totalServidores, *portaBase)

	for i := 0; i < totalServidores; i++ {
		porta := *portaBase + i

		servidor := &Servidor{
			ID:       i + 1,
			IP:       "127.0.0.1",
			Porta:    porta,
			Status:   "DOWN",
			Conexoes: 0,
		}

		tabela.Servidores = append(tabela.Servidores, servidor)
		go iniciaServer(servidor)
	}

	http.HandleFunc("/status", tabelaServidoresStatus)

	go func() {
		addr := fmt.Sprintf(":%d", *portaStatus)
		fmt.Printf("\n[STATUS] Endpoint de métricas rodando em http://localhost%s/status\n\n", addr)
		if err := http.ListenAndServe(addr, nil); err != nil {
			fmt.Printf("Erro no servidor de status HTTP: %v\n", err)
		}
	}()

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)

	fmt.Println("Servidores ativos. Pressione Ctrl+C para encerrar.")
	<-sigChan

	for _, s := range tabela.Servidores {
		s.Status = "DOWN"
	}
	fmt.Println("\nTodos os servidores foram finalizados.")
}
