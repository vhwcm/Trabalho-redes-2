package main

import (
	"fmt"
	"net"
	"flag"
	"bufio"
	"log"
)

func tratar (conn net.Conn){
	defer conn.Close()
	log.Printf("conexao de %s\n", conn.RemoteAddr())
	scanner := bufio.NewScanner(conn)
	for scanner.Scan(){
		fmt.Fprintf( conn, "OK\n")
	}
}

func main(){
	num_porta := flag.Int("port", 9001, "porta")
	flag.Parse()

	endereco := fmt.Sprintf(":%d", *num_porta)
	listener, listen_error := net.Listen("tcp", endereco)
	if listen_error != nil{
		log.Fatal(listen_error)
	}

	for{
		conn, err := listener.Accept()
		if err != nil{
			log.Printf("erro no accept: %v", err)
			continue
		}
		go tratar (conn)
	}
}