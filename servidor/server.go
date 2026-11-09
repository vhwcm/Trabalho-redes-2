package main

import (
	"fmt"
	"net"
	"trabalho-redes/rede"
)

const response = "KamehamehameHAAAAAAAAAA"

func iniciaServer(s *Servidor) {
	listener, err := rede.CriarListenerTCP(s.Porta)
	if err != nil {
		fmt.Printf("[ERRO] Falha ao iniciar servidor ID %d na porta %d: %v\n", s.ID, s.Porta, err)
		s.Status = "DOWN"
		return
	}
	defer listener.Close()

	s.Status = "UP"
	fmt.Printf("[Servidor ID %d] Ativo e ouvindo na porta %d\n", s.ID, s.Porta)

	mensagem := &rede.Mensagem{
		Tipo:     rede.TipoResposta,
		Porta:    s.Porta,
		Conteudo: response,
	}

	for {
		conn, err := listener.Accept()
		if err != nil {
			continue
		}

		s.Conexoes++
		tabela.TotalConexoes++

		go func(c net.Conn) {
			defer c.Close()

			msg, err := rede.LerMensagem(c)
			if err != nil {
				return
			}

			fmt.Printf("[Servidor :%d] msg recebida: %+v\n", s.Porta, msg)
			rede.EnviarMensagem(c, mensagem)
		}(conn)
	}
}