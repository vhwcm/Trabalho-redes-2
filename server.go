package main

import (
	"flag"
	"fmt"
	"net/http"
)

// resposta das requisições
func homeHandler(w http.ResponseWriter, r *http.Request) {
	fmt.Fprintf(w, "KAMEHAMEHAMEHAAAAAAAAAAAA\n")
}

func main() {
	portaNum := flag.Int("port", 8080, "Porta do servidor")
	flag.Parse()

	http.HandleFunc("/", homeHandler)

	portaStr := fmt.Sprintf(":%d", *portaNum)
	fmt.Printf("Servidor subindo na porta %s\n", portaStr)

	err := http.ListenAndServe(portaStr, nil)
	if err != nil {
		fmt.Printf("Erro ao iniciar servidor na porta %s: %s\n", portaStr, err)
	}
}