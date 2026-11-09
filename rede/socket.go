package rede

import (
	"bufio"
	"fmt"
	"net"
	"strconv"
)

// manda msg no socket
func EnviarMensagem(conn net.Conn, msg *Mensagem) error {
	dados, err := msg.ParaBytes()
	if err != nil {
		return fmt.Errorf("erro ao converter mensagem para envio: %w", err)
	}

	_, err = conn.Write(dados)
	if err != nil {
		return fmt.Errorf("erro ao escrever dados no socket: %w", err)
	}

	return nil
}


// recebe msg do socket
func LerMensagem(conn net.Conn) (*Mensagem, error) {
	reader :=  bufio.NewReader(conn)
	dados, err := reader.ReadBytes('\n')
	if err != nil {
		return nil, fmt.Errorf("erro ao ler do socket: %w", err)
	}

	msg, err := MensagemDosBytes(dados)
	if err != nil {
		return nil, fmt.Errorf("erro ao processar dados recebidos: %w", err)
	}
	return msg, nil
}

func CriarListenerTCP(porta int) (net.Listener, error) {
	endereco := fmt.Sprintf(":%d", porta)
	listener, err := net.Listen("tcp", endereco)
	if err != nil {
		return nil, fmt.Errorf("falha ao abrir porta TCP %s: %w", endereco, err)
	}
	return listener, nil
}

// addr é io + porta
func ObterIPePorta(addr net.Addr) (string, int) {
	host, portaStr, err := net.SplitHostPort(addr.String())
	if err != nil {
		return "", 0
	}
	porta, _ := strconv.Atoi(portaStr)
	return host, porta
}

