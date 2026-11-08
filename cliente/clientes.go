package main

import (
	"fmt"
	"io"
	"net/http"
	"time"
)

func fazerRequisicao(id int, url string) {
	resp, err := http.Get(url)
	if err != nil {
		fmt.Printf("[Cliente %d] Erro: %v\n", id, err)
		return
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	fmt.Printf("[Req #%d] Status: %d | Resposta: %s", id, resp.StatusCode, string(body))
}

func main() {
	url := "http://localhost:8080"
	totalRequisicoes := 10

	fmt.Printf("Enviando %d requisições para o em %s...\n\n", totalRequisicoes, url)

	for i := 1; i <= totalRequisicoes; i++ {
		fazerRequisicao(i, url)
		time.Sleep(300 * time.Millisecond) 
	}
}