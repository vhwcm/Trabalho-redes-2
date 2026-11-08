package main

import (
	"fmt"
	"net/http"
)


func handler(w http.ResponseWriter, r *http.Request) {


}

func main() {
	port := "8080"
	http.HandleFunc("/", handler);
	
	fmt.Println("Iniciando Load Balance")
	err := http.ListenAndServe(port, nil)
	if err != nil {
		fmt.Println("Erro ao iniciar load balance")
	}

}